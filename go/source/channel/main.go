package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	c := make(chan int, 2)
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		sender(c)
		wg.Done()
	}()
	go func() {
		receiver(c)
		wg.Done()
	}()
	wg.Wait()
}

func sender(c chan int) {
	time.Sleep(4 * time.Second)
	for i := 0; i < 5; i++ {
		start := time.Now()
		c <- i
		cost := time.Since(start)
		fmt.Printf("send %d cost %v\n", i, cost)
	}
	close(c)
}

func receiver(c chan int) {
	for {
		start := time.Now()
		rst, ok := <-c
		if ok {
			cost := time.Since(start)
			fmt.Printf("receive %d cost %v\n", rst, cost)
		} else {
			break
		}
		time.Sleep(4 * time.Second)
	}
}

// no buffer
// func main() {
// 	c := make(chan int)
// 	wg := sync.WaitGroup{}
// 	wg.Add(2)
// 	go func() {
// 		sender(c)
// 		wg.Done()
// 	}()
// 	go func() {
// 		waiter(c)
// 		wg.Done()
// 	}()
// 	wg.Wait()
// }

// func sender(c chan int) {
// 	for i := 0; i < 3; i++ {
// 		fmt.Printf("send %d\n", i)
// 		c <- i
// 		time.Sleep(2 * time.Second)
// 	}
// 	close(c)
// }
// func waiter(c chan int) {
// 	for {
// 		rst, ok := <-c
// 		if ok {
// 			fmt.Printf("receive %d\n", rst)
// 		} else {
// 			break
// 		}
// 	}
// }
