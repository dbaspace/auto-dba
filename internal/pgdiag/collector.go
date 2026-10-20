package pgdiag

import (
	"context"
	"log"
	"time"

	"auto-dba/internal/store"
)

const (
	defaultIntervalSec = 300
	defaultRetentionD  = 7
	firstDelay         = 30 * time.Second
)

// StartCollector 后台定时采集全部 PostgreSQL 实例活动快照，并按保留天数清理。
// appSecret 指向配置中的 jwt_secret 字段，热更后无需重启采集协程。
func StartCollector(ctx context.Context, meta *store.DB, appSecret *string) {
	interval := time.Duration(defaultIntervalSec) * time.Second
	select {
	case <-ctx.Done():
		return
	case <-time.After(firstDelay):
	}
	RunOnce(ctx, meta, derefSecret(appSecret))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			RunOnce(ctx, meta, derefSecret(appSecret))
		}
	}
}

func derefSecret(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// RunOnce 执行一轮活动快照采集与清理（供 jobreg 调用）。
func RunOnce(ctx context.Context, meta *store.DB, appSecret string) error {
	runOnceInner(ctx, meta, appSecret)
	return nil
}

func runOnceInner(ctx context.Context, meta *store.DB, appSecret string) {
	res, warns, err := Collect(ctx, meta, appSecret, "")
	if err != nil {
		log.Printf("pg activity snapshot collect: %v", err)
		return
	}
	for _, w := range warns {
		log.Printf("pg activity snapshot: %s", w)
	}
	if res.InstancesProcessed > 0 || res.RowsWritten > 0 {
		log.Printf("pg activity snapshot: instances=%d rows=%d", res.InstancesProcessed, res.RowsWritten)
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -defaultRetentionD)
	if n, err := meta.DeletePGActivitySnapshotsBefore(cutoff); err != nil {
		log.Printf("pg activity snapshot cleanup: %v", err)
	} else if n > 0 {
		log.Printf("pg activity snapshot cleanup: deleted=%d", n)
	}
}
