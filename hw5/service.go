package main

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const logBufferSize = 128

type microservice struct {
	acl map[string][]string

	mu       sync.Mutex
	nextID   int
	logSubs  map[int]chan *Event
	statSubs map[int]*statSubscriber
}

type statSubscriber struct {
	byMethod   map[string]uint64
	byConsumer map[string]uint64
}

func newMicroservice(acl map[string][]string) *microservice {
	return &microservice{
		acl:      acl,
		logSubs:  make(map[int]chan *Event),
		statSubs: make(map[int]*statSubscriber),
	}
}

// ACL

func methodAllowed(patterns []string, method string) bool {
	for _, p := range patterns {
		if p == method {
			return true
		}
		if strings.HasSuffix(p, "/*") && strings.HasPrefix(method, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}

func (m *microservice) authorize(ctx context.Context, method string) (consumer, host string, err error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", "", status.Error(codes.Unauthenticated, "no metadata")
	}
	vals := md.Get("consumer")
	if len(vals) == 0 {
		return "", "", status.Error(codes.Unauthenticated, "no consumer in metadata")
	}
	consumer = vals[0]

	patterns, ok := m.acl[consumer]
	if !ok || !methodAllowed(patterns, method) {
		return "", "", status.Error(codes.Unauthenticated, "access denied")
	}

	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		host = p.Addr.String()
	}
	return consumer, host, nil
}

// логирование + статистика

func (m *microservice) publish(consumer, method, host string) {
	now := time.Now().Unix()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ch := range m.logSubs {
		ev := &Event{Timestamp: now, Consumer: consumer, Method: method, Host: host}
		select {
		case ch <- ev:
		default: // медленный клиент не должен блокировать сервис
		}
	}
	for _, s := range m.statSubs {
		s.byMethod[method]++
		s.byConsumer[consumer]++
	}
}

func (m *microservice) subscribeLog() (int, chan *Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextID
	m.nextID++
	ch := make(chan *Event, logBufferSize)
	m.logSubs[id] = ch
	return id, ch
}

func (m *microservice) unsubscribeLog(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.logSubs, id)
}

func (m *microservice) subscribeStat() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextID
	m.nextID++
	m.statSubs[id] = &statSubscriber{
		byMethod:   make(map[string]uint64),
		byConsumer: make(map[string]uint64),
	}
	return id
}

func (m *microservice) unsubscribeStat(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.statSubs, id)
}

func (m *microservice) takeStat(id int) *Stat {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.statSubs[id]
	st := &Stat{
		Timestamp:  time.Now().Unix(),
		ByMethod:   s.byMethod,
		ByConsumer: s.byConsumer,
	}
	s.byMethod = make(map[string]uint64)
	s.byConsumer = make(map[string]uint64)
	return st
}

// interceptors

func (m *microservice) unaryInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	consumer, host, err := m.authorize(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	m.publish(consumer, info.FullMethod, host)
	return handler(ctx, req)
}

func (m *microservice) streamInterceptor(
	srv interface{},
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	consumer, host, err := m.authorize(ss.Context(), info.FullMethod)
	if err != nil {
		return err
	}
	m.publish(consumer, info.FullMethod, host)
	return handler(srv, ss)
}

type bizService struct {
	UnimplementedBizServer
}

func (bizService) Check(context.Context, *Nothing) (*Nothing, error) { return &Nothing{}, nil }
func (bizService) Add(context.Context, *Nothing) (*Nothing, error)   { return &Nothing{}, nil }
func (bizService) Test(context.Context, *Nothing) (*Nothing, error)  { return &Nothing{}, nil }

type adminService struct {
	UnimplementedAdminServer
	m *microservice
}

func (a *adminService) Logging(_ *Nothing, stream Admin_LoggingServer) error {
	id, ch := a.m.subscribeLog()
	defer a.m.unsubscribeLog(id)

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-ch:
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

func (a *adminService) Statistics(in *StatInterval, stream Admin_StatisticsServer) error {
	if in.IntervalSeconds == 0 {
		return status.Error(codes.InvalidArgument, "interval_seconds must be > 0")
	}

	id := a.m.subscribeStat()
	defer a.m.unsubscribeStat(id)

	ticker := time.NewTicker(time.Duration(in.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := stream.Send(a.m.takeStat(id)); err != nil {
				return err
			}
		}
	}
}

func StartMyMicroservice(ctx context.Context, addr string, acl string) error {
	var aclMap map[string][]string
	if err := json.Unmarshal([]byte(acl), &aclMap); err != nil {
		return err
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	m := newMicroservice(aclMap)

	server := grpc.NewServer(
		grpc.UnaryInterceptor(m.unaryInterceptor),
		grpc.StreamInterceptor(m.streamInterceptor),
	)
	RegisterBizServer(server, bizService{})
	RegisterAdminServer(server, &adminService{m: m})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(lis)
	}()

	go func() {
		select {
		case <-ctx.Done():
			server.Stop()
		case <-done:
		}
	}()

	return nil
}
