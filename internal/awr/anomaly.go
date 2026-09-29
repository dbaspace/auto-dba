package awr

import "fmt"

const (
	awrSeverityNone     = "none"
	awrSeverityWarning  = "warning"
	awrSeverityCritical = "critical"
)

type awrThresholdRule struct {
	Key       string
	Direction string
	Stat      string
	Warn      float64
	Crit      float64
	Unit      string
}

var awrThresholdRules = []awrThresholdRule{
	{Key: "cpu_usage", Direction: "above", Stat: "max", Warn: 0.85, Crit: 1.0, Unit: "ratio"},
	{Key: "mem_usage", Direction: "above", Stat: "max", Warn: 0.85, Crit: 0.95, Unit: "ratio"},
	{Key: "disk_usage", Direction: "above", Stat: "max", Warn: 0.80, Crit: 0.90, Unit: "ratio"},
	{Key: "conn_usage_ratio", Direction: "above", Stat: "max", Warn: 0.80, Crit: 0.95, Unit: "ratio"},
	{Key: "cache_hit_ratio", Direction: "below", Stat: "min", Warn: 0.95, Crit: 0.90, Unit: "ratio"},
	{Key: "replication_lag", Direction: "above", Stat: "max", Warn: 30, Crit: 300, Unit: "s"},
	{Key: "deadlocks_ps", Direction: "above", Stat: "max", Warn: 0.01, Crit: 0.1, Unit: "/s"},
	{Key: "slow_query_cnt", Direction: "above", Stat: "max", Warn: 10, Crit: 100, Unit: "count"},
}

type awrDoDRule struct {
	Key       string
	Direction string
	Stat      string
	Warn      float64
	Crit      float64
}

var awrDoDRules = []awrDoDRule{
	{Key: "tps", Direction: "drop", Stat: "avg", Warn: 0.5, Crit: 0.8},
	{Key: "backends", Direction: "spike", Stat: "avg", Warn: 1.0, Crit: 2.0},
	{Key: "slow_query_cnt", Direction: "spike", Stat: "max", Warn: 1.0, Crit: 3.0},
	{Key: "deadlocks_ps", Direction: "spike", Stat: "avg", Warn: 1.0, Crit: 3.0},
}

func (s *Service) evaluateAnomalies(cur, prev *AwrSections) ([]AwrAnomaly, string) {
	var out []AwrAnomaly
	highest := awrSeverityNone
	if cur == nil {
		return out, highest
	}

	for _, r := range awrThresholdRules {
		m, secName := FindMetric(cur, r.Key)
		if m == nil {
			continue
		}
		val := MetricStat(m, r.Stat)
		if val == nil {
			continue
		}
		warn := s.floatArg("awr_th_"+r.Key+"_"+awrSeverityWarning, r.Warn)
		crit := s.floatArg("awr_th_"+r.Key+"_"+awrSeverityCritical, r.Crit)
		sev, thr := "", 0.0
		if r.Direction == "below" {
			switch {
			case *val <= crit:
				sev, thr = awrSeverityCritical, crit
			case *val <= warn:
				sev, thr = awrSeverityWarning, warn
			}
		} else {
			switch {
			case *val >= crit:
				sev, thr = awrSeverityCritical, crit
			case *val >= warn:
				sev, thr = awrSeverityWarning, warn
			}
		}
		if sev == "" {
			continue
		}
		unit := r.Unit
		if unit == "" {
			unit = m.Unit
		}
		v, t := *val, thr
		out = append(out, AwrAnomaly{
			Key: r.Key, Title: m.Title, Section: secName, Severity: sev, Kind: "threshold",
			Value: &v, Threshold: &t, Unit: unit,
			Message: awrThresholdMsg(m.Title, r.Direction, sev, v, t, unit),
		})
		highest = awrMaxSeverity(highest, sev)
	}

	if prev != nil {
		for _, r := range awrDoDRules {
			m, secName := FindMetric(cur, r.Key)
			if m == nil {
				continue
			}
			pm, _ := FindMetric(prev, r.Key)
			if pm == nil {
				continue
			}
			cv := MetricStat(m, r.Stat)
			bv := MetricStat(pm, r.Stat)
			if cv == nil || bv == nil || *bv <= 0 {
				continue
			}
			delta := (*cv - *bv) / *bv
			mag := delta
			if r.Direction == "drop" {
				mag = -delta
			}
			warn := s.floatArg("awr_dod_"+r.Key+"_"+awrSeverityWarning, r.Warn)
			crit := s.floatArg("awr_dod_"+r.Key+"_"+awrSeverityCritical, r.Crit)
			sev := ""
			switch {
			case mag >= crit:
				sev = awrSeverityCritical
			case mag >= warn:
				sev = awrSeverityWarning
			}
			if sev == "" {
				continue
			}
			cvv, bvv, dp := *cv, *bv, delta*100
			out = append(out, AwrAnomaly{
				Key: r.Key, Title: m.Title, Section: secName, Severity: sev, Kind: "dod",
				Value: &cvv, Baseline: &bvv, DeltaPct: &dp, Unit: m.Unit,
				Message: awrDoDMsg(m.Title, r.Direction, sev, cvv, bvv, dp),
			})
			highest = awrMaxSeverity(highest, sev)
		}
	}
	return out, highest
}

// FindMetric 在所有板块中查找指定逻辑键（供 sqlinsight 复用）。
func FindMetric(sec *AwrSections, key string) (*AwrMetric, string) {
	if sec == nil {
		return nil, ""
	}
	for _, s := range sec.Sections {
		for _, m := range s.Metrics {
			if m != nil && m.Key == key {
				return m, s.Name
			}
		}
	}
	return nil, ""
}

// MetricStat 取指标的指定统计量（供 sqlinsight 复用）。
func MetricStat(m *AwrMetric, stat string) *float64 {
	if m == nil {
		return nil
	}
	switch stat {
	case "max":
		return m.Max
	case "min":
		return m.Min
	case "last":
		return m.Last
	default:
		return m.Avg
	}
}

func awrSeverityRank(s string) int {
	switch s {
	case awrSeverityCritical:
		return 2
	case awrSeverityWarning:
		return 1
	default:
		return 0
	}
}

func awrMaxSeverity(a, b string) string {
	if awrSeverityRank(b) > awrSeverityRank(a) {
		return b
	}
	return a
}

func awrSeverityLabel(s string) string {
	switch s {
	case awrSeverityCritical:
		return "严重"
	case awrSeverityWarning:
		return "告警"
	default:
		return "正常"
	}
}

func awrThresholdMsg(title, direction, sev string, val, thr float64, unit string) string {
	rel := "超过"
	if direction == "below" {
		rel = "低于"
	}
	return fmt.Sprintf("%s：%s观测值 %s%s，%s阈值 %s%s",
		title, awrSeverityLabel(sev), awrFmtDisplay(val), unit, rel, awrFmtDisplay(thr), unit)
}

func awrDoDMsg(title, direction, sev string, cur, base, deltaPct float64) string {
	trend := "环比上涨"
	if direction == "drop" {
		trend = "环比下跌"
	}
	return fmt.Sprintf("%s：%s，%s %.1f%%（今日 %s / 昨日 %s）",
		title, awrSeverityLabel(sev), trend, deltaPct, awrFmtDisplay(cur), awrFmtDisplay(base))
}
