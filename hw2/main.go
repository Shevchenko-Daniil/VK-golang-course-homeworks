package main

import (
	"fmt"
	"time"
)

func main() {

	testResult := []string{}
	inputData := []string{
		"harry.dubois@mail.ru",
		"k.kitsuragi@mail.ru",
		"d.vader@mail.ru",
		"noname@mail.ru",
		"e.musk@mail.ru",
		"spiderman@mail.ru", // is an alias for peter.parker@mail.ru
		"red_prince@mail.ru",
		"tomasangelo@mail.ru",
		"batman@mail.ru", // is an alias for bruce.wayne@mail.ru
		"bruce.wayne@mail.ru",
	}
	RunPipeline(
		cmd(new2CatStrings(inputData, 0)),
		cmd(SelectUsers),
		cmd(SelectMessages),
		cmd(CheckSpam),
		cmd(CombineResults),
		cmd(new2CollectStrings(&testResult)),
	)
}

func new2CatStrings(strs []string, pauses time.Duration) func(in, out chan interface{}) {
	return func(in, out chan interface{}) {
		for _, email := range strs {
			out <- email
			if pauses != 0 {
				time.Sleep(pauses)
			}
		}
	}
}

func new2CollectStrings(strs *[]string) func(in, out chan interface{}) {
	return func(in, out chan interface{}) {
		for dataRaw := range in {
			data := fmt.Sprintf("%v\n", dataRaw)
			*strs = append(*strs, data)
		}
		fmt.Println(strs)
	}
}
