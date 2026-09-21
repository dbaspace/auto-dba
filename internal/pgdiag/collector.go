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
func StartCollector(ctx context.Context, meta *store.DB, appSecret string) {
	interval := time.Duration(defaultIntervalSec) * time.Second
	select {
	case <-ctx.Done():
		return
	case <-time.After(firstDelay):
	}
	runOnce(ctx, meta, appSecret)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce(ctx, meta, appSecret)
		}
	}
}

func runOnce(ctx context.Context, meta *store.DB, appSecret string) {
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
