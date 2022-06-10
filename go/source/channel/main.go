package main

import (
	"fmt"
	"sync"
)

func main() {
	c := make(chan int)
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
	c <- 1
	close(c)
}

func receiver(c chan int) {
	for {
		rst, ok := <-c
		if ok {
			fmt.Printf("receive %d\n", rst)
		} else {
			break
		}
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
