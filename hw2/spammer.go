package main

import (
	"fmt"
	"slices"
	"sync"
)

func RunPipeline(cmds ...cmd) {
	//fmt.Println("Start RunPipeline func...")
	wg := &sync.WaitGroup{}
	in := make(chan interface{})

	for i, fn := range cmds {
		out := make(chan interface{})
		wg.Add(1)
		go func(i int, fn cmd, in, out chan interface{}) {
			defer wg.Done()
			defer close(out)
			fn(in, out)
		}(i, fn, in, out)

		in = out
	}
	wg.Wait()

}

func SelectUsers(in, out chan interface{}) {
	// 	in - string
	// 	out - User
	//fmt.Println("Start SelectUsers func...")

	wg := &sync.WaitGroup{}
	uniqueUserIds := new(sync.Map)
	for emailStr := range in {
		wg.Add(1)
		go func() {
			defer wg.Done()
			user := GetUser(emailStr.(string))
			if _, loaded := uniqueUserIds.LoadOrStore(user.ID, struct{}{}); !loaded {
				out <- user
			}
		}()
	}
	wg.Wait()
	//fmt.Println("Finish SelectUsers func")

}

func SelectMessages(in, out chan interface{}) {
	// 	in - User
	// 	out - MsgID
	//fmt.Println("Start SelectMessages func...")
	users := make([]User, 0, GetMessagesMaxUsersBatch)
	wg := &sync.WaitGroup{}
	for u := range in {
		users = append(users, u.(User))
		if len(users) == GetMessagesMaxUsersBatch {
			wg.Add(1)
			go func(users []User) {
				defer wg.Done()
				ids, err := GetMessages(users...)
				if err == nil {
					for _, id := range ids {
						out <- id
					}
				}
			}(users)
			users = make([]User, 0, GetMessagesMaxUsersBatch)
		}
	}
	if len(users) > 0 {
		wg.Add(1)
		go func(users []User) {
			defer wg.Done()
			ids, err := GetMessages(users...)
			if err == nil {
				for _, id := range ids {
					out <- id
				}
			}
		}(users)
		users = make([]User, 0, GetMessagesMaxUsersBatch)
	}

	wg.Wait()
	//fmt.Println("Finish SelectMessages func")
}

func CheckSpam(in, out chan interface{}) {
	// in - MsgID
	// out - MsgData
	//fmt.Println("Start CheckSpam func...")
	wg := &sync.WaitGroup{}
	for i := 0; i < HasSpamMaxAsyncRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for v := range in {
				id := v.(MsgID)
				hasSpam, err := HasSpam(id)
				if err != nil {
					continue
				}
				out <- MsgData{ID: id, HasSpam: hasSpam}
			}
		}()
	}

	wg.Wait()
	//fmt.Println("Finish CheckSpam func")
}

func CombineResults(in, out chan interface{}) {
	// in - MsgData
	// out - string
	//fmt.Println("Start CombineResults func...")
	hasSpamResults := make([]MsgID, 0)
	noSpamResults := make([]MsgID, 0)
	for v := range in {
		data := v.(MsgData)
		if data.HasSpam {
			hasSpamResults = append(hasSpamResults, data.ID)
		} else {
			noSpamResults = append(noSpamResults, data.ID)
		}
	}

	slices.Sort(hasSpamResults)
	slices.Sort(noSpamResults)

	for _, id := range hasSpamResults {
		out <- fmt.Sprintf("%v %v", true, id)
	}
	for _, id := range noSpamResults {
		out <- fmt.Sprintf("%v %v", false, id)
	}

	//fmt.Println("Finish CombineResults func")
}
