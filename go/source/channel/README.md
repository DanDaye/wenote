Channel
# 1. 前言

> channel 是并发安全的，可用于在不同协程中进行消息传递。

# 2. 基本使用

## 2.1. 无缓存的 channel

无缓存 channel 没有任何保存值的能力，因此会导致先发送的 sender 或先接收的 receiver 阻塞。数据的发送和接收需在同一时间发生。

下边是无缓存 channel 使用示例，启动 sender 和 waiter 两个 goroutine，sender 多次向共享 channel 发送数值，观察 receiver 的接收情况并打印输出。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)
![defer_delay](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/defer_delay.png)
func main() {
	c := make(chan int)
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		sender(c)
		wg.Done()
	}()
	go func() {
		waiter(c)
		wg.Done()
	}()
	wg.Wait()
}

func sender(c chan int) {
	for i := 0; i < 3; i++ {
		fmt.Printf("send %d\n", i)
		c <- i
		time.Sleep(2 * time.Second)
	}
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
```

得到控制台打印结果如下，sender 一旦向 channel 发送数据，数据将会立即被 receiver 接收。
```xml
send 0
receive 0
send 1
receive 1
send 2
receive 2
```

## 2.2. 有缓存的 channel

有缓存 channel 提供给定缓存容量 buffer 用来保存值，不要求 sender 或 receiver 必须同时存在才能发送或接收数据，当 buffer 未满时，sender 可无阻塞发送，当 buffer 已满时，才会阻塞 sender。对于接收者 receiver, 当 buffer 中未存储任何值，将会被阻塞等待。

下边为带缓存的 channel 使用示例，sender 在等待 4 秒后才开始向 channel 发送数据，receiver 每次接收到数据后均等待4秒后才开始继续接收，观察 sender 发送数据和 receiver 接收数据的耗时情况。

```go
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
```

打印输出如下，可以发现由于前 4 秒中 sender 未向 channel 发送任何数据，由于 channel 中的 buffer 为空，receiver 被阻塞等待，receiver 从开始接收在接收到第一个数据耗时大于 4 秒。sender 向 channel 发送前 3 个值时耗时极短，待发送第四个值时，由于 receiver 未能快速消费让 buffer 有空间，导致 sender 被阻塞，往后的数据发送均被阻塞约 4 秒时间。

```xml
send 0 cost 2.021µs
send 1 cost 833ns
send 2 cost 249ns
receive 0 cost 4.000499609s
receive 1 cost 2.219µs
send 3 cost 4.00121161s
receive 2 cost 2.408µs
send 4 cost 4.000425328s
receive 3 cost 1.615µs
receive 4 cost 1.476µs
```