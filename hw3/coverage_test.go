package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type TestCase struct {
	ID       int
	Request  SearchRequest
	Response *SearchResponse
}

type TestCaseError struct {
	ID       int
	req      SearchRequest
	token    string
	filePath string
	handler  http.HandlerFunc // nil = штатный SearchServer
	wantErr  string
}

// тут писать код тестов

// withDataset подменяет путь до датасета на время теста
func withDataset(t *testing.T, path string) {
	t.Helper()
	old := DatasetPath
	DatasetPath = path
	t.Cleanup(func() { DatasetPath = old })
}

func TestFindUsersErrors(t *testing.T) {

	badXML := filepath.Join(t.TempDir(), "bad.xml")
	if err := os.WriteFile(badXML, []byte("<root><row><id>x"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []TestCaseError{
		{1, SearchRequest{Limit: -1}, ValidAccessToken, DatasetPath, nil, "limit must be > 0"},
		{2, SearchRequest{Offset: -1}, ValidAccessToken, DatasetPath, nil, "offset must be > 0"},
		{3, SearchRequest{Limit: 1}, "wrong_token", DatasetPath, nil, "bad AccessToken"},
		{4, SearchRequest{Limit: 1, OrderField: "zzz"}, ValidAccessToken, DatasetPath, nil, "OrderFeld zzz invalid"},
		{5, SearchRequest{Limit: 1}, ValidAccessToken, "wrong_path", nil, "SearchServer fatal error"},
		{6, SearchRequest{Limit: 1}, ValidAccessToken, badXML, nil, "SearchServer fatal error"},
		{7, SearchRequest{Limit: 1, OrderBy: 5}, ValidAccessToken, DatasetPath, nil, "unknown bad request error: invalid order_by"},
		{8, SearchRequest{Limit: 1}, ValidAccessToken, DatasetPath, func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(client.Timeout + 500*time.Millisecond)
		}, "timeout for"},
		{9, SearchRequest{Limit: 1}, ValidAccessToken, DatasetPath, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("not json"))
		}, "cant unpack error json"},
		{10, SearchRequest{Limit: 1}, ValidAccessToken, DatasetPath, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not json"))
		}, "cant unpack result json"},
	}

	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.ID), func(t *testing.T) {
			withDataset(t, tc.filePath)

			h := tc.handler
			if h == nil {
				h = SearchServer
			}
			ts := httptest.NewServer(h)
			defer ts.Close()

			c := SearchClient{AccessToken: tc.token, URL: ts.URL}
			_, err := c.FindUsers(tc.req)

			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestFindUsersUnknownError(t *testing.T) {
	c := SearchClient{AccessToken: ValidAccessToken, URL: "unsupported://host"}
	_, err := c.FindUsers(SearchRequest{Limit: 1})
	if err == nil || !strings.Contains(err.Error(), "unknown error") {
		t.Fatalf("expected unknown error, got %v", err)
	}
}

func TestPositiveCases(t *testing.T) {
	cases := []TestCase{
		{
			ID: 1,
			Request: SearchRequest{
				Limit:  1,
				Offset: 0,
			},
			Response: &SearchResponse{
				Users: []User{
					{0, "Boyd Wolf", 22, "Nulla cillum enim voluptate consequat laborum esse excepteur occaecat commodo nostrud excepteur ut cupidatat. Occaecat minim incididunt ut proident ad sint nostrud ad laborum sint pariatur. Ut nulla commodo dolore officia. Consequat anim eiusmod amet commodo eiusmod deserunt culpa. Ea sit dolore nostrud cillum proident nisi mollit est Lorem pariatur. Lorem aute officia deserunt dolor nisi aliqua consequat nulla nostrud ipsum irure id deserunt dolore. Minim reprehenderit nulla exercitation labore ipsum.\n", "male"},
				},
				NextPage: true,
			},
		},
		{
			ID: 2,
			Request: SearchRequest{
				Limit:      1,
				Offset:     0,
				OrderBy:    -1,
				OrderField: "id",
			},
			Response: &SearchResponse{
				Users: []User{
					{34, "Kane Sharp", 34, "Lorem proident sint minim anim commodo cillum. Eiusmod velit culpa commodo anim consectetur consectetur sint sint labore. Mollit consequat consectetur magna nulla veniam commodo eu ut et. Ut adipisicing qui ex consectetur officia sint ut fugiat ex velit cupidatat fugiat nisi non. Dolor minim mollit aliquip veniam nostrud. Magna eu aliqua Lorem aliquip.\n", "male"},
				},
				NextPage: true,
			},
		},
		{
			ID: 3,
			Request: SearchRequest{
				Limit:      2,
				Offset:     0,
				OrderBy:    -1,
				OrderField: "age",
			},
			Response: &SearchResponse{
				Users: []User{
					{13, "Whitley Davidson", 40, "Consectetur dolore anim veniam aliqua deserunt officia eu. Et ullamco commodo ad officia duis ex incididunt proident consequat nostrud proident quis tempor. Sunt magna ad excepteur eu sint aliqua eiusmod deserunt proident. Do labore est dolore voluptate ullamco est dolore excepteur magna duis quis. Quis laborum deserunt ipsum velit occaecat est laborum enim aute. Officia dolore sit voluptate quis mollit veniam. Laborum nisi ullamco nisi sit nulla cillum et id nisi.\n", "male"},
					{32, "Christy Knapp", 40, "Incididunt culpa dolore laborum cupidatat consequat. Aliquip cupidatat pariatur sit consectetur laboris labore anim labore. Est sint ut ipsum dolor ipsum nisi tempor in tempor aliqua. Aliquip labore cillum est consequat anim officia non reprehenderit ex duis elit. Amet aliqua eu ad velit incididunt ad ut magna. Culpa dolore qui anim consequat commodo aute.\n", "female"},
				},
				NextPage: true,
			},
		},
		{
			ID: 4,
			Request: SearchRequest{
				Limit:   0,
				OrderBy: -1,
			},
			Response: &SearchResponse{
				Users:    []User{},
				NextPage: true,
			},
		},
		{
			ID: 5,
			Request: SearchRequest{
				Limit:   0,
				OrderBy: 1,
			},
			Response: &SearchResponse{
				Users:    []User{},
				NextPage: true,
			},
		},
		{
			ID: 6,
			Request: SearchRequest{
				Limit:   10,
				OrderBy: 1,
				Query:   "Incididunt culpa",
			},
			Response: &SearchResponse{
				Users: []User{
					{32, "Christy Knapp", 40, "Incididunt culpa dolore laborum cupidatat consequat. Aliquip cupidatat pariatur sit consectetur laboris labore anim labore. Est sint ut ipsum dolor ipsum nisi tempor in tempor aliqua. Aliquip labore cillum est consequat anim officia non reprehenderit ex duis elit. Amet aliqua eu ad velit incididunt ad ut magna. Culpa dolore qui anim consequat commodo aute.\n", "female"},
				},
				NextPage: false,
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(SearchServer))
	defer ts.Close()

	c := SearchClient{AccessToken: ValidAccessToken, URL: ts.URL}

	for _, testCase := range cases {
		result, err := c.FindUsers(testCase.Request)
		if err != nil {
			t.Errorf("case number %d: unexpected error: %v", testCase.ID, err)
			continue
		}
		if !reflect.DeepEqual(result, testCase.Response) {
			t.Errorf("case number %d\n\texpected: %v\n\tresult: %v", testCase.ID, testCase.Response, result)
		}
	}
}
