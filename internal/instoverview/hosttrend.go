package instoverview

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// HostDiskReportTitle 给历史列表用。没选的条件写成「全部」。
func HostDiskReportTitle(domain, host, envName string, n int) string {
	label := func(v, all string) string {
		if strings.TrimSpace(v) == "" {
			return all
		}
		return strings.TrimSpace(v)
	}
	return fmt.Sprintf("主机磁盘 · %s / %s / %s · %d 台",
		label(domain, "全部域名"), label(host, "全部地址"), label(envName, "全部环境"), n)
}

// TrendDays 是未指定时间时的默认对比天数。
const TrendDays = 7

// ParseTrendWindow 解析分析区间。都空则用现在往前 7 天。结束不能晚于现在，区间至少 1 天、最长 3 个月。
func ParseTrendWindow(startRaw, endRaw string, now time.Time) (start, end time.Time, days int, err error) {
	startRaw = strings.TrimSpace(startRaw)
	endRaw = strings.TrimSpace(endRaw)
	if startRaw == "" && endRaw == "" {
		end = now
		start = now.AddDate(0, 0, -TrendDays)
	} else {
		start, err = time.Parse(time.RFC3339, startRaw)
		if err != nil {
			return time.Time{}, time.Time{}, 0, errors.New("开始时间格式不正确")
		}
		end, err = time.Parse(time.RFC3339, endRaw)
		if err != nil {
			return time.Time{}, time.Time{}, 0, errors.New("结束时间格式不正确")
		}
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, 0, errors.New("结束时间必须晚于开始时间")
	}
	if end.After(now.Add(time.Minute)) {
		return time.Time{}, time.Time{}, 0, errors.New("结束时间不能晚于现在")
	}
	if start.Before(end.AddDate(0, -3, 0)) {
		return time.Time{}, time.Time{}, 0, errors.New("分析区间不能超过 3 个月")
	}
	if end.Sub(start) < 24*time.Hour {
		return time.Time{}, time.Time{}, 0, errors.New("分析区间至少 1 天")
	}
	days = trendDayCount(end.Sub(start).Hours() / 24)
	return start, end, days, nil
}

// DiskTrend 是一台主机一类盘在对比窗口内的用量变化。
type DiskTrend struct {
	SizeBytes     float64  `json:"size_bytes"`
	UsedBytes     float64  `json:"used_bytes"`
	Rate          float64  `json:"rate"`
	HasPrev       bool     `json:"has_prev"`
	PrevUsedBytes float64  `json:"prev_used_bytes,omitempty"`
	GrowthBytes   float64  `json:"growth_bytes,omitempty"`
	DailyBytes    float64  `json:"daily_bytes,omitempty"`
	DaysToFull    *float64 `json:"days_to_full,omitempty"`
	NewDevices    int      `json:"new_devices,omitempty"`
	Level         string   `json:"level"`
	Advice        string   `json:"advice"`
}

// HostDiskTrend 是一台主机的数据盘和系统盘变化。没有对应样本的一类盘为 nil。
type HostDiskTrend struct {
	Data   *DiskTrend
	System *DiskTrend
}

type devUse struct{ size, used float64 }

// diskDevices 把样本按主机 IP 和设备拆成数据盘与根分区两份。同一设备只取一次。
func diskDevices(samples []FSSample) (data, system map[string]map[string]devUse) {
	data = map[string]map[string]devUse{}
	system = map[string]map[string]devUse{}
	for _, s := range samples {
		if s.Size <= 0 {
			continue
		}
		ip := store.HostAddrKey(s.Instance)
		if ip == "" {
			continue
		}
		mp := strings.TrimSpace(s.Mountpoint)
		used := math.Max(s.Size-s.Avail, 0)
		if mp == "/" {
			if system[ip] == nil {
				system[ip] = map[string]devUse{"/": {s.Size, used}}
			}
			continue
		}
		if systemMount(mp) {
			continue
		}
		dev := strings.TrimSpace(s.Device)
		if dev == "" {
			dev = mp
		}
		if data[ip] == nil {
			data[ip] = map[string]devUse{}
		}
		if _, ok := data[ip][dev]; !ok {
			data[ip][dev] = devUse{s.Size, used}
		}
	}
	return data, system
}

// HostDiskTrends 对比当前与 days 天前的样本。只有两边都有的设备计入增长，新挂的盘不算成增长。
func HostDiskTrends(cur, prev []FSSample, days float64) map[string]HostDiskTrend {
	curData, curSys := diskDevices(cur)
	prevData, prevSys := diskDevices(prev)
	out := map[string]HostDiskTrend{}
	for ip, devs := range curData {
		t := out[ip]
		t.Data = buildTrend(devs, prevData[ip], days)
		out[ip] = t
	}
	for ip, devs := range curSys {
		t := out[ip]
		t.System = buildTrend(devs, prevSys[ip], days)
		out[ip] = t
	}
	return out
}

func buildTrend(cur, prev map[string]devUse, days float64) *DiskTrend {
	t := &DiskTrend{}
	var prevUsed, curMatched float64
	for dev, u := range cur {
		t.SizeBytes += u.size
		t.UsedBytes += u.used
		p, ok := prev[dev]
		if !ok {
			t.NewDevices++
			continue
		}
		t.HasPrev = true
		prevUsed += p.used
		curMatched += u.used
	}
	if t.SizeBytes > 0 {
		t.Rate = t.UsedBytes / t.SizeBytes * 100
	}
	if t.HasPrev {
		t.PrevUsedBytes = prevUsed
		t.GrowthBytes = curMatched - prevUsed
		if days > 0 {
			t.DailyBytes = t.GrowthBytes / days
		}
	}
	t.Level, t.Advice = trendAdvice(t, trendDayCount(days))
	return t
}

func trendDayCount(days float64) int {
	n := int(math.Round(days))
	if n < 1 {
		return 1
	}
	return n
}

// flatBytes 是视为持平的 7 天变化量：100 MiB 与容量 0.1% 取大。
func flatBytes(size float64) float64 {
	return math.Max(100*1024*1024, size*0.001)
}

func trendAdvice(t *DiskTrend, days int) (level, advice string) {
	if !t.HasPrev {
		return "muted", fmt.Sprintf("没有 %d 天前的样本，暂不能判断增长", days)
	}
	flat := flatBytes(t.SizeBytes)
	if t.GrowthBytes > flat && t.DailyBytes > 0 {
		days := math.Max(t.SizeBytes-t.UsedBytes, 0) / t.DailyBytes
		t.DaysToFull = &days
		switch {
		case t.Rate >= 80:
			return "danger", "使用率已过 80% 且仍在增长，优先清理或扩容"
		case days <= 14:
			return "danger", "按当前增速两周内可能写满，尽快清理或扩容"
		case days <= 30:
			return "warn", "按当前增速一个月内可能写满，需关注"
		case days <= 90:
			return "ok", "增长中，三个月内容量够用"
		default:
			return "ok", "增长中，容量仍充足"
		}
	}
	if t.Rate >= 80 {
		return "warn", fmt.Sprintf("使用率偏高，近 %d 天未增长，建议清理", days)
	}
	if t.GrowthBytes < -flat {
		return "ok", fmt.Sprintf("近 %d 天用量下降", days)
	}
	return "ok", fmt.Sprintf("近 %d 天未增长", days)
}

// TrendSortKey 是主机按风险排序用的值：两类盘中较短的写满天数，没有时排在后面。
func TrendSortKey(t HostDiskTrend) float64 {
	best := math.Inf(1)
	for _, d := range []*DiskTrend{t.Data, t.System} {
		if d != nil && d.DaysToFull != nil && *d.DaysToFull < best {
			best = *d.DaysToFull
		}
	}
	return best
}

// TrendItem 是报告里的一台主机。
type TrendItem struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	App     string     `json:"app,omitempty"`
	Domain  string     `json:"domain,omitempty"`
	Host    string     `json:"host"`
	EnvName string     `json:"env_name"`
	Data    *DiskTrend `json:"data,omitempty"`
	System  *DiskTrend `json:"system,omitempty"`
}

// TrendSummary 是报告顶部的汇总结论。
type TrendSummary struct {
	Hosts       int      `json:"hosts"`
	NoSample    int      `json:"no_sample"`
	NoPrev      int      `json:"no_prev"`
	Full14      int      `json:"full_14"`
	Full30      int      `json:"full_30"`
	HighGrowing int      `json:"high_growing"`
	HighFlat    int      `json:"high_flat"`
	Conclusions []string `json:"conclusions"`
}

// SortTrendItems 按写满天数从短到长，其次按两类盘较高的使用率从高到低。
func SortTrendItems(items []TrendItem) {
	rate := func(it TrendItem) float64 {
		r := 0.0
		for _, d := range []*DiskTrend{it.Data, it.System} {
			if d != nil && d.Rate > r {
				r = d.Rate
			}
		}
		return r
	}
	sort.SliceStable(items, func(i, j int) bool {
		a := TrendSortKey(HostDiskTrend{Data: items[i].Data, System: items[i].System})
		b := TrendSortKey(HostDiskTrend{Data: items[j].Data, System: items[j].System})
		if a != b {
			return a < b
		}
		return rate(items[i]) > rate(items[j])
	})
}

// SummarizeTrends 统计各类风险主机数并写出结论。一台主机只要有一类盘满足就计入。
func SummarizeTrends(items []TrendItem, days int) TrendSummary {
	if days < 1 {
		days = 1
	}
	s := TrendSummary{Hosts: len(items)}
	for _, it := range items {
		if it.Data == nil && it.System == nil {
			s.NoSample++
			continue
		}
		var f14, f30, hg, hf, compared bool
		for _, d := range []*DiskTrend{it.Data, it.System} {
			if d == nil {
				continue
			}
			if d.HasPrev {
				compared = true
			}
			if d.DaysToFull != nil {
				if *d.DaysToFull <= 14 {
					f14 = true
				}
				if *d.DaysToFull <= 30 {
					f30 = true
				}
				if d.Rate >= 80 {
					hg = true
				}
			} else if d.HasPrev && d.Rate >= 80 {
				hf = true
			}
		}
		if !compared {
			s.NoPrev++
		}
		if f14 {
			s.Full14++
		}
		if f30 {
			s.Full30++
		}
		if hg {
			s.HighGrowing++
		}
		if hf {
			s.HighFlat++
		}
	}
	if s.Hosts == 0 {
		s.Conclusions = []string{"当前选择没有主机"}
		return s
	}
	if s.HighGrowing > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台主机使用率已过 80%% 且仍在增长，优先处理", s.HighGrowing))
	}
	if s.Full14 > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台主机按近 %d 天增速两周内可能写满", s.Full14, days))
	}
	if n := s.Full30 - s.Full14; n > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("另有 %d 台一个月内可能写满", n))
	}
	if s.HighFlat > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台使用率已过 80%% 但近 %d 天未增长，可安排清理", s.HighFlat, days))
	}
	risky := s.HighGrowing+s.Full30+s.HighFlat > 0
	if compared := s.Hosts - s.NoSample - s.NoPrev; !risky && compared > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("已对比的 %d 台主机按近 %d 天增速，一个月内都不会写满，使用率也未过 80%%", compared, days))
	}
	if s.NoPrev > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台没有 %d 天前的样本，只给出当前使用率", s.NoPrev, days))
	}
	if s.NoSample > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台没有 node_exporter 样本，未纳入分析", s.NoSample))
	}
	return s
}
