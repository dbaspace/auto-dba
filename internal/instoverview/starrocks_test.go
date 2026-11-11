package instoverview

import "testing"

func TestSplitFEHosts(t *testing.T) {
	got := splitFEHosts("172.24.16.4,172.24.16.25，172.24.16.7")
	if len(got) != 3 || got[0] != "172.24.16.4" || got[2] != "172.24.16.7" {
		t.Fatalf("hosts=%v", got)
	}
	host, port := feHostPort("10.0.0.1:9030", 0)
	if host != "10.0.0.1" || port != 9030 {
		t.Fatalf("host=%s port=%d", host, port)
	}
	host, port = feHostPort("10.0.0.2", 0)
	if host != "10.0.0.2" || port != 9030 {
		t.Fatalf("fallback host=%s port=%d", host, port)
	}
}

func TestParseCapacityBytes(t *testing.T) {
	b, ok := parseCapacityBytes("99.951 GB")
	gb := 99.951
	want := int64(gb * 1024 * 1024 * 1024)
	if !ok || b != want {
		t.Fatalf("gb=%d want=%d ok=%v", b, want, ok)
	}
	b, ok = parseCapacityBytes("100MB")
	if !ok || b != 100*1024*1024 {
		t.Fatalf("mb=%d ok=%v", b, ok)
	}
	if _, ok := parseCapacityBytes(""); ok {
		t.Fatal("空容量不应解析")
	}
	pct, ok := parsePercent("37.02 %")
	if !ok || pct != 37.02 {
		t.Fatalf("pct=%v ok=%v", pct, ok)
	}
}

func TestSummarizeStarRocks(t *testing.T) {
	fe := []srNode{
		{Host: "10.0.0.1", Version: "3.2.9", Role: "FOLLOWER", Alive: "true"},
		{Host: "10.0.0.2", Version: "3.2.10", Role: "LEADER", Alive: "true"},
		{Host: "10.0.0.3", Version: "3.2.10", Role: "FOLLOWER", Alive: "false"},
	}
	be := []srNode{
		{Host: "10.0.0.11", Alive: "true", Total: "100 GB", DataUsed: "10 GB", UsedPct: "20 %", Tablet: "10"},
		{Host: "10.0.0.12", Alive: "true", Total: "50 GB", DataUsed: "40 GB", UsedPct: "85.5 %", Tablet: "3"},
		{Host: "10.0.0.13", Alive: "false", Total: "1 GB", DataUsed: "0 B", UsedPct: "1 %", Tablet: "0", Decommissioned: true},
	}
	s := summarizeStarRocks(fe, be)
	if s.Version != "3.2.10" {
		t.Fatalf("version=%s", s.Version)
	}
	if s.FEAlive != 2 || s.FETotal != 3 || s.BEAlive != 2 || s.BETotal != 3 {
		t.Fatalf("alive fe=%d/%d be=%d/%d", s.FEAlive, s.FETotal, s.BEAlive, s.BETotal)
	}
	if !s.DiskKnown || s.DiskBytes != int64((100+50+1)*1024*1024*1024) {
		t.Fatalf("disk=%d known=%v", s.DiskBytes, s.DiskKnown)
	}
	if !s.DataKnown || s.DataBytes != int64((10+40)*1024*1024*1024) {
		t.Fatalf("data=%d", s.DataBytes)
	}
	if !s.StorageRateKnown || s.StorageRate != 85.5 || s.StorageHotHost != "10.0.0.12" {
		t.Fatalf("rate=%v host=%s", s.StorageRate, s.StorageHotHost)
	}
	if !s.TabletKnown || s.TabletCount != 13 {
		t.Fatalf("tablet=%d known=%v", s.TabletCount, s.TabletKnown)
	}
	if !s.BEDecommissioning {
		t.Fatal("应标记下线中")
	}
}
