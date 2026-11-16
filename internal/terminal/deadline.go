package terminal

import (
	"sync"
	"time"
)

const persistEvery = time.Minute

// sessionDeadline 空闲窗口会在有输入或输出时往后推，但不会超过绝对上限。
type sessionDeadline struct {
	idle     time.Duration
	absolute time.Time

	mu       sync.Mutex
	idleAt   time.Time
	lastSave time.Time
	ping     chan struct{}
}

func newSessionDeadline(now time.Time, idle time.Duration, absolute time.Time) *sessionDeadline {
	if idle <= 0 {
		idle = 1800 * time.Second
	}
	if !absolute.After(now) {
		absolute = now.Add(idle)
	}
	idleAt := now.Add(idle)
	if idleAt.After(absolute) {
		idleAt = absolute
	}
	return &sessionDeadline{
		idle:     idle,
		absolute: absolute,
		idleAt:   idleAt,
		ping:     make(chan struct{}, 1),
	}
}

// Touch 记录一次活动（键盘输入或终端输出）。persist 为真时调用方应把 expires 写入库，最多约每分钟一次。
func (d *sessionDeadline) Touch(now time.Time) (expires time.Time, persist bool) {
	if d == nil {
		return now, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	exp := now.Add(d.idle)
	if exp.After(d.absolute) {
		exp = d.absolute
	}
	d.idleAt = exp
	select {
	case d.ping <- struct{}{}:
	default:
	}
	if d.lastSave.IsZero() || now.Sub(d.lastSave) >= persistEvery {
		d.lastSave = now
		return exp, true
	}
	return exp, false
}

func (d *sessionDeadline) IdleAt() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.idleAt
}

func (d *sessionDeadline) IdleDue(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !now.Before(d.idleAt)
}

func (d *sessionDeadline) AbsoluteDue(now time.Time) bool {
	return d != nil && !now.Before(d.absolute)
}
