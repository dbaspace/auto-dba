package jobreg

import (
	"testing"
	"time"
)

func TestDueNowInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	fk, due := dueNow(ScheduleInterval, "100s", now)
	if !due || fk != "i:10" {
		t.Fatalf("got due=%v key=%s", due, fk)
	}
}

func TestDueNowCron6(t *testing.T) {
	now := time.Date(2026, 10, 20, 3, 0, 15, 0, time.Local)
	fk, due := dueNow(ScheduleCron6, "0 0 3 * * *", now)
	if !due || fk != "d:2026-10-20" {
		t.Fatalf("got due=%v key=%s", due, fk)
	}
	_, due2 := dueNow(ScheduleCron6, "0 0 3 * * *", now.Add(time.Hour))
	if due2 {
		t.Fatal("expected not due")
	}
}

func TestMissedFireKey(t *testing.T) {
	// 今天 10:00，计划点 03:00 已过
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.Local)
	fk, ok := missedFireKey(ScheduleCron6, "0 0 3 * * *", now, 24*time.Hour)
	if !ok || fk != "d:2026-10-20" {
		t.Fatalf("got ok=%v key=%s", ok, fk)
	}
}

func TestParseDurationSpec(t *testing.T) {
	d, err := parseDurationSpec("30")
	if err != nil || d != 30*time.Second {
		t.Fatalf("got %v %v", d, err)
	}
}
