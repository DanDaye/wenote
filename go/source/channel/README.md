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

下边为带缓存的 channel 使用示例，sender 在等待 4 秒后才开始向 channel 发送数据，receiver 每次接收到数据后均等待 4 秒后才开始继续接收，观察 sender 发送数据和 receiver 接收数据的耗时情况。

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

# 3. 实现原理

## 3.1. 查看编译转换

对下边代码进行编译，查看编译器生成的汇编代码
```go
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
```
编译器将 make(chan int) 中的入参通过语法解析转换成 type.chan int 类型，并将 make 方法转换成 runtime.makechan 方法。

![20220610205025](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220610205025.png)

查看 sender 函数，可发现，指令 c <- 1 将通过 runtime.chansend1 函数实现，而 close(c) 指令，将通过 runtime.closechan 实现。

![20220610205342](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220610205342.png)

查看 receiver 函数的汇编情况， 接收 rst, ok := <- c 将通过 runtime.chanrecv2 函数进行处理。

![20220610205529](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220610205529.png)

## 3.2. hchan 的结构

hchan 结构定义在 `runtime/chan.go` 文件中

```go
type hchan struct {
 qcount   uint           // 现有 bufferr 队列中存储的元素个数
 dataqsiz uint           // 循环 buffer 队列的长度，对应创建 make(chan int,3) 中的 3
 buf      unsafe.Pointer // 指向 buffer 的头
 elemsize uint16         // 每个元素的大小
 closed   uint32         // 是否被关闭标识
 elemtype *_type         // 每个元素的类型 type，此类型为创建是编译器对类型转换而来
 sendx    uint           // 在队列中已被发送的下标索引
 recvx    uint           // 在队列中最后接收到的元素的下标索引
 recvq    waitq          // 等待接收的 goroutine 队列
 sendq    waitq          // 等待发送的 goroutine 队列

 // 保护 channel 中所有属性，以及在此 channel 中的几个 sudogs
 // 不要在持有这个锁的状态时改变另一个 G 的状态（特别是不要创建一个新的 G），因为可能会因为堆收缩导致死锁
 lock mutex
}
```

再来看看发送或接收等待队列使用的 waitq 结构
`runtime/chan.go`

```go
type waitq struct {
    first *sudog // 队列头
    last  *sudog // 队列尾
}
```

`runtime/runtime2.go`

```go
// TODO 详细说明
type sudog struct {
    g *g //

    next *sudog
    prev *sudog

    acquiretime int64
    releasetime int64
    ticket uint32

    isSelect bool

    success bool

    parent *sudog
    waitlink *sudog
    waittail *sudog
    c *hchan
}
```

waitq 和 sudog 之间的关系如图所示，waiq 中存在分别指向 sudog 双向链表的头尾指针。

![企业微信截图_16546592432364](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/企业微信截图_16546592432364.png)

- sudog 存在的必要性

由于一个 goroutine 可关联多个 channel, 一个 channel 也可关联多个 goroutine, sudog 的作用是作为 channel 和 goroutine 之间的边，描述具体 goroutine 和 channel 之间的关系，代替 goroutine 在不同的 channel 进行等待。

## 3.3. makechan

创建 channel 的流程如图所示
![20220610215642](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220610215642.png)

具体实现参考 `runtime/chan.go`

```go
func makechan(t *chantype, size int) *hchan {
 elem := t.elem

 // 检查元素类型大小
 if elem.size >= 1<<16 {
  throw("makechan: invalid channel element type")
 }
 // 检查元素对齐是否正常
 if hchanSize%maxAlign != 0 || elem.align > maxAlign {
  throw("makechan: bad alignment")
 }
 // 检查元素 elem.size * 个数 size 的大小是否过大
 mem, overflow := math.MulUintptr(elem.size, uintptr(size))
 if overflow || mem > maxAlloc-hchanSize || size < 0 {
  panic(plainError("makechan: size out of range"))
 }

 // 当存储在 buf 中的元素不包含指针时，hchan 不包含对 GC 感兴趣的执行。
 // 指向同一块分配地址的 buf 指针，元素类型是持久化的
 // SudoG's 引用其自身拥有的线程，因此无法被收集
 var c *hchan
 switch {
 case mem == 0:
  // Queue or element size is zero.
  c = (*hchan)(mallocgc(hchanSize, nil, true))
  // 为同步在此处使用 race 探测
  // 将 channel 中类似读取和写入的操作在此地址发生
  // 避免使用 qcount or dataqsize 地址，因为 len() 和 cap() 这些内置函数读取这些地址
  // 并且我们不希望这些内置操作和 close() 之类的操作发生竞争
  c.buf = c.raceaddr()
 case elem.ptrdata == 0:
  c = (*hchan)(mallocgc(hchanSize+mem, nil, true))
  c.buf = add(unsafe.Pointer(c), hchanSize)
 default:
  c = new(hchan)
  c.buf = mallocgc(mem, elem, true)
 }

 c.elemsize = uint16(elem.size)
 c.elemtype = elem
 c.dataqsiz = uint(size)
 lockInit(&c.lock, lockRankHchan)

 if debugChan {
  print("makechan: chan=", c, "; elemsize=", elem.size, "; dataqsiz=", size, "\n")
 }
 return c
}
```

## 3.4. chansend

runtime.chansend1 其主要内容如下, 其里边使用 runtime.chansend 函数，并设置 block 状态为 true

```go
func chansend1(c *hchan, elem unsafe.Pointer) {
	chansend(c, elem, true, getcallerpc())
}
```

chansend 函数大体流程如图所示
- 先无锁判断 channel 是否非阻塞未关闭且缓存已满，若是，则直接返回 false
- 判断 channel 是否已经关闭，若是，则 panic
- 判断 receiver 队列中是否有等待者，若是，则绕过 channel buffer 直接将数据发送给 receiver
- 判断 channel 缓存队列是否已满，若否，则将数据内容加入到缓存中
- 判断是否可阻塞，若否，则直接返回 false
- 可阻塞状态下，创建 sudog 关联 channel 和对应的 goroutine，并代替对应的 goroutine 阻塞等待唤醒。chansend1 中设置的 block 为 true,故缓存满的时候，会阻塞 sender

![20220610223708](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220610223708.png)


```go
func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
	if c == nil {
		if !block {
			return false
		}
		gopark(nil, nil, waitReasonChanSendNilChan, traceEvGoStop, 2)
		throw("unreachable")
	}

	if debugChan {
		print("chansend: chan=", c, "\n")
	}

	if raceenabled {
		racereadpc(c.raceaddr(), callerpc, abi.FuncPCABIInternal(chansend))
	}

	// 快路径：无需获取锁检查失败的非阻塞操作
	//
	// 观察到 channel 未关闭后，我们观察 channel 是否未准备好发送。每个观察都是一个单字节大小的读
	// 不能够向一个已关闭的 channel 发送数据，及时 channel 的关闭时机发生在两个观察者之间
	if !block && c.closed == 0 && full(c) {
		return false
	}

	var t0 int64
	if blockprofilerate > 0 {
		t0 = cputicks()
	}

	lock(&c.lock)
	// channel 已经关闭，无法发送，并返回 panic
	if c.closed != 0 {
		unlock(&c.lock)
		panic(plainError("send on closed channel"))
	}

	if sg := c.recvq.dequeue(); sg != nil {
		// Found a waiting receiver. We pass the value we want to send
		// directly to the receiver, bypassing the channel buffer (if any).
		// 查找一个等待的接收者，绕过 channel 缓存，直接把数据发送给接收者，
		send(c, sg, ep, func() { unlock(&c.lock) }, 3)
		return true
	}

	// 如果当前数据量少于缓存，则存入缓存
	if c.qcount < c.dataqsiz {
		// Space is available in the channel buffer. Enqueue the element to send.
		qp := chanbuf(c, c.sendx) // 查找数据可存入缓存的问题
		if raceenabled {
			racenotify(c, c.sendx, nil)
		}
		typedmemmove(c.elemtype, qp, ep) // 将数据存入缓存的指定位置
		c.sendx++                        // 指向下一个位置
		if c.sendx == c.dataqsiz {       // 循环队列，重新指向开头
			c.sendx = 0
		}
		c.qcount++      // 数据量增加 1
		unlock(&c.lock) // 释放锁
		return true     // 发送成功
	}

	if !block {
		unlock(&c.lock)
		return false
	}

	// Block on the channel. Some receiver will complete our operation for us.
	// 对于无缓存或缓存为空，将阻塞 channel
	gp := getg()
	mysg := acquireSudog() // 创建 sudog 用于存储当前 gp 信息
	mysg.releasetime = 0
	if t0 != 0 {
		mysg.releasetime = -1
	}
	mysg.elem = ep // 存入发送数据
	mysg.waitlink = nil
	mysg.g = gp // 存入当前 gp
	mysg.isSelect = false
	mysg.c = c        // 存入当前 channel
	gp.waiting = mysg //
	gp.param = nil
	c.sendq.enqueue(mysg) // 将等待存入等待发送队列
	// 修改当前的 g 的阻塞状态，并标明是 channel 发送阻塞
	atomic.Store8(&gp.parkingOnChan, 1)
	gopark(chanparkcommit, unsafe.Pointer(&c.lock), waitReasonChanSend, traceEvGoBlockSend, 2)
	// 确保接收前 ep 还活着
	KeepAlive(ep)

	// someone woke us up.
	if mysg != gp.waiting {
		throw("G waiting list is corrupted")
	}
	gp.waiting = nil // 清空等待状态
	gp.activeStackChans = false
	closed := !mysg.success
	gp.param = nil // 清空唤醒参数
	if mysg.releasetime > 0 {
		blockevent(mysg.releasetime-t0, 2)
	}
	mysg.c = nil       // 清空 channel
	releaseSudog(mysg) // 释放等待列表中的 g
	if closed {        // 如果 channle 关闭，发送 panic
		if c.closed == 0 {
			throw("chansend: spurious wakeup")
		}
		panic(plainError("send on closed channel"))
	}
	return true
}
```

在进行阻塞等待前需要创建对应的 sudog ，待唤醒并发送完后，释放对应的 sudog。

创建 sudog 的内存分配策略如下：
- 若 goroutine 运行所在的 P 的有本地缓存，则直接从本地缓存中分配
- 若无本地缓存，则试图从 central cache 中分配
- 若 central cache 仍无可供使用的内存，则 new 一个

释放 sudog 的内存分配则和创建是对应相反。



## 3.5. chanrecv

TODO 流程图

```go
func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
	// raceenabled: don't need to check ep, as it is always on the stack
	// or is new memory allocated by reflect.

	if debugChan {
		print("chanrecv: chan=", c, "\n")
	}

	if c == nil {
		if !block {
			return
		}
		gopark(nil, nil, waitReasonChanReceiveNilChan, traceEvGoStop, 2)
		throw("unreachable")
	}

	// Fast path: check for failed non-blocking operation without acquiring the lock.
	if !block && empty(c) {
		// After observing that the channel is not ready for receiving, we observe whether the
		// channel is closed.
		//
		// Reordering of these checks could lead to incorrect behavior when racing with a close.
		// For example, if the channel was open and not empty, was closed, and then drained,
		// reordered reads could incorrectly indicate "open and empty". To prevent reordering,
		// we use atomic loads for both checks, and rely on emptying and closing to happen in
		// separate critical sections under the same lock.  This assumption fails when closing
		// an unbuffered channel with a blocked send, but that is an error condition anyway.
		if atomic.Load(&c.closed) == 0 {
			// Because a channel cannot be reopened, the later observation of the channel
			// being not closed implies that it was also not closed at the moment of the
			// first observation. We behave as if we observed the channel at that moment
			// and report that the receive cannot proceed.
			return
		}
		// The channel is irreversibly closed. Re-check whether the channel has any pending data
		// to receive, which could have arrived between the empty and closed checks above.
		// Sequential consistency is also required here, when racing with such a send.
		if empty(c) {
			// The channel is irreversibly closed and empty.
			if raceenabled {
				raceacquire(c.raceaddr())
			}
			if ep != nil {
				typedmemclr(c.elemtype, ep)
			}
			return true, false
		}
	}

	var t0 int64
	if blockprofilerate > 0 {
		t0 = cputicks()
	}

	lock(&c.lock)

	if c.closed != 0 {
		if c.qcount == 0 {
			if raceenabled {
				raceacquire(c.raceaddr())
			}
			unlock(&c.lock)
			if ep != nil {
				typedmemclr(c.elemtype, ep)
			}
			return true, false
		}
		// The channel has been closed, but the channel's buffer have data.
	} else {
		// Just found waiting sender with not closed.
		if sg := c.sendq.dequeue(); sg != nil {
			// Found a waiting sender. If buffer is size 0, receive value
			// directly from sender. Otherwise, receive from head of queue
			// and add sender's value to the tail of the queue (both map to
			// the same buffer slot because the queue is full).
			recv(c, sg, ep, func() { unlock(&c.lock) }, 3)
			return true, true
		}
	}

	if c.qcount > 0 {
		// Receive directly from queue
		qp := chanbuf(c, c.recvx)
		if raceenabled {
			racenotify(c, c.recvx, nil)
		}
		if ep != nil {
			typedmemmove(c.elemtype, ep, qp)
		}
		typedmemclr(c.elemtype, qp)
		c.recvx++
		if c.recvx == c.dataqsiz {
			c.recvx = 0
		}
		c.qcount--
		unlock(&c.lock)
		return true, true
	}

	if !block {
		unlock(&c.lock)
		return false, false
	}

	// no sender available: block on this channel.
	gp := getg()
	mysg := acquireSudog()
	mysg.releasetime = 0
	if t0 != 0 {
		mysg.releasetime = -1
	}
	// No stack splits between assigning elem and enqueuing mysg
	// on gp.waiting where copystack can find it.
	mysg.elem = ep
	mysg.waitlink = nil
	gp.waiting = mysg
	mysg.g = gp
	mysg.isSelect = false
	mysg.c = c
	gp.param = nil
	c.recvq.enqueue(mysg)
	// Signal to anyone trying to shrink our stack that we're about
	// to park on a channel. The window between when this G's status
	// changes and when we set gp.activeStackChans is not safe for
	// stack shrinking.
	atomic.Store8(&gp.parkingOnChan, 1)
	gopark(chanparkcommit, unsafe.Pointer(&c.lock), waitReasonChanReceive, traceEvGoBlockRecv, 2)

	// someone woke us up
	if mysg != gp.waiting {
		throw("G waiting list is corrupted")
	}
	gp.waiting = nil
	gp.activeStackChans = false
	if mysg.releasetime > 0 {
		blockevent(mysg.releasetime-t0, 2)
	}
	success := mysg.success
	gp.param = nil
	mysg.c = nil
	releaseSudog(mysg)
	return true, success
}
```

## 3.6. closechan

```go
func closechan(c *hchan) {
 if c == nil {
  panic(plainError("close of nil channel"))
 }

 lock(&c.lock)
 if c.closed != 0 {
  unlock(&c.lock)
  panic(plainError("close of closed channel"))
 }

 if raceenabled {
  callerpc := getcallerpc()
  racewritepc(c.raceaddr(), callerpc, abi.FuncPCABIInternal(closechan))
  racerelease(c.raceaddr())
 }

 c.closed = 1

 var glist gList

 // release all readers
 for {
  sg := c.recvq.dequeue()
  if sg == nil {
   break
  }
  if sg.elem != nil {
   typedmemclr(c.elemtype, sg.elem)
   sg.elem = nil
  }
  if sg.releasetime != 0 {
   sg.releasetime = cputicks()
  }
  gp := sg.g
  gp.param = unsafe.Pointer(sg)
  sg.success = false
  if raceenabled {
   raceacquireg(gp, c.raceaddr())
  }
  glist.push(gp)
 }

 // release all writers (they will panic)
 for {
  sg := c.sendq.dequeue()
  if sg == nil {
   break
  }
  sg.elem = nil
  if sg.releasetime != 0 {
   sg.releasetime = cputicks()
  }
  gp := sg.g
  gp.param = unsafe.Pointer(sg)
  sg.success = false
  if raceenabled {
   raceacquireg(gp, c.raceaddr())
  }
  glist.push(gp)
 }
 unlock(&c.lock)

 // Ready all Gs now that we've dropped the channel lock.
 for !glist.empty() {
  gp := glist.pop()
  gp.schedlink = 0
  goready(gp, 3)
 }
}
```

# 参考
