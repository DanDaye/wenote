# sync 包的使用与原理

# 1. 前言

> 在并发过程中，多个线程或 goroutine 可能同时操作同一内存区域，导致出现竞争问题。为保持内存一致性，Go 的 sync 包提供了常见的并发编程原语。其中包括：Mutex、RWMutex、WaitGroup、Once 和 Pool 等。

# 2. Mutex

## 2.1. 具体使用

下边是简单的 Mutex 的使用示例：

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	mu := sync.Mutex{}
	mu.Lock()
	fmt.Printf("lock success")
	mu.Unlock()
}
```

输出打印如下：

```xml
lock success
```

## 2.2. 实现原理

汇编说明

```xml
TEXT "".main(SB) gofile../Users/liangjinsi/Documents/workspace/wenote/go/source/sync/main.go
  main.go:7             0x577                   493b6610                CMPQ 0x10(R14), SP
  main.go:7             0x57b                   7649                    JBE 0x5c6
  main.go:7             0x57d                   4883ec20                SUBQ $0x20, SP
  main.go:7             0x581                   48896c2418              MOVQ BP, 0x18(SP)
  main.go:7             0x586                   488d6c2418              LEAQ 0x18(SP), BP
  main.go:8             0x58b                   488d0500000000          LEAQ 0(IP), AX          [3:7]R_PCREL:type.sync.Mutex
  main.go:8             0x592                   0f1f440000              NOPL 0(AX)(AX*1)
  main.go:8             0x597                   e800000000              CALL 0x59c              [1:5]R_CALL:runtime.newobject<1>
  main.go:8             0x59c                   4889442410              MOVQ AX, 0x10(SP)
  main.go:8             0x5a1                   48c70000000000          MOVQ $0x0, 0(AX)
  main.go:9             0x5a8                   488b442410              MOVQ 0x10(SP), AX
  main.go:9             0x5ad                   e800000000              CALL 0x5b2              [1:5]R_CALL:sync.(*Mutex).Lock
  main.go:10            0x5b2                   488b442410              MOVQ 0x10(SP), AX
  main.go:10            0x5b7                   e800000000              CALL 0x5bc              [1:5]R_CALL:sync.(*Mutex).Unlock
  main.go:11            0x5bc                   488b6c2418              MOVQ 0x18(SP), BP
  main.go:11            0x5c1                   4883c420                ADDQ $0x20, SP
  main.go:11            0x5c5                   c3                      RET
  main.go:7             0x5c6                   e800000000              CALL 0x5cb              [1:5]R_CALL:runtime.morestack_noctxt
```

编译器会把 sync.Mutex 转换成 type.sync.Mutex 类型，type.sync.Mutex 在文件 /src/sync/mutex.go 文件中定义如下：

```go
type Mutex struct{
	state int32
	sema uint32
}

const  (
	mutexLocked = 1 << itoa // mutex is locked
	mutexWoken
	mutexStarving

	mutexWaiterShift  = itoa
	starvationThresholdNs = 1e6
)
```

Mutex 不可复制

（*Mutex)Lock() 函数定义如下：

```go
func (m *Mutex) Lock() {
	// Fast path: grab unlocked mutex.
	if atomic.CompareAndSwapInt32(&m.state, 0, mutexLocked) {
		if race.Enabled {
			race.Acquire(unsafe.Pointer(m))
		}
		return
	}
	// Slow path (outlined so that the fast path can be inlined)
	m.lockSlow()
}

func (m *Mutex) lockSlow() {
	var waitStartTime int64
	starving := false
	awoke := false
	iter := 0
	old := m.state
	for {
		// Don't spin in starvation mode, ownership is handed off to waiters
		// so we won't be able to acquire the mutex anyway.
		if old&(mutexLocked|mutexStarving) == mutexLocked && runtime_canSpin(iter) {
			// Active spinning makes sense.
			// Try to set mutexWoken flag to inform Unlock
			// to not wake other blocked goroutines.
			if !awoke && old&mutexWoken == 0 && old>>mutexWaiterShift != 0 &&
				atomic.CompareAndSwapInt32(&m.state, old, old|mutexWoken) {
				awoke = true
			}
			runtime_doSpin()
			iter++
			old = m.state
			continue
		}
		new := old
		// Don't try to acquire starving mutex, new arriving goroutines must queue.
		if old&mutexStarving == 0 {
			new |= mutexLocked
		}
		if old&(mutexLocked|mutexStarving) != 0 {
			new += 1 << mutexWaiterShift
		}
		// The current goroutine switches mutex to starvation mode.
		// But if the mutex is currently unlocked, don't do the switch.
		// Unlock expects that starving mutex has waiters, which will not
		// be true in this case.
		if starving && old&mutexLocked != 0 {
			new |= mutexStarving
		}
		if awoke {
			// The goroutine has been woken from sleep,
			// so we need to reset the flag in either case.
			if new&mutexWoken == 0 {
				throw("sync: inconsistent mutex state")
			}
			new &^= mutexWoken
		}
		if atomic.CompareAndSwapInt32(&m.state, old, new) {
			if old&(mutexLocked|mutexStarving) == 0 {
				break // locked the mutex with CAS
			}
			// If we were already waiting before, queue at the front of the queue.
			queueLifo := waitStartTime != 0
			if waitStartTime == 0 {
				waitStartTime = runtime_nanotime()
			}
			runtime_SemacquireMutex(&m.sema, queueLifo, 1)
			starving = starving || runtime_nanotime()-waitStartTime > starvationThresholdNs
			old = m.state
			if old&mutexStarving != 0 {
				// If this goroutine was woken and mutex is in starvation mode,
				// ownership was handed off to us but mutex is in somewhat
				// inconsistent state: mutexLocked is not set and we are still
				// accounted as waiter. Fix that.
				if old&(mutexLocked|mutexWoken) != 0 || old>>mutexWaiterShift == 0 {
					throw("sync: inconsistent mutex state")
				}
				delta := int32(mutexLocked - 1<<mutexWaiterShift)
				if !starving || old>>mutexWaiterShift == 1 {
					// Exit starvation mode.
					// Critical to do it here and consider wait time.
					// Starvation mode is so inefficient, that two goroutines
					// can go lock-step infinitely once they switch mutex
					// to starvation mode.
					delta -= mutexStarving
				}
				atomic.AddInt32(&m.state, delta)
				break
			}
			awoke = true
			iter = 0
		} else {
			old = m.state
		}
	}

	if race.Enabled {
		race.Acquire(unsafe.Pointer(m))
	}
}
```

流程图（TODO）：

加锁过程有两个路径
快路径：CAS 判断 state 是否为 0，若是，设置 state = mutextLocked, 并返回，此时为加锁成功，否则进入慢路径。
慢路径：
1. 自旋等待判断锁是否释放，若自旋达到 4 次仍未获得锁，则进入睡眠状态，并被加入到等待队列末尾
2. 进入睡眠态的 goroutine 其等待时间超饥饿阈值，则会被唤醒，并移动到等待队列头部，此时，新进入尝试加锁的 goroutine 将会被自动加入到等待队列尾部，从而避免旧 goroutine 一直获取不到锁。

自旋等待的原因：

* 一个 goroutine 尝试获取锁时，很大概率可以很快获取到锁，这时候通过自旋而非睡眠的方式，可以减少唤醒和上下文切换的操作，最快速度获取锁

饥饿阈值存在的意义：

* 当 goroutine 多次获取不到锁的时候，进入等待队列，当锁被释放时，将会被等待队列中的 goroutine 和新到来的 goroutine 竞争，由于新到来的 goroutine 无需上下文切换，所以一般情况下，等待队列中的 goroutine 都会竞争不过，容易导致队列中的 goroutine 一直获取不到锁。
* 为解决队列中的 goroutine 获取不到锁的问题，引入饥饿阈值，当某个 goroutine 达到饥饿阈值时，锁将会被等待队列头的 goroutine 获得，此时，新到来的 goroutine 将会自动追加到等待队列尾部。

# 3. RWMutex

TODO（锁重入问题）

> RWMutex 适用于读多写少的场景。在同一时刻，有且只有一个 goroutine 能够获取到锁，若读锁被持有，将会阻塞加写锁。

## 3.1. 具体使用

```go
package main

import (
	"sync"
)

func main() {
	mu := sync.RWMutex{}
	mu.RLock()
	fmt.Println("Read lock success")
	mu.RUnlock()
	mu.Lock()
	fmt.Println("Write lock success")
	mu.Unlock()
}
```

输出结果如下：
```xml
Read lock success
Write lock success
```

## 3.2. 实现原理

生成对应的汇编如下：

```xml
TEXT "".main(SB) gofile../Users/liangjinsi/Documents/workspace/wenote/go/source/sync/main.go
  main.go:7             0x602                   493b6610                CMPQ 0x10(R14), SP
  main.go:7             0x606                   7669                    JBE 0x671
  main.go:7             0x608                   4883ec20                SUBQ $0x20, SP
  main.go:7             0x60c                   48896c2418              MOVQ BP, 0x18(SP)
  main.go:7             0x611                   488d6c2418              LEAQ 0x18(SP), BP
  main.go:8             0x616                   488d0500000000          LEAQ 0(IP), AX          [3:7]R_PCREL:type.sync.RWMutex
  main.go:8             0x61d                   0f1f440000              NOPL 0(AX)(AX*1)
  main.go:8             0x622                   e800000000              CALL 0x627              [1:5]R_CALL:runtime.newobject<1>
  main.go:8             0x627                   4889442410              MOVQ AX, 0x10(SP)
  main.go:8             0x62c                   48c70000000000          MOVQ $0x0, 0(AX)
  main.go:8             0x633                   488d4808                LEAQ 0x8(AX), CX
  main.go:8             0x637                   440f1139                MOVUPS X15, 0(CX)
  main.go:9             0x63b                   488b442410              MOVQ 0x10(SP), AX
  main.go:9             0x640                   6690                    NOPW
  main.go:9             0x642                   e800000000              CALL 0x647              [1:5]R_CALL:sync.(*RWMutex).RLock
  main.go:10            0x647                   488b442410              MOVQ 0x10(SP), AX
  main.go:10            0x64c                   e800000000              CALL 0x651              [1:5]R_CALL:sync.(*RWMutex).RUnlock
  main.go:11            0x651                   488b442410              MOVQ 0x10(SP), AX
  main.go:11            0x656                   e800000000              CALL 0x65b              [1:5]R_CALL:sync.(*RWMutex).Lock
  main.go:12            0x65b                   488b442410              MOVQ 0x10(SP), AX
  main.go:12            0x660                   6690                    NOPW
  main.go:12            0x662                   e800000000              CALL 0x667              [1:5]R_CALL:sync.(*RWMutex).Unlock
  main.go:13            0x667                   488b6c2418              MOVQ 0x18(SP), BP
  main.go:13            0x66c                   4883c420                ADDQ $0x20, SP
  main.go:13            0x670                   c3                      RET
  main.go:7             0x671                   e800000000              CALL 0x676              [1:5]R_CALL:runtime.morestack_noctxt
  main.go:7             0x676                   eb8a                    JMP "".main(SB)
```

编译器将 sync.RWMutex 语法解析成对应的 type.sync.RWMutex，RWMutex 是不可复制的。RWMutex 的定义如下：

```go
const rwmutexMaxReaders = 1 << 30

type RWMutex struct {
	w           Mutex  // 互斥锁，保护写操作
	writerSem   uint32 // 写信号
	readerSem   uint32 // 读信号
	readerCount int32  // 读操作计数
	readerWait  int32  // writer 等待完成的 reader 数量
}
```

RWMutex.Lock 其实现原理如下所示：
```go
func (rw *RWMutex) Lock() {
	if race.Enabled {
		_ = rw.w.state
		race.Disable()
	}
    // 先解决和其它写者的竞争
	rw.w.Lock()
	// Announce to readers there is a pending writer.
	r := atomic.AddInt32(&rw.readerCount, -rwmutexMaxReaders) + rwmutexMaxReaders
	// Wait for active readers.
	if r != 0 && atomic.AddInt32(&rw.readerWait, r) != 0 {
		runtime_SemacquireMutex(&rw.writerSem, false, 0)
	}
	if race.Enabled {
		race.Enable()
		race.Acquire(unsafe.Pointer(&rw.readerSem))
		race.Acquire(unsafe.Pointer(&rw.writerSem))
	}
}
```
流程描述如下：

![20220626180126](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220626180126.png)

* 加写锁，会先进行 mutex 竞争，若未竞争到锁，则阻塞等待
* 若竞争到锁，则将 readerCount 的数量取反，并将当前的 readerCount 数记录到 readerWait, 阻塞 reader
* 当 r != 0 或 readerWait 数和 当前已被阻塞的 reader 数不一致时，表示存在读操作，则等待所有唤醒的 reader 处理完后，对 writerSem 信号量进行加锁。

加写锁的优先级较高，若 writer 已经获取到互斥锁，将会阻塞所有未在进行中的 reader, 新加入的 reader 将会用 readerWait 进行统计。

```go
func (rw *RWMutex) Unlock() {
	if race.Enabled {
		_ = rw.w.state
		race.Release(unsafe.Pointer(&rw.readerSem))
		race.Disable()
	}
    // 通知读者已经没有活跃的写者了，恢复可以读锁锁定
	r := atomic.AddInt32(&rw.readerCount, rwmutexMaxReaders)
	if r >= rwmutexMaxReaders {
		race.Enable()
		throw("sync: Unlock of unlocked RWMutex")
	}
	// 唤醒全部被阻塞的读操作
	for i := 0; i < int(r); i++ {
		runtime_Semrelease(&rw.readerSem, false, 0)
	}
	// 释放互斥锁，允许下一个写操作
	rw.w.Unlock()
	if race.Enabled {
		race.Enable()
	}
}
```

流程描述如下图所示：

![20220626181220](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220626181220.png)

RLock() 的实现如下所示：

```go
func (rw *RWMutex) RLock() {
	if race.Enabled {
		_ = rw.w.state
		race.Disable()
	}
    // reader 数量加 1，若 readerCount 为负值，表示此时有 writer 等待请求锁
    // writer 的优先级高，所以会把后来的 reader 阻塞
	if atomic.AddInt32(&rw.readerCount, 1) < 0 {
		// 等待写锁释放
		runtime_SemacquireMutex(&rw.readerSem, false, 0)
	}
	if race.Enabled {
		race.Enable()
		race.Acquire(unsafe.Pointer(&rw.readerSem))
	}
}
```

流程描述如下：

![20220629211552](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220629211552.png)

1. reader 想获取读锁时，会对 readerCount 进行加一，若加一后 readerCount 数仍小于 0，则表示当前有 writer 在执行中，将会堵塞 reader goroutine 直至 writer 释放

RWMutex.RUnlock 的实现如下：

```go
func (rw *RWMutex) RUnlock() {
	if race.Enabled {
		_ = rw.w.state
		race.ReleaseMerge(unsafe.Pointer(&rw.writerSem))
		race.Disable()
	}
	if r := atomic.AddInt32(&rw.readerCount, -1); r < 0 {
		// Outlined slow-path to allow the fast-path to be inlined
		rw.rUnlockSlow(r)
	}
	if race.Enabled {
		race.Enable()
	}
}

func (rw *RWMutex) rUnlockSlow(r int32) {
	// 仍有读操作的时候获得了写锁，就会报错
	if r+1 == 0 || r+1 == -rwmutexMaxReaders {
		race.Enable()
		throw("sync: RUnlock of unlocked RWMutex")
	}
	// 如果被 writer 阻塞的 reader 数为 0，表示所有的 reader 都释放了
	if atomic.AddInt32(&rw.readerWait, -1) == 0 {
		// The last reader unblocks the writer.
		runtime_Semrelease(&rw.writerSem, false, 1)
	}
}
```
流程描述如下：
![20220629211002](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220629211002.png)

1. 若无写锁，那么 r >= 0 直接放行，称为快路径；
2. 若有写尝试获取锁中，那么进入慢路径。
 * 若读操作数量已经超过预设值，或仍有读操作进行中加了写锁，则异常。
 * readerWait 减1,若 readerWait 为0，表示已经没有活跃的 reader ,此时释放 writeSem 信号，让获取到写锁的 writer 进入锁定状态。

# 4. WaitGroup

> WaitGroup 可用于等待一系列 goroutine 执行完成，主 goroutine 调用 Add 方法设置需要等待的 goroutine 数量，并发出去的 goroutine 在执行相关逻辑完成后，调用 Done 方法。同时 Wait 方法可用于阻塞 goroutine 直到所有并发的 goroutine 完成。

## 4.2. WaitGroup 使用示例

在主 goroutine 初始化 WaitGroup 对象，并使用 Add 方法设置需要等待的 goroutine 数量，并用 Wait 方法阻塞主 goroutine 等待并发的 goroutine 执行完毕，大体如下：

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		fmt.Println("do something")
		wg.Done()
	}()
	wg.Wait()
        fmt.Println("success")
}
```

打印输出结果如下：

```xml
do something
success
```

Wait 方法将阻塞主 goroutine, 使得在并发的 goroutine 执行完成后，才打印输出 success。修改程序，移除主程序调用 Wait 方法，修改程序如下：

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		fmt.Println("do something")
		wg.Done()
	}()
        fmt.Println("success")
}
```

观察输出如下：

```xml
success
```

主 goroutine 由于没有被 Wait 阻塞，没有等待并发的 goroutine 执行完成便早已打印输出了 `success` 信息。


# 4.2 WaitGroup 原理

```xml
liangjinsi@liangjinsideMacBook-Pro sync % go tool objdump main.o       
TEXT "".main(SB) gofile../Users/liangjinsi/Documents/workspace/wenote/go/source/sync/main.go
  main.go:7             0x5d9                   493b6610                CMPQ 0x10(R14), SP
  main.go:7             0x5dd                   7669                    JBE 0x648
  main.go:7             0x5df                   4883ec28                SUBQ $0x28, SP
  main.go:7             0x5e3                   48896c2420              MOVQ BP, 0x20(SP)
  main.go:7             0x5e8                   488d6c2420              LEAQ 0x20(SP), BP
  main.go:8             0x5ed                   488d0500000000          LEAQ 0(IP), AX          [3:7]R_PCREL:type.sync.WaitGroup
  main.go:8             0x5f4                   0f1f440000              NOPL 0(AX)(AX*1)
  main.go:8             0x5f9                   e800000000              CALL 0x5fe              [1:5]R_CALL:runtime.newobject<1>
  main.go:8             0x5fe                   4889442418              MOVQ AX, 0x18(SP)
  main.go:8             0x603                   48c70000000000          MOVQ $0x0, 0(AX)
  main.go:8             0x60a                   48c7400400000000        MOVQ $0x0, 0x4(AX)
  main.go:8             0x612                   488b442418              MOVQ 0x18(SP), AX
  main.go:8             0x617                   4889442410              MOVQ AX, 0x10(SP)
  main.go:9             0x61c                   bb01000000              MOVL $0x1, BX
  main.go:9             0x621                   e800000000              CALL 0x626              [1:5]R_CALL:sync.(*WaitGroup).Add
  main.go:10            0x626                   488b442410              MOVQ 0x10(SP), AX
  main.go:10            0x62b                   e800000000              CALL 0x630              [1:5]R_CALL:sync.(*WaitGroup).Done
  main.go:11            0x630                   488b442410              MOVQ 0x10(SP), AX
  main.go:11            0x635                   0f1f4000                NOPL 0(AX)
  main.go:11            0x639                   e800000000              CALL 0x63e              [1:5]R_CALL:sync.(*WaitGroup).Wait
  main.go:12            0x63e                   488b6c2420              MOVQ 0x20(SP), BP
  main.go:12            0x643                   4883c428                ADDQ $0x28, SP
  main.go:12            0x647                   c3                      RET
  main.go:7             0x648                   e800000000              CALL 0x64d              [1:5]R_CALL:runtime.morestack_noctxt
  main.go:7             0x64d                   eb8a                    JMP "".main(SB)
```


### 4.2.1. WaitGroup 结构定义
```go
type WaitGroup struct {
	noCopy noCopy

	state1 [3]uint32
}

// state returns pointers to the state and sema fields stored within wg.state1.
func (wg *WaitGroup) state() (statep *uint64, semap *uint32) {
	if uintptr(unsafe.Pointer(&wg.state1))%8 == 0 {
		// 地址是 64 位对齐，数组前两个元素做state,后一个元素做信号量
		return (*uint64)(unsafe.Pointer(&wg.state1)), &wg.state1[2]
	} else {
		// 如果地址是 32 位对齐，数组后两位做 state,第一个元素做信号量
		return (*uint64)(unsafe.Pointer(&wg.state1[1])), &wg.state1[0]
	}
}
```
WaitGroup 用来等待一系列 goroutine 去完成。主 goroutine 调用 Add 方法去设置需要等待的 goroutine 数量，每个 goroutine 运行结束后会调用 Done 方法。同时，Wait 方法用于阻塞主 goroutine 直到所有 goroutines 完成。
WaitGroup 在首次使用后一定不能被拷贝。

state1 里主要包含三个信息：waiter,counter和 status
waiter : 表示仍需等待多少个 goroutine 完成
count : 表示总共有多少个 goroutine
sema : 表示 waitgroup 的信号量

在64位系统中，数组前两个元素用作 state ,其中高32位表示counter,低32位表示waiter，最后一个元素表示 sema
在 32 位系统中，由于没有元素对齐，取后两个元素用作 state, 第一个元素表示 sema
不同系统中的结构如图所示：

64位系统

![20220702143329](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220702143329.png)


32位系统

![20220702143400](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220702143400.png)

### 4.2.2 WaitGroup.Add() 方法

```go
func (wg *WaitGroup) Add(delta int) {
	statep, semap := wg.state()
	state := atomic.AddUint64(statep, uint64(delta)<<32)
	v := int32(state >> 32)
	w := uint32(state)
	// count 小于0，panic
	if v < 0 {
		panic("sync: negative WaitGroup counter")
	}
	// 
	if w != 0 && delta > 0 && v == int32(delta) {
		panic("sync: WaitGroup misuse: Add called concurrently with Wait")
	}
	if v > 0 || w == 0 {
		return
	}
	
	// 当 waiter 大于 0 时，此时 goroutine 已将 counter 置为 0
	// 现在不能有并发更改 state
	// * Add 不能和 Wait 同时发生
	// * 如果 count == 0,那么 Wait 不能增加
	// 做一个简单的健全检测，避免 WaitGroup 滥用
	if *statep != state {
		panic("sync: WaitGroup misuse: Add called concurrently with Wait")
	}
	// 重置 waiter 和 count 为 0
	// 此时 v ==0 ，w != 0
	*statep = 0
	// 释放所有 waiter
	for ; w != 0; w-- {
		runtime_Semrelease(semap, false, 0)
	}
}
```

流程描述如下：

![20220702151132](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220702151132.png)


* Add 添加 delta 到 count 可能为负数，对于 WaitGroup , count 不能为负数，否则会 panic;
* 不能并发同时执行 Add 和 Wait 方法
* 当 count 为 0 时，所有调用 Wait 被阻塞的 goroutine 将会释放。



### 4.2.3. WaitGroup.Done() 方法

```go
// Done decrements the WaitGroup counter by one.
func (wg *WaitGroup) Done() {
	wg.Add(-1)
}
```

WaitGroup.Done() 方法直接调用 WaitGroup.Add(-1)方法

### 4.2.4 WaitGroup.Wait()

```go
// Wait 会阻塞 goroutine 直到 count 为 0
func (wg *WaitGroup) Wait() {
	statep, semap := wg.state()
	for {
		state := atomic.LoadUint64(statep)
		v := int32(state >> 32)
		w := uint32(state)
		// count == 0, 返回
		if v == 0 {
			return
		}
		// 增加 waiter 数
		if atomic.CompareAndSwapUint64(statep, state, state+1) {
			// 阻塞等待信号释放
			runtime_Semacquire(semap)
			// 在 Wait 返回之前被重用了，会引起 panic
			if *statep != 0 {
				panic("sync: WaitGroup is reused before previous Wait has returned")
			}
			return
		}
	}
}
```

调用流程如图所示：

![20220702152030](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220702152030.png)

* 调用 WaitGroup.Wait 方法，会阻塞当前 goroutine ，直到 WaitGroup 的 count 为 0
* 在 Wait 信号释放之前，不能重用 WaitGroup
* 通过循环 & CAS 的方式，进行 wait 数增加


## 4.3 会引起 panic 的使用示例

### 4.3.1 count 设置为负数

方式一：
调用 Add() 添加负数，如下：

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	wg := &sync.WaitGroup{}
	wg.Add(2)
	fmt.Println("add 2 success")
	wg.Add(-2)
	fmt.Println("add -2 success")
	wg.Add(-2)
	fmt.Println("add -2 success")
}
```

输出结果如下：

```xml
add 2 success
add -2 success
panic: sync: negative WaitGroup counter

goroutine 1 [running]:
sync.(*WaitGroup).Add(0x10bfe00, 0xc00000e018)
        /usr/local/go/src/sync/waitgroup.go:74 +0x105
main.main()
        /Users/liangjinsi/Documents/workspace/wenote/go/source/sync/main.go:14 +0xd6
exit status 2
```

可以用给 Add 方法传递负数，第一次 wg.Add(-2) 后 count 不小于 0， 程序能正常运行，在第二次 wg.Add(-2) 后，由于 count < 0, 此时引发 panic: sync: negative WaitGroup counter。

方式二：
调用 Done 次数过多,如下：

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	fmt.Println("wg add success")
	wg.Done()
	fmt.Println("wg done success")
	wg.Done()
	fmt.Println("wg done twice success")
}
```

输出结果如下：
```xml
wg add success
wg done success
panic: sync: negative WaitGroup counter

goroutine 1 [running]:
sync.(*WaitGroup).Add(0x10bfe60, 0xc00000e018)
        /usr/local/go/src/sync/waitgroup.go:74 +0x105
sync.(*WaitGroup).Done(...)
        /usr/local/go/src/sync/waitgroup.go:99
main.main()
        /Users/liangjinsi/Documents/workspace/wenote/go/source/sync/main.go:14 +0xd7
exit status 2
```

可以发现，当第二次调用 wg.Done 后，此时 count 小于 0， 同样会引发 panic: sync: negative WaitGroup counter 。


### 4.3.2 Wait 执行后再 Add

WaitGroup 的使用原则是，在所有 Add 方法调用完之后再调用 Wait, 否则可能导致程序 panic 或不期望的结果

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	wg := &sync.WaitGroup{}
	go sleepThenAdd(10, wg)
	go sleepThenAdd(11, wg)
	go sleepThenAdd(12, wg)
	time.Sleep(11 * time.Millisecond)
	wg.Wait()
}

func sleepThenAdd(t int32, wg *sync.WaitGroup) {
	time.Sleep(time.Duration(t) * time.Millisecond)
	wg.Add(1)
	fmt.Println("add success ttl ", t)
	wg.Done()
}
```

输出结果如下：
```xml
add success ttl  10
add success ttl  11
```
第三次调用 sleepThenAdd 没有按预期输出。我们需要在调用 Wait 前调用完所有 Add 的方法，解决方案可通过预设 Add 数，如下：

```go
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
```
或在调用并发前先调用Add，如下：

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	wg := &sync.WaitGroup{}
	// 在并发前先调用 Add 方法
	wg.Add(1)
	go sleepThenAdd(10, wg)
	wg.Add(1)
	go sleepThenAdd(11, wg)
	wg.Add(1)
	go sleepThenAdd(12, wg)
	wg.Wait()
}

func sleepThenAdd(t int32, wg *sync.WaitGroup) {
	time.Sleep(time.Duration(t) * time.Millisecond)
	fmt.Println("add success ttl ", t)
	wg.Done()
}
```

最终打印结果如下所示：
```xml
add success ttl  10
add success ttl  11
add success ttl  12
```


### 4.3.3 前一个 Wait 还没结束，就重用 WaitGroup

WaitGroup 的重用，需在前一次 Wait 全部释放后才可以重用，否则会导致程序 panic

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	fmt.Println("wg add success")
	go func() {
		wg.Done()
		fmt.Println("wg done success")
		wg.Add(1)
		fmt.Println("wg add again success")
	}()
	wg.Wait()
	fmt.Println("wg wait success")
}
```

打印输出如下：

```xml
wg add success
wg done success
panic: sync: WaitGroup is reused before previous Wait has returned

goroutine 1 [running]:
sync.(*WaitGroup).Wait(0x10c0080)
        /usr/local/go/src/sync/waitgroup.go:132 +0xa5
main.main()
        /Users/liangjinsi/Documents/workspace/wenote/go/source/sync/main.go:18 +0xbe
exit status 2
liangjinsi@liangjinsideMacBook-Pro sync % go run main.go
wg add success
wg done success
wg add again success
panic: sync: WaitGroup is reused before previous Wait has returned

goroutine 1 [running]:
sync.(*WaitGroup).Wait(0x10c0100)
        /usr/local/go/src/sync/waitgroup.go:132 +0xa5
main.main()
        /Users/liangjinsi/Documents/workspace/wenote/go/source/sync/main.go:19 +0xbe
exit status 2
```

可以发现，在 Wait 释放前重用的 WaitGroup, 引发 panic: sync: WaitGroup is reused before previous Wait has returned

修改程序在重用 WaitGroup 前等待 2 秒，以保证 wg.Wait 释放后再重用 WaitGroup, 修改如下：

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	fmt.Println("wg add success")
	go func() {
		wg.Done()
		fmt.Println("wg done success")
		time.Sleep(2 * time.Second)
		wg.Add(1)
		fmt.Println("wg add again success")
	}()
	wg.Wait()
	fmt.Println("wg wait success")
}
```

打印输出如下：

```xml
wg add success
wg done success
wg wait success
```

WaitGroup 的重用在 Wait 释放后，程序能够正常执行。

