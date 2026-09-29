package awr

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

// StartDailyCron 启动 AWR 每日生成监视（每 30s 检查一次）。
func StartDailyCron(ctx context.Context, svc *Service) {
	if svc == nil {
		return
	}
	cronOnce.Do(func() {
		svc.MarkJobsInterrupted(context.Background())
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
	if !s.enabled() {
		return
	}
	spec := s.dailyCronSpec()
	_, min, hour, ok := parseDailyCronHMS(spec)
	if !ok {
		return
	}
	now := time.Now()
	if now.Minute() != min || now.Hour() != hour {
		return
	}
	dayKey := now.Format("2006-01-02")
	if cronLastFire == dayKey {
		return
	}
	cronLastFire = dayKey
	log.Printf("awr: daily cron triggered (%s)", spec)

	day := time.Now().UTC().AddDate(0, 0, -1)
	processed, written, failed, err := s.RunGenerateJobSync(context.Background(), day, "system")
	if err != nil {
		log.Printf("awr: daily generate failed: %v", err)
	} else {
		log.Printf("awr: daily generate done day=%s processed=%d written=%d failed=%d",
			day.Format("2006-01-02"), processed, written, failed)
	}

	retention := s.retentionDays()
	if retention > 0 {
		if n, e := s.CleanupReports(context.Background(), retention); e != nil {
			log.Printf("awr: cleanup failed: %v", e)
		} else if n > 0 {
			log.Printf("awr: cleanup done deleted=%d retention_days=%d", n, retention)
		}
	}

	s.dailyPush(context.Background(), day)
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
