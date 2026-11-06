package cloud

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNextWindowFirstAndOverlap(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	first := NextWindow(time.Time{}, now, 5*time.Minute, time.Minute)
	if !first.From.Equal(now.Add(-5*time.Minute)) || !first.To.Equal(now) {
		t.Fatalf("first window: %+v", first)
	}
	last := now.Add(-5 * time.Minute)
	w := NextWindow(last, now, 5*time.Minute, time.Minute)
	if !w.From.Equal(last.Add(-time.Minute)) {
		t.Fatalf("overlap from %s", w.From)
	}
}

func TestSplitAndCursor(t *testing.T) {
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	w := Window{From: start, To: start.Add(50 * time.Hour)}
	parts := SplitWindow(w, 24*time.Hour)
	if len(parts) != 3 {
		t.Fatalf("parts %d", len(parts))
	}
	cur := time.Time{}
	cur = AdvanceCursor(cur, parts[0], true)
	if !cur.Equal(parts[0].To) {
		t.Fatal("cursor should move after success")
	}
	stuck := AdvanceCursor(cur, parts[1], false)
	if !stuck.Equal(cur) {
		t.Fatal("failure must not advance cursor")
	}
}

func TestFingerprintDedupe(t *testing.T) {
	a := Fingerprint("SELECT  1")
	b := Fingerprint("select 1")
	if a != b {
		t.Fatal("normalized sql should share fingerprint")
	}
}

func TestPickPGIntranetPrefersInner(t *testing.T) {
	nets := []any{
		map[string]any{"NetType": "public", "Ip": "1.2.3.4", "Port": float64(5432)},
		map[string]any{"NetType": "inner", "Ip": "", "Address": "pg.bj.restosuite.shop", "Port": float64(5432)},
	}
	host, port := PickPGIntranet(nets)
	if host != "pg.bj.restosuite.shop" || port != 5432 {
		t.Fatalf("host=%s port=%d", host, port)
	}
	if h, _ := PickPGIntranet([]any{map[string]any{"NetType": "2", "Ip": "8.8.8.8"}}); h != "" {
		t.Fatalf("public used: %s", h)
	}
}

func TestMatchPlatformIDs(t *testing.T) {
	items := []PlatformRef{
		{ID: "a", Host: "pg.bj.restosuite.shop, 10.0.0.8", DBType: "PostgreSQL"},
		{ID: "b", Host: "PG.BJ.RESTOSUITE.SHOP", DBType: "postgres"},
		{ID: "c", Host: "pg.bj.restosuite.shop", DBType: "mysql"},
	}
	one := MatchPlatformIDs("pg.bj.restosuite.shop", EnginePostgres, items[:1])
	if len(one) != 1 || one[0] != "a" {
		t.Fatalf("unique %#v", one)
	}
	many := MatchPlatformIDs("pg.bj.restosuite.shop", EnginePostgres, items)
	if len(many) != 2 {
		t.Fatalf("many %#v", many)
	}
	if ids := MatchPlatformIDs("", EnginePostgres, items); len(ids) != 0 {
		t.Fatalf("empty %#v", ids)
	}
}

func TestFillFromInventory(t *testing.T) {
	rows := []InventoryRef{
		{ID: "r1", CloudID: "postgres-7ajcrtsc", Vendor: "tencent", Region: "ap-beijing", Intranet: "pg.bj.restosuite.shop"},
		{ID: "r2", CloudID: "postgres-other", Vendor: "tencent", Region: "ap-shanghai", Intranet: "10.1.1.1"},
	}
	got := FillFromInventory("pg.bj.restosuite.shop", "", "", "", rows)
	if !got.OK || got.Vendor != "tencent" || got.CloudID != "postgres-7ajcrtsc" || got.Region != "ap-beijing" || len(got.ResourceIDs) != 1 {
		t.Fatalf("host fill %#v", got)
	}
	keep := FillFromInventory("10.1.1.1", "tencent", "postgres-7ajcrtsc", "", rows)
	if !keep.OK || keep.CloudID != "postgres-7ajcrtsc" || keep.Region != "ap-beijing" {
		t.Fatalf("cloud id wins %#v", keep)
	}
	clash := FillFromInventory("10.1.1.1", "", "postgres-missing", "ap-beijing", rows)
	if clash.OK || clash.CloudID != "" {
		t.Fatalf("mismatch should not write %#v", clash)
	}
	kept := FillFromInventory("pg.bj.restosuite.shop", "aliyun", "", "cn-beijing", rows)
	if kept.Vendor != "aliyun" || kept.CloudID != "postgres-7ajcrtsc" || kept.Region != "cn-beijing" {
		t.Fatalf("non-empty kept %#v", kept)
	}
	dup := []InventoryRef{
		{ID: "r1", CloudID: "postgres-a", Intranet: "10.0.0.1"},
		{ID: "r2", CloudID: "postgres-b", Intranet: "10.0.0.1"},
	}
	if many := FillFromInventory("10.0.0.1", "", "", "", dup); many.OK {
		t.Fatalf("ambiguous %#v", many)
	}
}

func TestDecideBind(t *testing.T) {
	if DecideBind("", "a1", 1) != BindFill {
		t.Fatal("empty should fill")
	}
	if DecideBind("a1", "a2", 1) != BindConflict {
		t.Fatal("existing bind must not be overwritten")
	}
	if DecideBind("", "a1", 2) != BindSkip {
		t.Fatal("multiple matches skip")
	}
}

func TestDiskAndApproval(t *testing.T) {
	if err := CheckDisk(ActionScaleDisk, 100, 80, false); err == nil {
		t.Fatal("shrink should fail")
	}
	if err := CheckDisk(ActionScaleDisk, 100, 120, false); err != nil {
		t.Fatal(err)
	}
	if (Approval{}).Valid() {
		t.Fatal("empty approval")
	}
	if !(Approval{By: "admin", At: "t", Opinion: "ok"}).Valid() {
		t.Fatal("approval should be valid")
	}
}

func TestHealGuards(t *testing.T) {
	p := Policy{Enabled: true, Action: ActionScaleSpec, CapCPU: 8, CapMemoryMB: 16384, CooldownMin: 30, DailyMax: 1, Mode: "ticket"}
	now := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
	if ok, _ := HealDecision(p, now, now.Add(-10*time.Minute), 0, false, 4, 8192, 8, 16384, 100, 100); ok {
		t.Fatal("cooldown")
	}
	if ok, reason := HealDecision(p, now, time.Time{}, 0, false, 8, 16384, 4, 8192, 100, 100); ok || reason != "自动自愈不允许降配" {
		t.Fatalf("downgrade: %v %s", ok, reason)
	}
	if ok, _ := HealDecision(p, now, time.Time{}, 1, false, 4, 8192, 8, 16384, 100, 100); ok {
		t.Fatal("daily cap")
	}
	p.Action = ActionSwitch
	if ok, reason := HealDecision(p, now, time.Time{}, 0, true, 0, 0, 0, 0, 0, 0); ok || reason == "" {
		t.Fatalf("recent switch: %v %s", ok, reason)
	}
	p.Action = ActionRestart
	if ok, reason := HealDecision(p, now, time.Time{}, 0, false, 0, 0, 0, 0, 0, 0); !ok {
		t.Fatal(reason)
	}
}

func TestRetryDoesNotHideOtherErrors(t *testing.T) {
	n := 0
	err := RetryRead(t.Context(), func(context.Context, time.Duration) error { return nil }, func() error {
		n++
		return errors.New("boom")
	})
	if err == nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestRetryRateLimit(t *testing.T) {
	n := 0
	err := RetryRead(t.Context(), func(context.Context, time.Duration) error { return nil }, func() error {
		n++
		if n < 3 {
			return ErrRateLimit
		}
		return nil
	})
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestLimiterSerialPerAccount(t *testing.T) {
	var slept time.Duration
	now := time.Now()
	l := &Limiter{
		next: map[string]time.Time{},
		now:  func() time.Time { return now },
		sleep: func(_ context.Context, d time.Duration) error {
			slept += d
			return nil
		},
		qps: func() int { return 5 },
	}
	if err := l.Wait(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if err := l.Wait(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if slept < 200*time.Millisecond {
		t.Fatalf("same account should wait, slept %s", slept)
	}
	slept = 0
	if err := l.Wait(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}
	if slept != 0 {
		t.Fatalf("other account should not wait, slept %s", slept)
	}
}
