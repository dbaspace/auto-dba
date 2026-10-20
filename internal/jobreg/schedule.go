package jobreg

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseDurationSpec(spec string) (time.Duration, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, fmt.Errorf("empty interval")
	}
	if d, err := time.ParseDuration(spec); err == nil && d > 0 {
		return d, nil
	}
	if sec, err := strconv.Atoi(spec); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second, nil
	}
	return 0, fmt.Errorf("invalid interval %q", spec)
}

func parseCron6HMS(spec string) (min, hour int, ok bool) {
	parts := strings.Fields(strings.TrimSpace(spec))
	if len(parts) != 6 {
		return 0, 0, false
	}
	for _, p := range parts[3:] {
		if p != "*" {
			return 0, 0, false
		}
	}
	parseField := func(s string) (int, bool) {
		if strings.ContainsAny(s, "*/,-") {
			return 0, false
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	_, ok1 := parseField(parts[0])
	min, ok2 := parseField(parts[1])
	hour, ok3 := parseField(parts[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, 0, false
	}
	if min < 0 || min > 59 || hour < 0 || hour > 23 {
		return 0, 0, false
	}
	return min, hour, true
}

func humanSchedule(kind, spec string) string {
	switch kind {
	case ScheduleInterval:
		d, err := parseDurationSpec(spec)
		if err != nil {
			return spec
		}
		if d%time.Minute == 0 {
			return fmt.Sprintf("每 %d 分钟", int(d/time.Minute))
		}
		return fmt.Sprintf("每 %s", d)
	case ScheduleCron6:
		min, hour, ok := parseCron6HMS(spec)
		if !ok {
			return spec
		}
		return fmt.Sprintf("每日 %02d:%02d", hour, min)
	case ScheduleCron5:
		return "cron: " + spec
	default:
		return spec
	}
}

// dueNow 判断当前是否应触发（不含 ledger/lease）。
func dueNow(kind, spec string, now time.Time) (fireKey string, due bool) {
	switch kind {
	case ScheduleInterval:
		d, err := parseDurationSpec(spec)
		if err != nil || d <= 0 {
			return "", false
		}
		sec := int64(d.Seconds())
		if sec <= 0 {
			return "", false
		}
		bucket := now.Unix() / sec
		return fmt.Sprintf("i:%d", bucket), true
	case ScheduleCron6:
		min, hour, ok := parseCron6HMS(spec)
		if !ok {
			return "", false
		}
		if now.Minute() != min || now.Hour() != hour {
			return "", false
		}
		day := now.Format("2006-01-02")
		return "d:" + day, true
	case ScheduleCron5:
		if !cron5Match(spec, now) {
			return "", false
		}
		// 对齐到分钟
		t := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), 0, 0, now.Location())
		return "c:" + t.Format(time.RFC3339), true
	default:
		return "", false
	}
}

func nextFireAt(kind, spec string, now time.Time) time.Time {
	switch kind {
	case ScheduleInterval:
		d, err := parseDurationSpec(spec)
		if err != nil || d <= 0 {
			return time.Time{}
		}
		sec := int64(d.Seconds())
		nextBucket := now.Unix()/sec + 1
		return time.Unix(nextBucket*sec, 0).In(now.Location())
	case ScheduleCron6:
		min, hour, ok := parseCron6HMS(spec)
		if !ok {
			return time.Time{}
		}
		cand := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
		if !cand.After(now) {
			cand = cand.AddDate(0, 0, 1)
		}
		return cand
	default:
		return time.Time{}
	}
}

// missedFireKey 若计划点已过且仍在 grace 内、需要补跑，返回应补的 fire_key。
func missedFireKey(kind, spec string, now time.Time, grace time.Duration) (fireKey string, ok bool) {
	if grace <= 0 {
		grace = 24 * time.Hour
	}
	switch kind {
	case ScheduleCron6:
		min, hour, okp := parseCron6HMS(spec)
		if !okp {
			return "", false
		}
		planned := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
		if now.Before(planned) {
			// 看昨天
			planned = planned.AddDate(0, 0, -1)
		}
		if now.Sub(planned) > grace || now.Before(planned) {
			return "", false
		}
		return "d:" + planned.Format("2006-01-02"), true
	default:
		return "", false
	}
}

func cron5Match(expr string, now time.Time) bool {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return false
	}
	return cronFieldMatch(fields[0], now.Minute(), 0, 59) &&
		cronFieldMatch(fields[1], now.Hour(), 0, 23) &&
		cronFieldMatch(fields[2], now.Day(), 1, 31) &&
		cronFieldMatch(fields[3], int(now.Month()), 1, 12) &&
		cronFieldMatch(fields[4], int(now.Weekday()), 0, 6)
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
			ab := strings.SplitN(part, "-", 2)
			if len(ab) != 2 {
				continue
			}
			a, e1 := strconv.Atoi(ab[0])
			b, e2 := strconv.Atoi(ab[1])
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
