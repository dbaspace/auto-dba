package cdc

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const orchLockWait = 1800 * time.Second

var orchResourceLocks sync.Map // key -> *sync.Mutex

type orchLockCtxKey struct{}

func acquireOrchLock(ctx context.Context, key string) (func(), error) {
	v, _ := orchResourceLocks.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	done := make(chan struct{})
	go func() {
		mu.Lock()
		close(done)
	}()
	timer := time.NewTimer(orchLockWait)
	defer timer.Stop()
	select {
	case <-done:
		return mu.Unlock, nil
	case <-ctx.Done():
		go func() {
			<-done
			mu.Unlock()
		}()
		return nil, ctx.Err()
	case <-timer.C:
		go func() {
			<-done
			mu.Unlock()
		}()
		return nil, fmt.Errorf("等待编排资源锁超时（%s，%s）", key, orchLockWait)
	}
}

func normalizeBrokerLockKey(brokers string) string {
	parts := strings.Split(brokers, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// withOrchestrateLocks 同一 Kafka brokers 或同一源实例的编排排队执行。
func (o *Orchestrator) withOrchestrateLocks(ctx context.Context, sourceID, brokers string, fn func(context.Context) error) error {
	if ctx.Value(orchLockCtxKey{}) != nil {
		return fn(ctx)
	}
	var keys []string
	if id := strings.TrimSpace(sourceID); id != "" {
		keys = append(keys, "pg:"+id)
	}
	if b := strings.TrimSpace(brokers); b != "" {
		keys = append(keys, "kafka:"+normalizeBrokerLockKey(b))
	}
	sort.Strings(keys)
	var unlocks []func()
	for _, k := range keys {
		u, err := acquireOrchLock(ctx, k)
		if err != nil {
			for i := len(unlocks) - 1; i >= 0; i-- {
				unlocks[i]()
			}
			return err
		}
		unlocks = append(unlocks, u)
	}
	defer func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}()
	return fn(context.WithValue(ctx, orchLockCtxKey{}, true))
}
