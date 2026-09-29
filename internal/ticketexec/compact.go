package ticketexec

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// DetailCompactThreshold 成功明细超过该阈值则收敛，避免 OOM / 落库失败
	DetailCompactThreshold = 1000
	// MaxSuccessSamplesPerTarget 大批量时每个 host+db 最多保留的成功抽样条数
	MaxSuccessSamplesPerTarget = 3
)

// TargetStat 按库汇总（大批量结果用）
type TargetStat struct {
	Host    string `json:"host"`
	DB      string `json:"db"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
}

func targetKey(host, db string) string {
	return strings.TrimSpace(host) + "\x00" + strings.TrimSpace(db)
}

func (r *Result) enableCompactMode() {
	if r == nil {
		return
	}
	r.compactMode = true
}

func (r *Result) shouldKeepSuccessDetail(host, db string) bool {
	if r == nil {
		return false
	}
	if r.sampleKept == nil {
		r.sampleKept = map[string]int{}
	}
	key := targetKey(host, db)
	// 未进入收敛模式且未超阈值：全量保留
	if !r.compactMode && r.SuccessCount <= DetailCompactThreshold {
		r.sampleKept[key]++
		return true
	}
	// 大批量：每库仅保留少量抽样
	if r.sampleKept[key] < MaxSuccessSamplesPerTarget {
		r.sampleKept[key]++
		return true
	}
	return false
}

// Compact 收敛结果集：失败全留，成功按库抽样，清空大 success_sqls，写汇总。
// 小规模结果原样返回。可重复调用。
func Compact(r *Result) *Result {
	if r == nil {
		return nil
	}
	successN, failN := r.SuccessCount, r.FailCount
	if successN == 0 && failN == 0 {
		for _, rec := range r.Records {
			switch strings.ToLower(strings.TrimSpace(rec.Status)) {
			case "failed":
				failN++
			case "success":
				successN++
			}
		}
		if successN == 0 {
			successN = len(r.SuccessSQLs)
		}
	}
	r.SuccessCount = successN
	r.FailCount = failN

	// 已收敛过：只保证明细有界并刷新文案，保留准确 TargetStats
	if r.Compacted {
		r.SuccessSQLs = nil
		keptSamples := map[string]int{}
		out := make([]Record, 0, len(r.Records))
		for _, rec := range r.Records {
			st := strings.ToLower(strings.TrimSpace(rec.Status))
			if st == "failed" || st == "skipped" {
				out = append(out, rec)
				continue
			}
			if st != "success" {
				out = append(out, rec)
				continue
			}
			key := targetKey(rec.Host, rec.DB)
			if keptSamples[key] < MaxSuccessSamplesPerTarget {
				keptSamples[key]++
				out = append(out, rec)
			}
		}
		for i := range out {
			out[i].Order = i + 1
		}
		r.Records = out
		r.refreshCompactSummary(successN, failN)
		return r
	}

	need := r.compactMode ||
		successN > DetailCompactThreshold ||
		len(r.SuccessSQLs) > DetailCompactThreshold ||
		countSuccessRecords(r.Records) > DetailCompactThreshold
	if !need {
		if r.Summary == "" && successN > 0 {
			r.Summary = fmt.Sprintf("成功执行 %d 条 SQL", successN)
		}
		return r
	}

	var stats map[string]*TargetStat
	if len(r.targetHit) > 0 {
		stats = r.targetHit
	} else {
		stats = map[string]*TargetStat{}
		bump := func(host, db, status string) {
			key := targetKey(host, db)
			st := stats[key]
			if st == nil {
				st = &TargetStat{Host: host, DB: db}
				stats[key] = st
			}
			if strings.EqualFold(status, "failed") {
				st.Failed++
			} else if strings.EqualFold(status, "success") {
				st.Success++
			}
		}
		if len(r.Records) == 0 && len(r.SuccessSQLs) > 0 {
			for _, s := range r.SuccessSQLs {
				bump(s.Host, s.DB, "success")
			}
		} else {
			for _, rec := range r.Records {
				st := strings.ToLower(strings.TrimSpace(rec.Status))
				if st == "success" || st == "failed" {
					bump(rec.Host, rec.DB, st)
				}
			}
		}
	}

	keptSamples := map[string]int{}
	out := make([]Record, 0, len(r.Records))
	for _, rec := range r.Records {
		st := strings.ToLower(strings.TrimSpace(rec.Status))
		if st == "failed" || st == "skipped" {
			out = append(out, rec)
			continue
		}
		if st != "success" {
			out = append(out, rec)
			continue
		}
		key := targetKey(rec.Host, rec.DB)
		if keptSamples[key] < MaxSuccessSamplesPerTarget {
			keptSamples[key]++
			out = append(out, rec)
		}
	}
	for i := range out {
		out[i].Order = i + 1
	}
	r.Records = out
	r.SuccessSQLs = nil
	r.Compacted = true
	r.compactMode = true

	r.TargetStats = targetStatsSlice(stats)
	r.refreshCompactSummary(successN, failN)
	return r
}

// refreshCompactSummary 事务回滚的摘要（第 N 条失败、均未生效）比收敛统计更关键，不覆盖。
func (r *Result) refreshCompactSummary(successN, failN int) {
	if r.RolledBack && r.Summary != "" {
		return
	}
	r.Summary = compactSummary(successN, failN, r.StartedAt, r.EndedAt, r.TargetStats)
}

func compactSummary(successN, failN int, startedAt, endedAt string, stats []TargetStat) string {
	dur := ""
	if startedAt != "" && endedAt != "" {
		if a, e1 := time.Parse(time.RFC3339, startedAt); e1 == nil {
			if b, e2 := time.Parse(time.RFC3339, endedAt); e2 == nil && b.After(a) {
				dur = b.Sub(a).Round(time.Second).String()
			}
		}
	}
	parts := []string{fmt.Sprintf("大批量结果已收敛：成功 %d，失败 %d", successN, failN)}
	if dur != "" {
		parts = append(parts, "耗时 "+dur)
	}
	if len(stats) > 0 {
		var dbParts []string
		for _, st := range stats {
			label := st.DB
			if label == "" {
				label = st.Host
			}
			dbParts = append(dbParts, fmt.Sprintf("%s 成功%d/失败%d", label, st.Success, st.Failed))
		}
		if len(dbParts) > 8 {
			dbParts = append(dbParts[:8], fmt.Sprintf("…共%d库", len(stats)))
		}
		parts = append(parts, strings.Join(dbParts, "；"))
	}
	return strings.Join(parts, "；")
}

func countSuccessRecords(recs []Record) int {
	n := 0
	for _, rec := range recs {
		if strings.EqualFold(strings.TrimSpace(rec.Status), "success") {
			n++
		}
	}
	return n
}

func targetStatsSlice(m map[string]*TargetStat) []TargetStat {
	if len(m) == 0 {
		return nil
	}
	out := make([]TargetStat, 0, len(m))
	for _, st := range m {
		if st != nil {
			out = append(out, *st)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].DB < out[j].DB
	})
	return out
}
