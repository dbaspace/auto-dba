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

// StartCollector 已废弃：平台调度由 internal/jobreg 接管（pgdiag.activity_snapshot）。
//
// Deprecated: 使用 jobboot.RegisterPlatformJobs + jobreg.Registry.Start。
func StartCollector(ctx context.Context, meta *store.DB, appSecret *string) {
	_ = ctx
	_ = meta
	_ = appSecret
	log.Printf("DEPRECATED: pgdiag.StartCollector ignored; scheduling is owned by jobreg (pgdiag.activity_snapshot)")
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
