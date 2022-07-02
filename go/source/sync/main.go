package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	wg := &sync.WaitGroup{}
	// 预设并发数
	wg.Add(3)
	go sleepThenAdd(10, wg)
	go sleepThenAdd(11, wg)
	go sleepThenAdd(12, wg)
	wg.Wait()
}

func sleepThenAdd(t int32, wg *sync.WaitGroup) {
	time.Sleep(time.Duration(t) * time.Millisecond)
	fmt.Println("add success ttl ", t)
	wg.Done()
}
