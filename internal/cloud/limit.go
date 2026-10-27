package cloud

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// Limiter 按账号串行限速。不同账号互不等待。
type Limiter struct {
	mu    sync.Mutex
	next  map[string]time.Time
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
	qps   func() int
}

func NewLimiter(qps func() int) *Limiter {
	if qps == nil {
		qps = func() int { return 5 }
	}
	return &Limiter{
		next:  map[string]time.Time{},
		now:   time.Now,
		sleep: sleepCtx,
		qps:   qps,
	}
}

var defaultLimiter = NewLimiter(nil)

func DefaultLimiter() *Limiter { return defaultLimiter }

func (l *Limiter) SetQPS(fn func() int) {
	if l == nil || fn == nil {
		return
	}
	l.qps = fn
}

func (l *Limiter) Wait(ctx context.Context, accountID string) error {
	if l == nil {
		return nil
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil
	}
	qps := 5
	if l.qps != nil {
		if n := l.qps(); n > 0 {
			qps = n
		}
	}
	gap := time.Second / time.Duration(qps)
	l.mu.Lock()
	now := l.now()
	at := l.next[accountID]
	if at.Before(now) {
		at = now
	}
	l.next[accountID] = at.Add(gap)
	l.mu.Unlock()
	wait := at.Sub(now)
	if wait <= 0 {
		return nil
	}
	return l.sleep(ctx, wait)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// RetryRead 只重试只读查询。变更提交不能调用它。
func RetryRead(ctx context.Context, sleep func(context.Context, time.Duration) error, fn func() error) error {
	if sleep == nil {
		sleep = sleepCtx
	}
	delay := time.Second
	var err error
	for i := 0; i < 5; i++ {
		err = fn()
		if err == nil || !IsRateLimit(err) {
			return err
		}
		if i == 4 {
			break
		}
		jitter := time.Duration(rand.Intn(200)) * time.Millisecond
		if e := sleep(ctx, delay+jitter); e != nil {
			return e
		}
		delay *= 2
	}
	return err
}

func IsRateLimit(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRateLimit) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "RequestLimitExceeded") || strings.Contains(s, "RateLimit")
}
