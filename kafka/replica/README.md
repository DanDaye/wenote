# 副本

# 1. 定义

> 副本是分布式系统对数据和服务提供的一种冗余方式

# 2. 作用

1. 解决系统数据丢失问题
2. 对外提供相同的服务，提高并行处理的能力

# 3. 范围界定

1. 副本是相对分区而言
2. 一个分区可能包含多个副本，一个 leader 可以有多个 follower
3. 分区中所有副本称为 AR，与 leader 保持同步状态的副本集合称为 ISR
4. LEO 表示分区最后一条消息的最后一个位置，ISR 中最小的 LEO 即为 HW，俗称高水位，消费者只能拉取到 HW 之前的消息

# 4. 基本概念

## 4.1. 失效副本

### 4.1.1. 定义

失效副本，是在 ISR 集合之外，处于同步失效或功能失效的副本。失效副本对应的分区也称为同步失效分区，即 under-replicated 分区。

### 4.1.2. 判定标准

* 当 ISR 集合中一个 follower 副本滞后 leader 副本的时间超过 `replica.lag.time.max.ms` 指定的值，则认为同步失败。
* 当一个 follower 副本滞后 leader 副本的消息数超过 `replica.lag.max.message` 的大小时，则判定它处于同步失效的状态。broker 级别，对 broker 上所有 topic 都生效。

### 4.1.3. 实现原理

当 follower 副本将 leader 副本 LEO 之前的日志全部同步时，则认为该 follower 副本已经追赶上 leader 副本，此时更新副本的 lastCaughtUpTimeMS 标识。
kafka 副本管理器会启动一个副本过期检测的定时任务，而这个定时任务会定时检查当前时间与副本 lastCaughtUpTimeMS 的差值是否大于参数 `replica.lag.time.max.ms`.

### 4.1.4. 失效情况

* follower 副本进程卡主，在一段时间内根本没有向 leader 副本发起同步请求，比如频繁的 full GC 
* follower 副本进程同步过慢，在一段时间内都无法追赶上 leader 副本，比如 I/O 开销过大

## 4.2. ISR的伸缩

* `isr-expiration`:周期性地检测每个分区是否需要缩减其 ISR 集合，大小是 `replica.lag.time.max.ms` 参数的一半，默认值为 5000ms.
* `isr-change-propagation`:周期性地检查 isrChangeSet，如果发现 isrChangeSet 中有 ISR 集合的biang记录，

## 4.3. LEO 与 HW

* 本地副本：对应的 Log 分配在当前的 broker 节点上

* 远程副本：对应的 Log 分配在其他的 broker 节点上

## 4.4. Leader Epoch

> kafka 使用的是基于 HW 的同步机制，可能出现数据丢失或 leader 副本和 follower 副本数据不一致的问题。

### 4.4.1. 数据丢失场景

1. 

# 5. 读写分离





