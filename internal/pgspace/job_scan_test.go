package pgspace

import (
	"errors"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestResolveCollectScan(t *testing.T) {
	mode, db := resolveCollectScan("__cluster__", true)
	if mode != scanNone || db != "" {
		t.Fatalf("cluster: %v %q", mode, db)
	}
	mode, db = resolveCollectScan("__all__", false)
	if mode != scanAll || db != "" {
		t.Fatalf("all: %v %q", mode, db)
	}
	mode, db = resolveCollectScan(" orders ", true)
	if mode != scanOne || db != "orders" {
		t.Fatalf("one: %v %q", mode, db)
	}
	mode, _ = resolveCollectScan("", true)
	if mode != scanAll {
		t.Fatalf("default all: %v", mode)
	}
	mode, _ = resolveCollectScan("", false)
	if mode != scanNone {
		t.Fatalf("default none: %v", mode)
	}
}

func TestCollectFinish(t *testing.T) {
	st, msg := collectFinish(true, true, errors.New("x"), "10.0.0.1", "app")
	if st != store.SpaceJobCanceled || msg != "" {
		t.Fatalf("cancel wins: %s %q", st, msg)
	}
	st, msg = collectFinish(false, true, errors.New("context deadline exceeded"), "10.0.0.1", "app")
	if st != store.SpaceJobFailed || !strings.Contains(msg, "10.0.0.1") || !strings.Contains(msg, "app") {
		t.Fatalf("timeout both: %s %q", st, msg)
	}
	st, msg = collectFinish(false, true, nil, "", "app")
	if st != store.SpaceJobFailed || msg != "采集超时，停在库 app" {
		t.Fatalf("timeout db: %s %q", st, msg)
	}
	st, msg = collectFinish(false, true, nil, "10.0.0.1", "")
	if msg != "采集超时，停在 10.0.0.1" {
		t.Fatalf("timeout inst: %q", msg)
	}
	st, msg = collectFinish(false, true, nil, "", "")
	if msg != "采集超时" {
		t.Fatalf("timeout bare: %q", msg)
	}
	st, msg = collectFinish(false, false, errors.New("boom"), "", "")
	if st != store.SpaceJobFailed || msg != "boom" {
		t.Fatalf("run err: %s %q", st, msg)
	}
	st, msg = collectFinish(false, false, nil, "", "")
	if st != store.SpaceJobSucceeded || msg != "" {
		t.Fatalf("ok: %s %q", st, msg)
	}
}

func TestInstanceCollectTimeout(t *testing.T) {
	if instanceCollectTimeout != 3*time.Minute {
		t.Fatalf("timeout = %s", instanceCollectTimeout)
	}
}
