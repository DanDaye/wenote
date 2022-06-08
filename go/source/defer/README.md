defer 的使用与原理

# 1. 前言
> defer 是 go 语言的关键字，被 defer 修饰的 func，将会在函数返回之前执行。
defer 具有以下特点
* 延迟执行
* 参数预计算
* 同一 goroutine 中多个 defer 的执行顺序依照 FILO 规则

# 2. 使用场景

## 2.1. 资源释放

下边的程序是打开文件，并在执行一系列操作后，将文件关闭。

```go
func readFile() {
  src,err := os.Open("filename")
  if err != nil {
    return
  }
  // do something
  src.Close()
}
```

代码通过 os.Open 打开一个文件，在执行一系列操作后，在最后通过 src.Close 方法将资源释放。正常情况下，该程序都能够正常运行到 src.Close。但在执行一系列操作过程中，出现异常并退出程序后，文件资源将无法被释放。一般，我们期望在程序退出前能够执行资源释放操作，而使用 defer 能达到这种目的。将上边程序修改如下，在成功打开文件之后，无论后边程序是否能够正常运行 ，在退出前，都能够将文件资源释放。

```go
func readFile() {
  src,err := os.Open("filename")
  if err != nil {
    return
  }
  defer src.Close()
  // do something
}
```

同样的问题还会发生在使用 lock, channel 的情况下，一旦加锁后发生错误，将可能导致严重的死锁问题，多次创建未被 close 掉的 channel，长期下来，将导致严重的内存泄漏问题。

## 2.2. 异常捕获

程序可能存在 panic ，如除 0 运算、数组越界访问 、对空指针取值等，这些异常将导致程序退出。某些时候，我们期望捕获这些异常，并让程序正常执行。下边为模拟程序运行过程中发生 panic。

```go
func doPanic() {
  panic("panic")
  fmt.Println("do panic success")
}
```

将会得到输出如下，terminal 将会打印出程序异常退出栈追踪信息 。

```go
panic: panic

goroutine 1 [running]:
main.doPanic(...)
        /Users/xxx/Documents/workspace/go/notecode/main.go:29
main.main()
        /Users/xxx/Documents/workspace/go/notecode/main.go:8 +0x27
exit status 2
```

若期望程序能够正常运行，可将程序修改如下：

```go
func doPanic() {
  defer func(){
    if err := recover();err != nil {
      fmt.Println("recover")
    }
  }()
  panic("panic")
  fmt.Println("do panic success")
}
```

输出如下，表明程序异常后，仍能被正常调用执行。

```xml
recover
```

# 3. 原理解析

本文使用的 go version 为 go1.17.10

## 3.1. defer 结构定义

每个协程对象中，存在一个 _defer 指针，该指针指向与该协程相关联的 defer，每个 defer 的调用只与当前协程有关 ，与其它协程无关。

```go
type g struct  {
  _defer  *_defer
}
```

_defer 结构定义在 runtime/runtime2.go 文件中，具体定义如下：

```go
type _defer struct {
  siz int32 // 参数大小
  started bool  // 当前 defer 是否开始被执行
  heap bool // 是否被分配到堆中
  openDefer bool // 是否开放式编程
  sp uintptr  // sp 寄存器 
  pc uintptr // pc 寄存器
  fn *funcval // defer 运行的 func
  _panic *_panic 
  link *_defer // 下一个 _defer
  // 与开放式编程相关的参数
  fd unsafe.Pointer  
  varp uintptr
  framepc uintptr
}
```

多个 defer 与 goroutine 的关联如下：

![g_defer.png](https://p6-juejin.byteimg.com/tos-cn-i-k3u1fbpfcp/e5aa09a8afb6440686b2b74e125f8083~tplv-k3u1fbpfcp-watermark.image?)

## 3.2. 延迟执行 

defer 后的函数不会立即执行，而是待函数执行结束后再调用。

```go
func doDefer() {
	defer func() {
		fmt.Println("do defer")
	}()
	fmt.Println("doDefer normal")
}
```

输出结果如下，说明 defer 后的函数待函数执行返回时才执行，因此可以很好地用作资源释放。

```xml
doDefer normal
do defer
```

通过 `go tool compile -N -l main.go` + `go tool objdump main.o`运行函数，观察 main.go 的汇编情况。

![defer_delay.png](https://p9-juejin.byteimg.com/tos-cn-i-k3u1fbpfcp/bf963e4a80c745ae9a198449e82e70f5~tplv-k3u1fbpfcp-watermark.image?)

在执行 doDefer 函数的过程中，先是调用 runtime.deferprocStack() 方法，待 fmt.Println() 打印结束后，再执行 runtime.deferreturn。编译器会自动在函数的最后插入 deferreturn, 这也是 defer 为何具有延迟执行特性的原因。

打开 runtime/panic.go 文件， 查看 deferprocStack 函数的内容，如下：

```go
func deferprocStack(d *_defer) {
	gp := getg() // 获取 g, 判断是否在当前栈上
	if gp.m.curg != gp {
		// go code on the system stack can't defer
		throw("defer on system stack")
	}
	if goexperiment.RegabiDefer && d.siz != 0 {
		throw("defer with non-empty frame")
	}
	// siz and fn are already set.
	// The other fields are junk on entry to deferprocStack and
	// are initialized here.
  // defer 未开始
	d.started = false
  // defer 不在堆上
	d.heap = false
  // defer 没有开放式编程
	d.openDefer = false
  // 设置 sp
	d.sp = getcallersp()
  // 设置 pc
	d.pc = getcallerpc()
  // 没有开放式编程，framepc,varp 等属性都是为 0
	d.framepc = 0
	d.varp = 0
	// The lines below implement:
	//   d.panic = nil
	//   d.fd = nil
	//   d.link = gp._defer
	//   gp._defer = d
	// But without write barriers. The first three are writes to
	// the stack so they don't need a write barrier, and furthermore
	// are to uninitialized memory, so they must not use a write barrier.
	// The fourth write does not require a write barrier because we
	// explicitly mark all the defer structures, so we don't need to
	// keep track of pointers to them with a write barrier.
	*(*uintptr)(unsafe.Pointer(&d._panic)) = 0
	*(*uintptr)(unsafe.Pointer(&d.fd)) = 0
  // 将当前 defer 插入到链表头部
	*(*uintptr)(unsafe.Pointer(&d.link)) = uintptr(unsafe.Pointer(gp._defer))
	*(*uintptr)(unsafe.Pointer(&gp._defer)) = uintptr(unsafe.Pointer(d))

  // 在 return0 之后，将不能再设置任何代码
	return0()
	// No code can go here - the C return register has
	// been set and must not be clobbered.
}
```

deferreturn 内容如下：

```go
func deferreturn() {
  // 获取当前 g
	gp := getg()
  // 获取 g 的 _defer 头节点
	d := gp._defer
  // 不存在 _defer 链表，直接结束
	if d == nil {
		return
	}
	sp := getcallersp()
	if d.sp != sp {
		return
	}
	if d.openDefer {
		done := runOpenDeferFrame(gp, d)
		if !done {
			throw("unfinished open-coded defers in deferreturn")
		}
		gp._defer = d.link
		freedefer(d)
		return
	}

	// 移动参数，在这个点后的所有调用的内容不得包含堆栈溢出
  // 因为垃圾收集器不会知道参数的形式直到 jumpdefer 执行完
	argp := getcallersp() + sys.MinFrameSize
	switch d.siz {
	case 0:
		// Do nothing.
	case sys.PtrSize:
		*(*uintptr)(unsafe.Pointer(argp)) = *(*uintptr)(deferArgs(d))
	default:
    // 在栈上的 _defer, 与它关联的参数立即存储在内存的  _defer head 后 
		memmove(unsafe.Pointer(argp), deferArgs(d), uintptr(d.siz))
	}
	fn := d.fn
	d.fn = nil
	gp._defer = d.link
  // 清空当前 _defer
	freedefer(d)
	// If the defer function pointer is nil, force the seg fault to happen
	// here rather than in jmpdefer. gentraceback() throws an error if it is
	// called with a callback on an LR architecture and jmpdefer is on the
	// stack, because the stack trace can be incorrect in that case - see
	// issue #8153).
	_ = fn.fn
  // 执行 _defer 关联的 func 和下一个 _defer
	jmpdefer(fn, argp)
}
```

## 3.3. 参数预计算
下边程序的 defer 函数中传入参数 a , 参数 a 在后边执行加一操作。

```go
package main

import "fmt"

func main(){
   a := 1
   defer DeferA(a)
   a += 1
   fmt.Printf("normal run,a:%d\n",a)
}
```

输出如下，在 deferA 中输出的参数 a, 并没有随着 a 的改变，而输出 2，在

```xml
normal run,a:2
DeferA,a :1
```

## 3.4. 多次 defer 与 LIFO 执行顺序一致

```go
package main

import "fmt"

func main(){
   defer DeferA()
   defer DeferB()
   defer DeferC()
   fmt.Println("normal run")
}
func DeferA(){
   fmt.Println("DeferA")
}
func  DeferB()  {
   fmt.Println("DeferB")
}

func DeferC() {
   fmt.Println("DeferC")
}
```

输出如下，越靠前的 defer ，其输出越靠后

```xml
normal run
DeferC
DeferB
DeferA
```
查看其汇编代码；
```xml
  main.go:5             0x115f                  4c8da42458ffffff                LEAQ 0xffffff58(SP), R12        [3:3]R_USEIFACE:type.string
  main.go:5             0x1167                  4d3b6610                        CMPQ 0x10(R14), R12
  main.go:5             0x116b                  0f863b010000                    JBE 0x12ac
  main.go:5             0x1171                  4881ec28010000                  SUBQ $0x128, SP
  main.go:5             0x1178                  4889ac2420010000                MOVQ BP, 0x120(SP)
  main.go:5             0x1180                  488dac2420010000                LEAQ 0x120(SP), BP
  main.go:6             0x1188                  488d0d00000000                  LEAQ 0(IP), CX                  [3:7]R_PCREL:"".DeferA·f
  main.go:6             0x118f                  48898c24c0000000                MOVQ CX, 0xc0(SP)
  main.go:6             0x1197                  488d8424a8000000                LEAQ 0xa8(SP), AX
  main.go:6             0x119f                  e800000000                      CALL 0x11a4                     [1:5]R_CALL:runtime.deferprocStack<1>
  main.go:6             0x11a4                  85c0                            TESTL AX, AX
  main.go:6             0x11a6                  0f85eb000000                    JNE 0x1297
  main.go:6             0x11ac                  eb00                            JMP 0x11ae
  main.go:7             0x11ae                  488d0d00000000                  LEAQ 0(IP), CX                  [3:7]R_PCREL:"".DeferB·f
  main.go:7             0x11b5                  48894c2478                      MOVQ CX, 0x78(SP)
  main.go:7             0x11ba                  488d442460                      LEAQ 0x60(SP), AX
  main.go:7             0x11bf                  e800000000                      CALL 0x11c4                     [1:5]R_CALL:runtime.deferprocStack<1>
  main.go:7             0x11c4                  85c0                            TESTL AX, AX
  main.go:7             0x11c6                  0f85b6000000                    JNE 0x1282
  main.go:7             0x11cc                  eb00                            JMP 0x11ce
  main.go:8             0x11ce                  488d0d00000000                  LEAQ 0(IP), CX                  [3:7]R_PCREL:"".DeferC·f
  main.go:8             0x11d5                  48894c2430                      MOVQ CX, 0x30(SP)
  main.go:8             0x11da                  488d442418                      LEAQ 0x18(SP), AX
  main.go:8             0x11df                  e800000000                      CALL 0x11e4                     [1:5]R_CALL:runtime.deferprocStack<1>
  main.go:8             0x11e4                  85c0                            TESTL AX, AX
  main.go:8             0x11e6                  0f8581000000                    JNE 0x126d
  main.go:8             0x11ec                  eb00                            JMP 0x11ee
  main.go:9             0x11ee                  440f11bc24f8000000              MOVUPS X15, 0xf8(SP)
  main.go:9             0x11f7                  488d8424f8000000                LEAQ 0xf8(SP), AX
  main.go:9             0x11ff                  48898424f0000000                MOVQ AX, 0xf0(SP)
  main.go:9             0x1207                  8400                            TESTB AL, 0(AX)
  main.go:9             0x1209                  488d1500000000                  LEAQ 0(IP), DX                  [3:7]R_PCREL:type.string
  main.go:9             0x1210                  48899424f8000000                MOVQ DX, 0xf8(SP)
  main.go:9             0x1218                  488d1500000000                  LEAQ 0(IP), DX                  [3:7]R_PCREL:""..stmp_0<1>
  main.go:9             0x121f                  4889942400010000                MOVQ DX, 0x100(SP)
  main.go:9             0x1227                  8400                            TESTB AL, 0(AX)
  main.go:9             0x1229                  eb00                            JMP 0x122b
  main.go:9             0x122b                  4889842408010000                MOVQ AX, 0x108(SP)
  main.go:9             0x1233                  48c784241001000001000000        MOVQ $0x1, 0x110(SP)
  main.go:9             0x123f                  48c784241801000001000000        MOVQ $0x1, 0x118(SP)
  main.go:9             0x124b                  bb01000000                      MOVL $0x1, BX
  main.go:9             0x1250                  4889d9                          MOVQ BX, CX
  main.go:9             0x1253                  e800000000                      CALL 0x1258                     [1:5]R_CALL:fmt.Println
  main.go:10            0x1258                  e800000000                      CALL 0x125d                     [1:5]R_CALL:runtime.deferreturn<1>
  main.go:10            0x125d                  488bac2420010000                MOVQ 0x120(SP), BP
  main.go:10            0x1265                  4881c428010000                  ADDQ $0x128, SP
  main.go:10            0x126c                  c3                              RET
  main.go:8             0x126d                  e800000000                      CALL 0x1272                     [1:5]R_CALL:runtime.deferreturn<1>
  main.go:8             0x1272                  488bac2420010000                MOVQ 0x120(SP), BP
  main.go:8             0x127a                  4881c428010000                  ADDQ $0x128, SP
  main.go:8             0x1281                  c3                              RET
```
针对 DeferA, DeferB,DeferC, 编译器分布将关键字 defer 转换成 runtime.deferprocStack，并在运行末尾进行三次 deferreturn。从 3.2 中可知，runtime.deferprocStack 每次都创建一个 _defer ，并将其加入到 goroutine 的 _defer 链表头部，执行完三次 runtime.deferprocStack 后，_defer 链表的情况是 DeferC -> DeferB -> DeferA 。同样从 3.2 中可知 runtime.deferreturn 每次取出 goroutine 的 _defer 并调用执行其关联的 function。因此，程序中三个 defer 的执行顺序是 DeferC、DeferB、DeferA。

# 4. 捕捉异常
## 4.1. 如何捕捉异常
一般情况下程序发生异常，将会终止整个请求运行。如下所示：
```go
package main

import "fmt"

func main() {
	fmt.Println("start to do panic")
	doPanic()
}

func doPanic() {
	fmt.Println("hello")
	panic("panic")
}

```

输出结果如下：

```xml
start to do panic
hello
panic: panic

goroutine 1 [running]:
main.doPanic(...)
        /Users/gertieliang/GolandProjects/LearnGoProject/main.go:12
main.main()
        /Users/gertieliang/GolandProjects/LearnGoProject/main.go:7 +0xee
exit status 2
```

程序将退出，并将整个执行调用栈打印出来。若期望程序能够正常运行，可以 recover 进行捕获，使用方式如下：

```go
package main

import "fmt"

func main() {
	fmt.Println("start to do panic")
	doPanic()
}

func doPanic() {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("recover")
		}
	}()
	fmt.Println("hello")
	panic("panic")
}

```

输出结果如下，可发现，程序执行过程中发生异常后被捕获，程序能正常执行。

```xml
start to do panic
hello
recover
```
recover 函数必须结合 defer 使用，未定义在 defer func 里的 recover，将无法捕获异常，示例如下：

```go
package main

import "fmt"

func main() {
	fmt.Println("start to do panic")
	doPanic()
}

func doPanic() {
	if err := recover(); err != nil {
		fmt.Println("recover")
	}
	fmt.Println("hello")
	panic("panic")
}
```

此时程序运行过程中发生 panic, 但并未被 recover 住，程序仍被终止并输出异常调用栈。

```xml
start to do panic
hello
panic: panic

goroutine 1 [running]:
main.doPanic()
        /Users/gertieliang/GolandProjects/LearnGoProject/main.go:15 +0x103
main.main()
        /Users/gertieliang/GolandProjects/LearnGoProject/main.go:7 +0x7a
exit status 2
```

## 4.2. defer 与 panic 的执行顺序
下边是多次 defer 尾随 panic 发生，观察发生 panic 时程序的执行顺序：

```go
package main

import "fmt"

func main() {
	doPanic()
}

func doPanic() {
	defer deferA()
	defer deferB()
	panic("panic")
}

func deferA() {
	fmt.Println("deferA")
}

func deferB() {
	fmt.Println("deferB")
}

```

程序将按 LIFO 的顺序先执行 defer，待 defer 执行完后，打印 panic 关联的堆栈信息。

```xml
deferB
deferA
panic: panic

goroutine 1 [running]:
main.doPanic()
        /Users/gertieliang/GolandProjects/LearnGoProject/main.go:12 +0x97
main.main()
        /Users/gertieliang/GolandProjects/LearnGoProject/main.go:6 +0x20
exit status 2
```

_panic 的结构定义如下：

```go
type _panic struct {
	argp      unsafe.Pointer // 指向 defer 在 panic 期间运行的参数空间
	arg       interface{}    // panic 中的参数
	link      *_panic        // 链接更早的 _panic
	recovered bool           // panic 是否被捕获
	aborted   bool           // panic 被终止
}
```
通过汇编编译得到对应的。o 文件如下：

```xml
TEXT %22%22.doPanic(SB) gofile../Users/gertieliang/GolandProjects/LearnGoProject/main.go
  main.go:9             0xa98                   65488b0c2500000000      MOVQ GS:0, CX           [5:9]R_TLS_LE
  main.go:9             0xaa1                   488d4424f8              LEAQ -0x8(SP), AX
  main.go:9             0xaa6                   483b4110                CMPQ 0x10(CX), AX
  main.go:9             0xaaa                   0f86af000000            JBE 0xb5f
  main.go:9             0xab0                   4881ec88000000          SUBQ $0x88, SP
  main.go:9             0xab7                   4889ac2480000000        MOVQ BP, 0x80(SP)
  main.go:9             0xabf                   488dac2480000000        LEAQ 0x80(SP), BP
  main.go:10            0xac7                   c744244800000000        MOVL $0x0, 0x48(SP)
  main.go:10            0xacf                   488d0500000000          LEAQ 0(IP), AX          [3:7]R_PCREL:%22%22.deferA·f
  main.go:10            0xad6                   4889442460              MOVQ AX, 0x60(SP)
  main.go:10            0xadb                   488d442448              LEAQ 0x48(SP), AX
  main.go:10            0xae0                   48890424                MOVQ AX, 0(SP)
  main.go:10            0xae4                   e800000000              CALL 0xae9              [1:5]R_CALL:runtime.deferprocStack
  main.go:10            0xae9                   85c0                    TESTL AX, AX
  main.go:10            0xaeb                   755c                    JNE 0xb49
  main.go:10            0xaed                   eb00                    JMP 0xaef
  main.go:11            0xaef                   c744241000000000        MOVL $0x0, 0x10(SP)
  main.go:11            0xaf7                   488d0500000000          LEAQ 0(IP), AX          [3:7]R_PCREL:%22%22.deferB·f
  main.go:11            0xafe                   4889442428              MOVQ AX, 0x28(SP)
  main.go:11            0xb03                   488d442410              LEAQ 0x10(SP), AX
  main.go:11            0xb08                   48890424                MOVQ AX, 0(SP)
  main.go:11            0xb0c                   e800000000              CALL 0xb11              [1:5]R_CALL:runtime.deferprocStack
  main.go:11            0xb11                   85c0                    TESTL AX, AX
  main.go:11            0xb13                   751e                    JNE 0xb33
  main.go:11            0xb15                   eb00                    JMP 0xb17
  main.go:12            0xb17                   488d0500000000          LEAQ 0(IP), AX          [3:7]R_PCREL:type.string
  main.go:12            0xb1e                   48890424                MOVQ AX, 0(SP)
  main.go:12            0xb22                   488d0500000000          LEAQ 0(IP), AX          [3:7]R_PCREL:%22%22..stmp_0
  main.go:12            0xb29                   4889442408              MOVQ AX, 0x8(SP)
  main.go:12            0xb2e                   e800000000              CALL 0xb33              [1:5]R_CALL:runtime.gopanic
  main.go:11            0xb33                   90                      NOPL
  main.go:11            0xb34                   e800000000              CALL 0xb39              [1:5]R_CALL:runtime.deferreturn
  main.go:11            0xb39                   488bac2480000000        MOVQ 0x80(SP), BP
  main.go:11            0xb41                   4881c488000000          ADDQ $0x88, SP
  main.go:11            0xb48                   c3                      RET
  main.go:10            0xb49                   90                      NOPL
  main.go:10            0xb4a                   e800000000              CALL 0xb4f              [1:5]R_CALL:runtime.deferreturn
  main.go:10            0xb4f                   488bac2480000000        MOVQ 0x80(SP), BP
  main.go:10            0xb57                   4881c488000000          ADDQ $0x88, SP
  main.go:10            0xb5e                   c3                      RET
  main.go:9             0xb5f                   e800000000              CALL 0xb64              [1:5]R_CALL:runtime.morestack_noctxt
  main.go:9             0xb64                   e92fffffff              JMP %22%22.doPanic(SB)
```

可以发现，编译时会将 panic 关键字转换成 runtime.gopanic 函数。程序发生 panic 后，依次调用 runtime.deferreturn。

查看 `runtime.gopanic` 的内容如下：

```go
func gopanic(e interface{}) {
	gp := getg()
	// 省略一些列操作 ...
        // 创建一个 _panic 对象，并加到 goroutine 的 _panic 中
        var p _panic
	p.arg = e
	p.link = gp._panic
	gp._panic = (*_panic)(noescape(unsafe.Pointer(&p)))

	atomic.Xadd(&runningPanicDefers, 1)
	for {
		d := gp._defer
                // goroutine 的 defer 队列为空，退出
		if d == nil {
			break
		}
                // 如果 defer 运行中
                if d.started {
			if d._panic != nil {
				d._panic.aborted = true
			}
			d._panic = nil
			d.fn = nil
			gp._defer = d.link
			freedefer(d)
			continue
		}
                // 将 goroutine 队头的 _defer 的 started 状态置为 true，并执行调用 _defer 
		d.started = true
		d._panic = (*_panic)(noescape(unsafe.Pointer(&p)))

		p.argp = unsafe.Pointer(getargp(0))
		reflectcall(nil, unsafe.Pointer(d.fn), deferArgs(d), uint32(d.siz), uint32(d.siz))
		p.argp = nil

		// reflectcall did not panic. Remove d.
		if gp._defer != d {
			throw("bad defer entry in panic")
		}
                // 置空当前 _defer, 从 goroutine 的 _defer 中移除，并 free 调
		d._panic = nil
		d.fn = nil
		gp._defer = d.link
		pc := d.pc
		sp := unsafe.Pointer(d.sp) 
		freedefer(d)
                // 如果 _panic 被捕获，则从 goroutine 的 _panic 链表中移除当前 _panic
		if p.recovered {
			atomic.Xadd(&runningPanicDefers, -1)
			gp._panic = p.link
			for gp._panic != nil && gp._panic.aborted {
				gp._panic = gp._panic.link
			}
			if gp._panic == nil { 
				gp.sig = 0
			}
			gp.sigcode0 = uintptr(sp)
			gp.sigcode1 = pc
                        // 调用 recover 内容
			mcall(recovery)
			throw("recovery failed") // mcall should not return
		}
	}
        // 省略一些列操作
}
```

`runtime.gopanic` 内容可以简单概括为执行一下内容：
* 创建 _panic 对象，并加到 goroutine 关联的 _panic 的链表头
* 循环取出 goroutine 关联的 _defer 链表头的 _defer 执行调用，并 free 调用完成的 _defer

## 4.3. defer 过程中发生 panic

在执行 _defer 过程中，程序仍可能发生 panic，下边为在执行 defer 过程中发生 panic 的示例。

```go
package main

import "fmt"

func main() {
	doPanic()
}

func doPanic() {
	defer deferA()
	defer deferB()
	panic("panicM")
}

func deferA() {
	fmt.Println("deferA")
}

func deferB() {
	panic("panicB")
}
```
在 defer 过程中发生 panic，并不会影响程序继续执行下一个 defer 内容，待最后一个 defer 被执行完后，将按 panic 发生的先后顺序，依次打印 panic 情况。

```xml
deferA
panic: panic
        panic: deferB

goroutine 1 [running]:
main.deferB()
        /Users/gertieliang/GolandProjects/LearnGoProject/main.go:20 +0x39
panic(0x10ab020, 0x10e95d0)
        /usr/local/go/src/runtime/panic.go:679 +0x1b2
main.doPanic()
        /Users/gertieliang/GolandProjects/LearnGoProject/main.go:12 +0x97
main.main()
        /Users/gertieliang/GolandProjects/LearnGoProject/main.go:6 +0x20
exit status 2
```
此时程序的运行过程如下：
* 将 panicM 对象加入 goroutine 的 _panic 链表头
* 将 deferB 的 started 状态置为 true, 将 deferB 的 _panic 属性指向当前 panic, 执行调用 deferB
* 调用 deferB 过程中发生 panicB, 将 paincB 插入 goroutine 的 _panic 链表头，并将 panicB.link 执行 panicM
* 取出 goroutine 的 _defer 头，发现此时 panicB 的状态为 started, 将 deferB._panic 即 panicB 的 aborted 状态置为 true, 置空 deferB 并 free
* 继续取出 goroutine 的 _defer 头 deferA, 修改 deferA 的状态为 stated, deferA._panic = panicB, 执行调用 deferA, 待 deferA 调用完成后，free deferA
* 继续取 goroutine 的 _defer 头，发现链表为空，结束 _defer 链表的调用
* 从头到尾，依次打印 goroutine 的 _panic 链表中未被 recoverd 的 panic 的堆栈信息。

## 4.4. 使用 recover 捕获 panic

`runtime.gorecover` 的执行如下所示：
```go
func gorecover(argp uintptr) interface{} {
	gp := getg()
	p := gp._panic
	if p != nil && !p.recovered && argp == uintptr(p.argp) {
		p.recovered = true
		return p.arg
	}
	return nil
}
```

gorecover 并不处理 _panic, 只将对应的 _panic 的 recovered 状态置为 true，具体处理仍交由 gopanic。

recover 未定义在 defer 中，将不能捕获异常，下边未将 recover 置于 defer 中
```go
package main

import "fmt"

func main() {
	doPanic()
}

func doPanic() {
	if err := recover(); err != nil {
		fmt.Println("recover")
	}
	panic("panic")
}

```
生成汇编代码如下：

```xml
TEXT %22%22.doPanic(SB) gofile../Users/gertieliang/GolandProjects/LearnGoProject/main.go
  main.go:9             0x783                   65488b0c2500000000      MOVQ GS:0, CX           [5:9]R_TLS_LE
  main.go:9             0x78c                   483b6110                CMPQ 0x10(CX), SP
  main.go:9             0x790                   0f86bd000000            JBE 0x853
  main.go:9             0x796                   4883ec78                SUBQ $0x78, SP
  main.go:9             0x79a                   48896c2470              MOVQ BP, 0x70(SP)
  main.go:9             0x79f                   488d6c2470              LEAQ 0x70(SP), BP
  main.go:10            0x7a4                   488d842480000000        LEAQ 0x80(SP), AX
  main.go:10            0x7ac                   48890424                MOVQ AX, 0(SP)
  main.go:10            0x7b0                   e800000000              CALL 0x7b5              [1:5]R_CALL:runtime.gorecover
  main.go:10            0x7b5                   488b442408              MOVQ 0x8(SP), AX
  main.go:10            0x7ba                   488b4c2410              MOVQ 0x10(SP), CX
  main.go:10            0x7bf                   4889442438              MOVQ AX, 0x38(SP)
  main.go:10            0x7c4                   48894c2440              MOVQ CX, 0x40(SP)
  main.go:10            0x7c9                   4885c0                  TESTQ AX, AX
  main.go:10            0x7cc                   7502                    JNE 0x7d0
  main.go:10            0x7ce                   eb64                    JMP 0x834
  main.go:11            0x7d0                   0f57c0                  XORPS X0, X0
  main.go:11            0x7d3                   0f11442448              MOVUPS X0, 0x48(SP)
  main.go:11            0x7d8                   488d442448              LEAQ 0x48(SP), AX
  main.go:11            0x7dd                   4889442430              MOVQ AX, 0x30(SP)
  main.go:11            0x7e2                   8400                    TESTB AL, 0(AX)
  main.go:11            0x7e4                   488d0d00000000          LEAQ 0(IP), CX          [3:7]R_PCREL:type.string
  main.go:11            0x7eb                   48894c2448              MOVQ CX, 0x48(SP)
  main.go:11            0x7f0                   488d0d00000000          LEAQ 0(IP), CX          [3:7]R_PCREL:%22%22..stmp_0
  main.go:11            0x7f7                   48894c2450              MOVQ CX, 0x50(SP)
  main.go:11            0x7fc                   8400                    TESTB AL, 0(AX)
  main.go:11            0x7fe                   eb00                    JMP 0x800
  main.go:11            0x800                   4889442458              MOVQ AX, 0x58(SP)
  main.go:11            0x805                   48c744246001000000      MOVQ $0x1, 0x60(SP)
  main.go:11            0x80e                   48c744246801000000      MOVQ $0x1, 0x68(SP)
  main.go:11            0x817                   48890424                MOVQ AX, 0(SP)
  main.go:11            0x81b                   48c744240801000000      MOVQ $0x1, 0x8(SP)
  main.go:11            0x824                   48c744241001000000      MOVQ $0x1, 0x10(SP)
  main.go:11            0x82d                   e800000000              CALL 0x832              [1:5]R_CALL:fmt.Println
  main.go:11            0x832                   eb02                    JMP 0x836
  main.go:10            0x834                   eb00                    JMP 0x836
  main.go:13            0x836                   488d0500000000          LEAQ 0(IP), AX          [3:7]R_PCREL:type.string
  main.go:13            0x83d                   48890424                MOVQ AX, 0(SP)
  main.go:13            0x841                   488d0500000000          LEAQ 0(IP), AX          [3:7]R_PCREL:%22%22..stmp_1
  main.go:13            0x848                   4889442408              MOVQ AX, 0x8(SP)
  main.go:13            0x84d                   e800000000              CALL 0x852              [1:5]R_CALL:runtime.gopanic
  main.go:13            0x852                   90                      NOPL
  main.go:9             0x853                   e800000000              CALL 0x858              [1:5]R_CALL:runtime.morestack_noctxt
  main.go:9             0x858                   e926ffffff              JMP %22%22.doPanic(SB)
```
程序中 runtime.gorecover 的调用在程序发生 panic 之前，当程序发生 panic 后，并无 recover 对进行异常捕获。而是置于 defer 的 recover，将利用 defer 的延迟执行特点，在程序返回前执行调用 recover ，从而成功捕获 panic。