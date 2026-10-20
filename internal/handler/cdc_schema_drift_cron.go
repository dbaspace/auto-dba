package handler

import (
	"context"
	"log"
	"time"

	"auto-dba/internal/cdc"
	"auto-dba/internal/store"
)

const schemaDriftCronInterval = 300 * time.Second

// StartSchemaDriftCron 对齐 Hub syncCdcSchemaDrift：周期性扫 running/paused init 写内存缓存。
func StartSchemaDriftCron(ctx context.Context, h *CDCHandler) {
	if h == nil || h.Store == nil {
		return
	}
	RunSchemaDriftScanOnce(ctx, h)
	ticker := time.NewTicker(schemaDriftCronInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			RunSchemaDriftScanOnce(ctx, h)
		}
	}
}

// RunSchemaDriftScanOnce 供 jobreg 调用。
func RunSchemaDriftScanOnce(parent context.Context, h *CDCHandler) {
	list, err := h.Store.ListCDCInTenant("", "", "init")
	if err != nil {
		log.Printf("cdc_schema_drift_cron: list: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	scanned, drifted := 0, 0
	for i := range list {
		if ctx.Err() != nil {
			break
		}
		t := &list[i]
		if t.Status != store.StatusRunning && t.Status != store.StatusPaused {
			continue
		}
		data, serr := cdc.ScanSchemaAlerts(ctx, h.Store, h.Secret, t)
		if serr != nil || data == nil {
			continue
		}
		cdc.SetSchemaDriftCache(t.ID, data)
		scanned++
		if data.DriftTableCount > 0 || data.State == cdc.SchemaAlertWarn {
			drifted++
		}
	}
	if scanned > 0 {
		log.Printf("cdc_schema_drift_cron: scanned=%d drifted=%d", scanned, drifted)
	}
}
