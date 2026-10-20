package cmdb

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"
)

// ScanDueCron 扫描启用的同步任务，按 cron/HH:MM 触发（供 jobreg 调用）。
func (s *Service) ScanDueCron(ctx context.Context) error {
	if s == nil || s.Store == nil {
		return nil
	}
	jobs, err := s.Store.ListCmdbSyncJobs()
	if err != nil {
		return err
	}
	now := time.Now()
	for _, j := range jobs {
		if !j.Enabled || strings.TrimSpace(j.Cron) == "" {
			continue
		}
		last := ""
		if runs, err := s.Store.ListCmdbSyncRuns(j.ID, 1); err == nil && len(runs) > 0 {
			last = runs[0].StartedAt
		}
		if !cmdbScheduleDue(j.Cron, last, now) {
			continue
		}
		if _, _, err := s.RunSyncJob(ctx, j.ID, "scheduler"); err != nil {
			log.Printf("cmdb sync cron job=%s: %v", j.ID, err)
		}
	}
	return nil
}

func cmdbScheduleDue(expr, lastRun string, now time.Time) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	if strings.Contains(expr, " ") {
		return cmdbCronDue(expr, lastRun, now)
	}
	parts := strings.Split(expr, ":")
	if len(parts) != 2 {
		return false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return false
	}
	if now.Hour() != h || now.Minute() != m {
		return false
	}
	if lastRun == "" {
		return true
	}
	if t, ok := parseCmdbTime(lastRun); ok && t.Format("2006-01-02") == now.Format("2006-01-02") {
		return false
	}
	return true
}

func cmdbCronDue(expr, lastRun string, now time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	if !cmdbCronField(fields[0], now.Minute(), 0, 59) ||
		!cmdbCronField(fields[1], now.Hour(), 0, 23) ||
		!cmdbCronField(fields[2], now.Day(), 1, 31) ||
		!cmdbCronField(fields[3], int(now.Month()), 1, 12) ||
		!cmdbCronField(fields[4], int(now.Weekday()), 0, 6) {
		return false
	}
	if lastRun == "" {
		return true
	}
	if t, ok := parseCmdbTime(lastRun); ok && now.Sub(t) < 55*time.Second {
		return false
	}
	return true
}

func cmdbCronField(field string, value, min, max int) bool {
	field = strings.TrimSpace(field)
	if field == "*" {
		return true
	}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err != nil || step <= 0 {
				continue
			}
			if value%step == 0 {
				return true
			}
			continue
		}
		if strings.Contains(part, "-") {
			rg := strings.SplitN(part, "-", 2)
			if len(rg) != 2 {
				continue
			}
			a, e1 := strconv.Atoi(rg[0])
			b, e2 := strconv.Atoi(rg[1])
			if e1 != nil || e2 != nil {
				continue
			}
			if value >= a && value <= b {
				return true
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err == nil && n == value {
			return true
		}
		_ = min
		_ = max
	}
	return false
}

func parseCmdbTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
