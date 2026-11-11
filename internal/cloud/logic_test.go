package cloud

import (
	"context"
	"errors"
	"strings"
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

func TestSlowSyncCooling(t *testing.T) {
	now := time.Date(2026, 11, 10, 11, 30, 0, 0, time.UTC)
	if !SlowSyncCooling(now.Add(-4*time.Minute).Format(time.RFC3339), now) {
		t.Fatal("4 minutes should still be cooling")
	}
	if SlowSyncCooling(now.Add(-5*time.Minute).Format(time.RFC3339), now) {
		t.Fatal("5 minutes should allow another sync")
	}
	if SlowSyncCooling("", now) {
		t.Fatal("missing cursor should allow the first sync")
	}
}

func TestClampSlowWindowKeepsAPIStartInsideRetention(t *testing.T) {
	now := time.Date(2026, 11, 10, 11, 16, 3, 0, time.UTC)
	wide := NextWindow(time.Time{}, now, 15*24*time.Hour, time.Minute)
	got := ClampSlowWindow(wide, now)
	floor := now.Add(-SlowLogAPILookback)
	if got.From.Before(floor) || !got.To.Equal(now) {
		t.Fatalf("clamped window %+v floor %s", got, floor)
	}
	if !got.From.Equal(floor) {
		t.Fatalf("15 day start was not pulled forward: %s", got.From)
	}
	last := now.Add(-time.Hour)
	inc := ClampSlowWindow(NextWindow(last, now, 15*24*time.Hour, time.Minute), now)
	if !inc.From.Equal(last.Add(-time.Minute)) {
		t.Fatalf("incremental start changed: %s", inc.From)
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

func TestNetFromExtras(t *testing.T) {
	host, port := NetFromExtras(EnginePostgres, map[string]any{
		"DBInstanceNetInfo": []any{
			map[string]any{"NetType": "public", "Ip": "1.2.3.4", "Port": float64(5432)},
			map[string]any{"NetType": "inner", "Ip": "10.0.0.8", "Port": float64(5432)},
		},
	})
	if host != "10.0.0.8" || port != 5432 {
		t.Fatalf("pg %s %d", host, port)
	}
	host, port = NetFromExtras(EngineMySQL, map[string]any{"Vip": "10.1.1.1", "Vport": float64(3306)})
	if host != "10.1.1.1" || port != 3306 {
		t.Fatalf("mysql %s %d", host, port)
	}
	host, port = NetFromExtras(EngineRedis, map[string]any{"WanIp": "10.2.2.2", "Port": float64(6379)})
	if host != "10.2.2.2" || port != 6379 {
		t.Fatalf("redis %s %d", host, port)
	}
	host, port = NetFromExtras(EngineHost, map[string]any{"PrivateIpAddresses": []any{"10.3.3.3", "10.3.3.4"}})
	if host != "10.3.3.3" || port != 0 {
		t.Fatalf("host %s %d", host, port)
	}
	if h, p := NetFromExtras(EnginePostgres, nil); h != "" || p != 0 {
		t.Fatalf("empty %s %d", h, p)
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

func TestShouldFallbackTenant(t *testing.T) {
	miss := CloudFill{Message: MsgNoCloudResource}
	if !ShouldFallbackTenant("tenant-sales", "tenant-default", miss) {
		t.Fatal("other tenant should fall back when nothing matched")
	}
	if ShouldFallbackTenant("tenant-default", "tenant-default", miss) {
		t.Fatal("default tenant should not fall back to itself")
	}
	if ShouldFallbackTenant("", "tenant-default", miss) {
		t.Fatal("empty tenant already searched without a tenant filter")
	}
	hit := CloudFill{OK: true, CloudID: "postgres-a"}
	if ShouldFallbackTenant("tenant-sales", "tenant-default", hit) {
		t.Fatal("a hit in the instance tenant must win")
	}
	many := CloudFill{Message: MsgManyResources}
	if ShouldFallbackTenant("tenant-sales", "tenant-default", many) {
		t.Fatal("ambiguous host must not switch tenant")
	}
	own := []InventoryRef{{ID: "own", CloudID: "postgres-own", Intranet: "10.1.1.1", Vendor: "tencent", Region: "ap-beijing"}}
	def := []InventoryRef{{ID: "def", CloudID: "postgres-def", Intranet: "10.128.19.62", Vendor: "tencent", Region: "ap-beijing"}}
	first := FillFromInventory("10.128.19.62", "", "", "", own)
	if !ShouldFallbackTenant("tenant-sales", "tenant-default", first) {
		t.Fatal(first.Message)
	}
	second := FillFromInventory("10.128.19.62", "", "", "", def)
	if !second.OK || second.CloudID != "postgres-def" {
		t.Fatalf("default tenant fill %#v", second)
	}
}

func TestReconcileConflictNote(t *testing.T) {
	got := ReconcileConflictNote("resto-专属", "旧账号", 1)
	if got != "云账号不一致：实例是 旧账号，清单是 resto-专属" {
		t.Fatal(got)
	}
	dup := ReconcileConflictNote("resto-专属", "旧账号", 2)
	if !strings.Contains(dup, "多个云账号") || !strings.Contains(dup, "旧账号") || !strings.Contains(dup, "resto-专属") {
		t.Fatal(dup)
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

func TestCheckTargetSpec(t *testing.T) {
	if err := CheckTargetSpec(ActionScaleSpec, 8, 16384, 100, 0, 0, 100, false); err == nil {
		t.Fatal("zero target spec should fail")
	}
	if err := CheckTargetSpec(ActionScaleSpec, 8, 16384, 100, 4, 8192, 100, false); err == nil {
		t.Fatal("smaller spec should fail")
	}
	if err := CheckTargetSpec(ActionScaleSpec, 8, 16384, 100, 16, 32768, 100, false); err != nil {
		t.Fatal(err)
	}
	if err := CheckTargetSpec(ActionScaleDisk, 8, 16384, 3980, 0, 0, 0, false); err == nil {
		t.Fatal("zero disk should fail")
	}
	if err := CheckSwitch(EnginePostgres, ActionScaleSpec, 1, "", ""); err == nil {
		t.Fatal("specified switch needs a window")
	}
	if err := CheckSwitch(EnginePostgres, ActionScaleSpec, 1, "06:00:00", "02:00:00"); err == nil {
		t.Fatal("switch start must be earlier")
	}
	if err := CheckSwitch(EnginePostgres, ActionScaleDisk, 1, "02:00:00", "06:00:00"); err != nil {
		t.Fatal(err)
	}
	if err := CheckSwitch(EnginePostgres, ActionScaleSpec, 0, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := CheckSwitch(EnginePostgres, ActionScaleSpec, 2, "", ""); err != nil {
		t.Fatal(err)
	}
	if SwitchText(0, "", "") != "升级完成时" || SwitchText(2, "", "") != "维护时间内" || SwitchText(1, "02:00:00", "06:00:00") != "指定时间 02:00:00–06:00:00" {
		t.Fatal("switch text")
	}
	rebind := Payload{Action: ActionRebind, BeforeCPU: 8, BeforeMem: 16, BeforeDisk: 3980}
	CopyCurrentSpec(&rebind)
	if rebind.CPU != 8 || rebind.MemoryMB != 16 || rebind.DiskGB != 3980 {
		t.Fatalf("rebind should keep current spec: %+v", rebind)
	}
}

func TestCheckSpecStorage(t *testing.T) {
	specs := []Spec{
		{CPU: 4, MemoryMB: 8192, MaxStorageGB: 3000},
		{CPU: 4, MemoryMB: 16384, MaxStorageGB: 6000},
	}
	if MaxStorageGB(specs, 4, 8192) != 3000 || MaxStorageGB(specs, 8, 16384) != 0 {
		t.Fatal("max storage lookup")
	}
	disk := DiskForStorageCheck(ActionScaleSpec, 0, 3500)
	if err := CheckSpecStorage(disk, MaxStorageGB(specs, 4, 8192)); err == nil || err.Error() != "当前磁盘 3500GB，该规格最大支持 3000GB" {
		t.Fatal(err)
	}
	if err := CheckSpecStorage(920, 3000); err != nil {
		t.Fatal(err)
	}
	if err := CheckSpecStorage(3500, 0); err != nil {
		t.Fatal(err)
	}
	if DiskForStorageCheck(ActionScaleDisk, 4000, 920) != 4000 {
		t.Fatal("disk expansion should use the target size")
	}
}

func TestScaleDeltaOnlyChangedFields(t *testing.T) {
	spec, disk, err := ScaleDelta(EnginePostgres, ActionScaleSpec, 1, 2048, 500, 0, 0, 800, false)
	if err != nil || spec || !disk {
		t.Fatalf("disk only: spec=%v disk=%v err=%v", spec, disk, err)
	}
	in := SpecForAPI(Payload{Engine: EnginePostgres, Action: ActionScaleSpec, CloudID: "pg-1", DiskGB: 800, SwitchTag: 2}, spec, disk)
	if in.CPU != 0 || in.MemoryMB != 0 || in.DiskGB != 800 || in.SwitchTag != 2 {
		t.Fatalf("%+v", in)
	}
	spec, disk, err = ScaleDelta(EnginePostgres, ActionScaleSpec, 1, 2048, 500, 2, 4096, 500, false)
	if err != nil || !spec || disk {
		t.Fatalf("spec only: spec=%v disk=%v err=%v", spec, disk, err)
	}
	in = SpecForAPI(Payload{Engine: EnginePostgres, Action: ActionScaleSpec, CPU: 2, MemoryMB: 4096, DiskGB: 500}, spec, disk)
	if in.CPU != 2 || in.MemoryMB != 4096 || in.DiskGB != 0 {
		t.Fatalf("%+v", in)
	}
	spec, disk, err = ScaleDelta(EnginePostgres, ActionScaleSpec, 1, 2048, 500, 2, 4096, 800, false)
	if err != nil || !spec || !disk {
		t.Fatalf("both: spec=%v disk=%v err=%v", spec, disk, err)
	}
	if _, _, err = ScaleDelta(EnginePostgres, ActionScaleSpec, 4, 8192, 500, 4, 8192, 500, false); err == nil || err.Error() != "没有需要执行的变更" {
		t.Fatal(err)
	}
	if _, _, err = ScaleDelta(EnginePostgres, ActionScaleSpec, 4, 8192, 500, 4, 0, 800, false); err == nil || err.Error() != "请同时填写核数和内存" {
		t.Fatal(err)
	}
	if _, _, err = ScaleDelta(EngineMySQL, ActionScaleSpec, 4, 8192, 500, 2, 4096, 500, false); err == nil || err.Error() != "目标规格不能小于当前规格" {
		t.Fatal(err)
	}
	blip, text := ImpactFor(Payload{Engine: EnginePostgres, Action: ActionScaleSpec, BeforeCPU: 1, BeforeMem: 2048, BeforeDisk: 500, DiskGB: 800}, false)
	if blip || text != "只扩容磁盘" {
		t.Fatalf("blip=%v text=%s", blip, text)
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

func TestValidateCreatePostgres(t *testing.T) {
	ok := Payload{
		Action: ActionCreate, Engine: EnginePostgres, Region: "ap-beijing",
		Zone: "ap-beijing-6", StandbyZone: "ap-beijing-7",
		VpcID: "vpc-1", SubnetID: "subnet-1", DBMajorVersion: "14",
		SpecCode: "pg.2c4g", CPU: 2, MemoryMB: 4096, DiskGB: 100,
		ChargeType: "PREPAID", Period: 5, Charset: "UTF8", SyncMode: "Semi-sync",
		AdminUser: "dbadmin", PasswordEnc: "Abcdef1!",
	}
	if err := ValidateCreate(ok); err != nil {
		t.Fatal(err)
	}
	mysql := ok
	mysql.Engine = EngineMySQL
	mysql.VpcID = ""
	if err := ValidateCreate(mysql); err != nil {
		t.Fatal(err)
	}
	missing := ok
	missing.SubnetID = ""
	if err := ValidateCreate(missing); err == nil {
		t.Fatal("subnet is required")
	}
	disk := ok
	disk.DiskGB = 15
	if err := ValidateCreate(disk); err == nil {
		t.Fatal("disk step")
	}
	postpaid := ok
	postpaid.ChargeType = "POSTPAID_BY_HOUR"
	postpaid.Period = 0
	if err := ValidateCreate(postpaid); err != nil {
		t.Fatal(err)
	}
}
