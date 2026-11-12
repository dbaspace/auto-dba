package instoverview

import "testing"
import "time"

const gib = float64(1 << 30)

func TestHostDiskUsageSplitsRoot(t *testing.T) {
	got := HostDiskUsage([]FSSample{
		{Instance: "10.0.0.1:9100", Mountpoint: "/", Device: "/dev/vda1", Size: 100 * gib, Avail: 25 * gib},
		{Instance: "10.0.0.1:9100", Mountpoint: "/boot", Device: "/dev/vda2", Size: gib, Avail: 0},
		{Instance: "10.0.0.1:9100", Mountpoint: "/data", Device: "/dev/vdb", Size: 500 * gib, Avail: 400 * gib},
	})
	d := got["10.0.0.1"]
	if d.System.Size != 100*gib || d.System.Rate() != 75 {
		t.Fatalf("system = %+v", d.System)
	}
	if d.Data.Size != 500*gib || d.Data.Rate() != 20 {
		t.Fatalf("data = %+v", d.Data)
	}
}

func TestHostDiskTrendsNewDeviceNotGrowth(t *testing.T) {
	cur := []FSSample{
		{Instance: "10.0.0.1:9100", Mountpoint: "/data", Device: "/dev/vdb", Size: 500 * gib, Avail: 300 * gib},
		{Instance: "10.0.0.1:9100", Mountpoint: "/data2", Device: "/dev/vdc", Size: 500 * gib, Avail: 100 * gib},
	}
	prev := []FSSample{
		{Instance: "10.0.0.1:9100", Mountpoint: "/data", Device: "/dev/vdb", Size: 500 * gib, Avail: 370 * gib},
	}
	d := HostDiskTrends(cur, prev, 7)["10.0.0.1"].Data
	if d == nil || !d.HasPrev || d.NewDevices != 1 {
		t.Fatalf("trend = %+v", d)
	}
	if d.GrowthBytes != 70*gib || d.DailyBytes != 10*gib {
		t.Fatalf("growth = %v daily = %v", d.GrowthBytes/gib, d.DailyBytes/gib)
	}
	if d.UsedBytes != 600*gib || d.DaysToFull == nil || *d.DaysToFull != 40 {
		t.Fatalf("used = %v days = %v", d.UsedBytes/gib, d.DaysToFull)
	}
}

func TestHostDiskTrendsNoPrev(t *testing.T) {
	cur := []FSSample{{Instance: "10.0.0.1:9100", Mountpoint: "/", Device: "/dev/vda1", Size: 100 * gib, Avail: 10 * gib}}
	s := HostDiskTrends(cur, nil, 7)["10.0.0.1"].System
	if s == nil || s.HasPrev || s.GrowthBytes != 0 || s.DaysToFull != nil || s.Level != "muted" {
		t.Fatalf("system = %+v", s)
	}
}

func TestHostDiskTrendsShrinkHasNoFullDays(t *testing.T) {
	cur := []FSSample{{Instance: "10.0.0.1:9100", Mountpoint: "/", Device: "/dev/vda1", Size: 100 * gib, Avail: 60 * gib}}
	prev := []FSSample{{Instance: "10.0.0.1:9100", Mountpoint: "/", Device: "/dev/vda1", Size: 100 * gib, Avail: 50 * gib}}
	s := HostDiskTrends(cur, prev, 7)["10.0.0.1"].System
	if s.DaysToFull != nil || s.GrowthBytes != -10*gib || s.Advice != "近 7 天用量下降" {
		t.Fatalf("system = %+v", s)
	}
}

func TestParseTrendWindow(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	start, end, days, err := ParseTrendWindow("", "", now)
	if err != nil || days != 7 || !end.Equal(now) || !start.Equal(now.AddDate(0, 0, -7)) {
		t.Fatalf("default start=%s end=%s days=%d err=%v", start, end, days, err)
	}
	_, _, _, err = ParseTrendWindow(now.AddDate(0, -4, 0).Format(time.RFC3339), now.Format(time.RFC3339), now)
	if err == nil {
		t.Fatal("4 months must be rejected")
	}
	_, _, _, err = ParseTrendWindow(now.Add(-12*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339), now)
	if err == nil {
		t.Fatal("under 1 day must be rejected")
	}
	_, _, days, err = ParseTrendWindow(now.AddDate(0, -2, 0).Format(time.RFC3339), now.Format(time.RFC3339), now)
	if err != nil || days < 59 || days > 62 {
		t.Fatalf("2 months days=%d err=%v", days, err)
	}
}

func TestHostDiskReportTitle(t *testing.T) {
	if got := HostDiskReportTitle("", "", "", 25); got != "主机磁盘 · 全部域名 / 全部地址 / 全部环境 · 25 台" {
		t.Fatalf("all = %q", got)
	}
	if got := HostDiskReportTitle("a.example", "10.0.0.1", "北京", 1); got != "主机磁盘 · a.example / 10.0.0.1 / 北京 · 1 台" {
		t.Fatalf("picked = %q", got)
	}
}

func TestSummarizeTrendsCountsRisk(t *testing.T) {
	high := []FSSample{{Instance: "10.0.0.1:9100", Mountpoint: "/data", Device: "/dev/vdb", Size: 100 * gib, Avail: 10 * gib}}
	highPrev := []FSSample{{Instance: "10.0.0.1:9100", Mountpoint: "/data", Device: "/dev/vdb", Size: 100 * gib, Avail: 17 * gib}}
	tr := HostDiskTrends(high, highPrev, 7)["10.0.0.1"]
	items := []TrendItem{
		{ID: "b", Host: "10.0.0.2"},
		{ID: "a", Host: "10.0.0.1", Data: tr.Data},
	}
	SortTrendItems(items)
	if items[0].ID != "a" {
		t.Fatalf("order = %v", items)
	}
	s := SummarizeTrends(items, 7)
	if s.HighGrowing != 1 || s.Full14 != 1 || s.NoSample != 1 || len(s.Conclusions) == 0 {
		t.Fatalf("summary = %+v", s)
	}
}
