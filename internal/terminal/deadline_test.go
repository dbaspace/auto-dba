package terminal

import (
	"testing"
	"time"
)

func TestTouchResetsIdleUntilAbsolute(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	idle := 30 * time.Minute
	abs := start.Add(8 * time.Hour)
	d := newSessionDeadline(start, idle, abs)

	// 刚打开、还在 psql 里操作时不应判空闲。
	if d.IdleDue(start.Add(29 * time.Minute)) {
		t.Fatal("activity window should still be open before 30m")
	}
	if !d.IdleDue(start.Add(30 * time.Minute)) {
		t.Fatal("idle at 30m without input or output")
	}

	// psql 敲 SQL 或结果回显都走 Touch，空闲窗口从这次活动重算。
	later := start.Add(29 * time.Minute)
	exp, persist := d.Touch(later)
	if !persist {
		t.Fatal("first activity should persist expiry")
	}
	if exp.Before(later.Add(30*time.Minute)) || exp.After(abs) {
		t.Fatalf("expires=%s", exp)
	}
	if d.IdleDue(later.Add(29 * time.Minute)) {
		t.Fatal("sql input should push the idle deadline")
	}
	if !d.IdleDue(later.Add(30 * time.Minute)) {
		t.Fatal("idle 30m after the last psql output")
	}

	if _, persist := d.Touch(later.Add(10 * time.Second)); persist {
		t.Fatal("expiry writes are throttled to about once a minute")
	}
	if _, persist := d.Touch(later.Add(time.Minute)); !persist {
		t.Fatal("expiry should persist again after a minute")
	}

	near := abs.Add(-time.Second)
	exp, _ = d.Touch(near)
	if exp.After(abs) {
		t.Fatalf("activity must not extend past absolute cap, got %s", exp)
	}
	if !d.AbsoluteDue(abs) {
		t.Fatal("absolute cap reached")
	}
	if !d.IdleDue(abs) {
		t.Fatal("idle deadline is capped by the absolute end")
	}
}
