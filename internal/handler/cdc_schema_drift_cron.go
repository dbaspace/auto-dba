package handler

import (
	"context"
	"log"
	"time"

	"auto-dba/internal/cdc"
	"auto-dba/internal/store"
)

// StartSchemaDriftCron 已废弃：由 jobreg cdc.schema_drift 接管。
//
// Deprecated: 使用 jobboot.RegisterPlatformJobs。
func StartSchemaDriftCron(ctx context.Context, h *CDCHandler) {
	_ = ctx
	_ = h
	log.Printf("DEPRECATED: StartSchemaDriftCron ignored; scheduling is owned by jobreg (cdc.schema_drift)")
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
