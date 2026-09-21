package cdc

import "fmt"

// shardFanoutMaxConcurrency 同一批分库编排的并发。
// 必须为 1：这些库共享同一 Kafka brokers / 源 PG，编排时抢同一把资源锁。
// 并发大于 1 时，后启动的库会在兄弟任务持锁期间空转，超过
// sync_orchestrate_lock_wait_sec（默认 1800s）即失败，Publication/Slot 都不会创建。
// 不在这里放宽全局锁等待。
const shardFanoutMaxConcurrency = 1

// runShardStepsSerial 按顺序执行分库步骤。上一个返回后才开始下一个，不并行。
func runShardStepsSerial(n int, step func(i int) error) error {
	if shardFanoutMaxConcurrency != 1 {
		return fmt.Errorf("shard fanout concurrency must be 1, got %d", shardFanoutMaxConcurrency)
	}
	if n < 0 {
		return fmt.Errorf("negative shard step count")
	}
	for i := 0; i < n; i++ {
		if err := step(i); err != nil {
			return err
		}
	}
	return nil
}
