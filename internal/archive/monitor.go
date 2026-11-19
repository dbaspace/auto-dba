package archive

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"auto-dba/internal/store"
)

const (
	alertRunTimeout   = "run_timeout"
	alertFailCount    = "fail_count"
	alertStorageUsage = "storage_usage"
	alertDBDisconnect = "db_disconnect"
)

func NormalizeAlertRuleType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case alertRunTimeout, "timeout":
		return alertRunTimeout
	case alertFailCount, "fail", "failed":
		return alertFailCount
	case alertStorageUsage, "storage":
		return alertStorageUsage
	case alertDBDisconnect, "disconnect", "db":
		return alertDBDisconnect
	default:
		return ""
	}
}

func DefaultAlertThreshold(ruleType string) float64 {
	switch ruleType {
	case alertRunTimeout:
		return 30
	case alertFailCount:
		return 3
	case alertStorageUsage:
		return 10
	case alertDBDisconnect:
		return 1
	default:
		return 0
	}
}

func DefaultAlertSeverity(ruleType string) string {
	switch ruleType {
	case alertRunTimeout, alertDBDisconnect:
		return "critical"
	case alertStorageUsage:
		return "notice"
	default:
		return "warning"
	}
}

func AlertRuleTitle(ruleType string, threshold float64, title string) string {
	if s := strings.TrimSpace(title); s != "" {
		return s
	}
	switch ruleType {
	case alertRunTimeout:
		return fmt.Sprintf("任务执行超时 > %.0f 分钟", threshold)
	case alertFailCount:
		return fmt.Sprintf("归档失败次数 > %.0f", threshold)
	case alertStorageUsage:
		return fmt.Sprintf("存储剩余 < %.0f TB", threshold)
	case alertDBDisconnect:
		return "数据库连接断开"
	default:
		return ruleType
	}
}

type alertHit struct {
	Fingerprint string
	Message     string
}

func evalAlertRule(rule store.ArchiveAlertRule, running, failed []store.ArchiveRun, failedToday int, now time.Time, storage StorageSettings) []alertHit {
	if !rule.Enabled {
		return nil
	}
	switch rule.RuleType {
	case alertRunTimeout:
		min := rule.Threshold
		if min <= 0 {
			min = 30
		}
		limit := time.Duration(min * float64(time.Minute))
		var hits []alertHit
		for _, r := range running {
			started := parseArchiveTime(r.StartedAt)
			if started == nil {
				continue
			}
			if now.Sub(*started) > limit {
				hits = append(hits, alertHit{
					Fingerprint: "run:" + r.ID,
					Message:     fmt.Sprintf("归档任务超时（run=%s 已运行 %s）", r.ID, now.Sub(*started).Truncate(time.Second)),
				})
			}
		}
		return hits
	case alertFailCount:
		if float64(failedToday) > rule.Threshold {
			day := now.In(archiveLocation()).Format("2006-01-02")
			return []alertHit{{
				Fingerprint: "fail:" + day,
				Message:     fmt.Sprintf("归档失败次数 %d > %.0f", failedToday, rule.Threshold),
			}}
		}
	case alertStorageUsage:
		if !storage.Set {
			return nil
		}
		if storage.AvailableTb < rule.Threshold {
			day := now.In(archiveLocation()).Format("2006-01-02")
			return []alertHit{{
				Fingerprint: "storage:" + day,
				Message:     fmt.Sprintf("存储剩余 %.1f TB < %.0f TB", storage.AvailableTb, rule.Threshold),
			}}
		}
	case alertDBDisconnect:
		n := 0
		var first string
		for _, r := range failed {
			if looksLikeDisconnect(r.Message) {
				n++
				if first == "" {
					first = r.ID
				}
			}
		}
		if n > 0 && float64(n) >= rule.Threshold {
			day := now.In(archiveLocation()).Format("2006-01-02")
			return []alertHit{{
				Fingerprint: "db:" + day,
				Message:     fmt.Sprintf("数据库连接异常（%d 条，最近 run=%s）", n, first),
			}}
		}
	}
	return nil
}

func looksLikeDisconnect(msg string) bool {
	s := strings.ToLower(msg)
	if s == "" {
		return false
	}
	keys := []string{"connection refused", "connect:", "dial ", "i/o timeout", "no route", "broken pipe", "连接", "conn reset", "eof"}
	for _, k := range keys {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func archiveLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func archiveDayStart(now time.Time) time.Time {
	t := now.In(archiveLocation())
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func parseArchiveTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	return nil
}

func SeedAlertRules(st *store.DB) error {
	if st == nil {
		return nil
	}
	n, err := st.CountArchiveAlertRules()
	if err != nil || n > 0 {
		return err
	}
	defaults := []store.ArchiveAlertRule{
		{RuleType: alertRunTimeout, Threshold: 30, Severity: "critical", Enabled: true},
		{RuleType: alertFailCount, Threshold: 3, Severity: "warning", Enabled: true},
		{RuleType: alertStorageUsage, Threshold: 10, Severity: "notice", Enabled: true},
		{RuleType: alertDBDisconnect, Threshold: 1, Severity: "critical", Enabled: true},
	}
	for i := range defaults {
		r := defaults[i]
		r.Title = AlertRuleTitle(r.RuleType, r.Threshold, "")
		if err := st.InsertArchiveAlertRule(&r); err != nil {
			return err
		}
	}
	return nil
}

// EvaluateAlerts 按已保存的规则检查运行、失败、存储和断连，新事件落库并按设置通知。
func EvaluateAlerts(ctx context.Context, st *store.DB) {
	if st == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := SeedAlertRules(st); err != nil {
		log.Printf("archive seed alert rules: %v", err)
		return
	}
	rules, err := st.ListArchiveAlertRules()
	if err != nil {
		log.Printf("archive list alert rules: %v", err)
		return
	}
	now := time.Now()
	since := archiveDayStart(now).UTC().Format(time.RFC3339)
	running, err := st.ListArchiveRunsSince(store.StatusRunning, "", 200)
	if err != nil {
		log.Printf("archive list running: %v", err)
		return
	}
	failed, err := st.ListArchiveRunsSince(store.StatusFailed, since, 200)
	if err != nil {
		log.Printf("archive list failed: %v", err)
		return
	}
	failedToday, err := st.CountArchiveRunsSince(store.StatusFailed, since)
	if err != nil {
		log.Printf("archive count failed: %v", err)
		failedToday = len(failed)
	}
	raw, _ := st.GetGlobalArg(store.GlobalArgArchiveSettings)
	cfg := ParseAlertSettings(raw)
	for _, rule := range rules {
		hits := evalAlertRule(rule, running, failed, failedToday, now, cfg.Storage)
		for _, hit := range hits {
			ev := &store.ArchiveAlertEvent{
				RuleID: rule.ID, RuleType: rule.RuleType, Fingerprint: hit.Fingerprint,
				Message: hit.Message, Severity: rule.Severity,
			}
			inserted, err := st.InsertArchiveAlertEvent(ev)
			if err != nil {
				log.Printf("archive insert alert: %v", err)
				continue
			}
			if !inserted {
				continue
			}
			if err := NotifyAlert(ctx, cfg, hit.Message); err != nil {
				log.Printf("archive notify: %v", err)
			}
		}
	}
}
