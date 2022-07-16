# 浅析Go 依赖和版本控制 go.mod 和 go.sum 

[TOC]

# 1. go.mod

> 用来标记一个 Module 并管理 Module 的依赖库以及依赖库的版本

## 1.1. module path

一般采用 "仓库 + module name" 的方式定义，在获取一个 module 时，可以到它的仓库中去查询，或者让 go proxy 到仓库中去查询

```xml
module https://github.com/DanDaye/wenote/go/basic/versioncontrol
```

如果 module 的 版本已经 >= 2.0.0 ，按 Go 的规范，要加上 major 后缀，module path 格式修改如下：

```xml
module https://github.com/DanDaye/wenote/go/basic/versioncontrol/v2

module https://github.com/DanDaye/wenote/go/basic/versioncontrol/v3
```

引用的时候要加上v2、vx后缀，便于和其它 major 版本进行区分。可以同时依赖库的不同的major 版本。


## 1.2. go directive

指明当前module的代码所需要的 Go 的最低版本，由于 go 的标准库在不断更新，会有新的 API 不断地加进来，此时仍可能需要指明它依赖的 Go 的版本，此行非必须。

```xml
go 1.17
```

## 1.3. require 

当前 Module 所依赖的库以及它们的版本。

版本号格式遵循：[语义化版本2.0.0](https://semver.bootcss.com/)

![20220716143136](https://cdn.jsdelivr.net/gh/DanDaye/wenote/go/source/picture/20220716143136.png)


正常版本号： v0.0.0
伪版本号：v0.0.0 - yyyyMMddhhmmss - commit id, 实际上库并没有发布这个版本


## 1.4. indirect 注释

三种情况会出现 indirect 注释

* 当前 Module 依赖于 A，A 依赖于 B，但 B 没有出现在 A 的 go.mod 中，则会在当前 Module 的 go.mod 通过 indirect 方式引入
* 当前 Module 依赖于 A，但 A 没有 go.mod ,则 补充 A 的到 当前 Module 的 indirect 中
* 当前 Module 依赖于 A，A 依赖于 B， 当 A 发生降级后不再依赖 B ，补充 B 到当前 Module 的 go.mod 中

## 1.5. incompatible

当依赖版本 >= 2 却没有按照 go module 的规范 在 module path 尾加上 /v2 版本信息，表示依赖的版本不符合 go module 版本规范

## 1.6. exclude 注释

想在项目中跳过某个依赖库的某个版本

```xml
exclude (
    go.etcd.io/etcd/client/v2 v2.305.0-rc.0
)
```

这样 Go 在版本选择的时候，会主动跳过这些版本。

## 1.7. replace

用来解决一些错误的依赖库的引用或者调试依赖库

replace (
    github.com/coreos/bbolt => go.etcd.io/bbolt v1.3.3
    github.com/coreos/bbolt => ../rrr
)

## 1.8. retract

宣布撤回库的某个版本。在误发布了某个版本，或者事后发现某个版本不成熟，可以推出一个新的版本，在新的版本中，声明前面的某个版本被撤回，提示大家都不要用了。
撤回的版本 tag 依然还存在，go proxy 也存在这个版本，如果强制使用，还是可以使用的，否则这些版本就会被跳过。

和 exclude 的区别是，retract 是这个库的 owner 定义的，而 exclude 是库的使用者在自己的 go.mod 中定义的。

# 2. go.sum

用于校验所依赖 Module 的校验信息，防止下载依赖被随意篡改，用于安全校验。
