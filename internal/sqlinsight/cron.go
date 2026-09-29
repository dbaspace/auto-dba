package sqlinsight

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	cronOnce     sync.Once
	cronLastFire string
)

// StartDailyCron 启动智能分析每日定时（每 30s 检查一次）。
func StartDailyCron(ctx context.Context, svc *Service) {
	if svc == nil {
		return
	}
	cronOnce.Do(func() {
		_ = svc.SeedSystemCases(context.Background())
		_ = svc.SeedSystemRules(context.Background())
		go svc.dailyCronWatcher(ctx)
	})
}

func (s *Service) dailyCronWatcher(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.maybeFireDailyCron()
		}
	}
}

func (s *Service) maybeFireDailyCron() {
	if !s.cronEnabled() {
		return
	}
	spec := s.cronSpec()
	_, min, hour, ok := parseDailyCronHMS(spec)
	if !ok {
		return
	}
	now := time.Now()
	if now.Minute() != min || now.Hour() != hour {
		return
	}
	day := now.Format("2006-01-02")
	if cronLastFire == day {
		return
	}
	cronLastFire = day
	log.Printf("sql_insight: daily cron triggered (%s)", spec)

	prev := now.In(time.Local).AddDate(0, 0, -1)
	prev = time.Date(prev.Year(), prev.Month(), prev.Day(), 0, 0, 0, 0, time.Local)
	if _, err := s.ExecuteRun(context.Background(), prev, "", 0, "cron", "system", nil); err != nil {
		log.Printf("sql_insight: daily analysis failed: %v", err)
	}
}

func (s *Service) cronEnabled() bool {
	v, err := s.Store.GetGlobalArg(siArgCronEnabled)
	if err != nil || strings.TrimSpace(v) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "on", "yes":
		return true
	default:
		return false
	}
}

func (s *Service) cronSpec() string {
	v, err := s.Store.GetGlobalArg(siArgCronSpec)
	if err != nil || strings.TrimSpace(v) == "" {
		return siDefaultCronSpec
	}
	return strings.TrimSpace(v)
}

func parseDailyCronHMS(spec string) (sec, min, hour int, ok bool) {
	parts := strings.Fields(strings.TrimSpace(spec))
	if len(parts) != 6 {
		return 0, 0, 0, false
	}
	for _, p := range parts[3:] {
		if p != "*" {
			return 0, 0, 0, false
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
	sec, ok1 := parseField(parts[0])
	min, ok2 := parseField(parts[1])
	hour, ok3 := parseField(parts[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, 0, 0, false
	}
	if sec < 0 || sec > 59 || min < 0 || min > 59 || hour < 0 || hour > 23 {
		return 0, 0, 0, false
	}
	return sec, min, hour, true
}
