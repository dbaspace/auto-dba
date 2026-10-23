package cdc

import (
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

const inflightRecoverStale = 300 * time.Second

// RecoverInflight 启动时把上次进程没跑完的编排标成失败，便于从 error_step 重试。
func (o *Orchestrator) RecoverInflight() (int, error) {
	if o == nil || o.Store == nil {
		return 0, nil
	}
	list, err := o.Store.ListCDC("", "")
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-inflightRecoverStale)
	n := 0
	for i := range list {
		t := &list[i]
		extra := parseTaskExtra(t.Description)
		inflight := extra["orchestrating"] == "true" || t.Status == store.StatusReinitializing
		if !inflight || t.Status == store.StatusFailed || t.Status == store.StatusDeleted {
			continue
		}
		updated, perr := time.Parse(time.RFC3339, strings.TrimSpace(t.UpdatedAt))
		if perr == nil && updated.After(cutoff) {
			continue
		}
		step := strings.TrimSpace(extra["error_step"])
		if step == "" {
			if t.Status == store.StatusReinitializing {
				step = "reinit"
			} else {
				step = "pg"
			}
		}
		msg := fmt.Sprintf("编排进程中断（疑似服务重启），任务停留在 %s；已标为失败，可在详情重试", t.Status)
		extra["error_step"] = step
		extra["error_message"] = msg
		delete(extra, "orchestrating")
		o.saveExtra(t, extra)
		_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
		o.appendLog(t, "WARN", "orchestrate", msg)
		n++
	}
	return n, nil
}
