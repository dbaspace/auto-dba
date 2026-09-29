package pgspace

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
	cronLastFire string // YYYY-MM-DD of last successful daily fire
)

// StartDailyCron 启动表空间每日采集监视（每 30s 检查一次）。
// 解析 6 段 cron「秒 分 时 * * *」；匹配当前时分秒且当日未触发过则启动全量采集并清理过期快照。
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
	// 30s 轮询：匹配时分即可（秒字段仅作配置兼容，不要求精确命中）。
	if now.Minute() != min || now.Hour() != hour {
		return
	}
	day := now.Format("2006-01-02")
	if cronLastFire == day {
		return
	}
	cronLastFire = day
	log.Printf("pg_space: daily cron triggered (%s)", spec)

	if _, err := s.StartCollectJob(context.Background(), "", "", 0, "system"); err != nil {
		log.Printf("pg_space: scheduled collect failed: %v", err)
	}
	retention := s.retentionDays()
	if retention > 0 {
		before := time.Now().UTC().AddDate(0, 0, -retention)
		if n, err := s.Store.DeleteSpaceSnapshotsBefore(context.Background(), before); err != nil {
			log.Printf("pg_space: cleanup failed: %v", err)
		} else if n > 0 {
			log.Printf("pg_space: cleanup done deleted=%d retention_days=%d", n, retention)
		}
	}
}

// parseDailyCronHMS 仅支持「秒 分 时 * * *」定点每日表达式。
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
