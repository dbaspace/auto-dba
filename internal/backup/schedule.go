package backup

import (
	"strconv"
	"strings"
	"time"
)

// scheduleDue 支持 HH:MM 或 5 段 cron（分 时 日 月 周）。
func scheduleDue(expr, lastRun string, now time.Time) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	if strings.Contains(expr, " ") {
		return cronDue(expr, lastRun, now)
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
	due := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if now.Before(due) {
		return false
	}
	if lastRun == "" {
		return true
	}
	last, ok := parseTime(lastRun)
	if !ok {
		return true
	}
	return last.Before(due)
}

func cronDue(expr, lastRun string, now time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	if !cronFieldMatch(fields[0], now.Minute(), 0, 59) ||
		!cronFieldMatch(fields[1], now.Hour(), 0, 23) ||
		!cronFieldMatch(fields[2], now.Day(), 1, 31) ||
		!cronFieldMatch(fields[3], int(now.Month()), 1, 12) ||
		!cronFieldMatch(fields[4], int(now.Weekday()), 0, 6) {
		return false
	}
	if lastRun == "" {
		return true
	}
	last, ok := parseTime(lastRun)
	if !ok {
		return true
	}
	return now.Sub(last) >= 55*time.Second
}

func cronFieldMatch(field string, value, min, max int) bool {
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
	}
	return false
}

func parseTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	layouts := []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
