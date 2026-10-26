package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

var errExecStopped = errors.New("执行已终止")

var ticketExecCancel sync.Map // issue id -> context.CancelFunc

func bindExecCancel(id string, cancel context.CancelFunc) {
	id = strings.TrimSpace(id)
	if id == "" || cancel == nil {
		return
	}
	ticketExecCancel.Store(id, cancel)
}

func unbindExecCancel(id string) {
	ticketExecCancel.Delete(strings.TrimSpace(id))
}

func cancelExec(id string) {
	id = strings.TrimSpace(id)
	if v, ok := ticketExecCancel.Load(id); ok {
		if cancel, ok := v.(context.CancelFunc); ok && cancel != nil {
			cancel()
		}
	}
}

func (h *TicketHandler) issueStopped(id string) bool {
	if h == nil || h.Store == nil {
		return false
	}
	cur, err := h.Store.GetIssue(id)
	return err == nil && cur != nil && cur.Status == store.StatusStopped
}

func (h *TicketHandler) saveProgress(id string, p *store.ExecProgress) {
	if h == nil || h.Store == nil || strings.TrimSpace(id) == "" {
		return
	}
	_ = h.Store.SaveIssueProgress(id, p)
}

func (h *TicketHandler) checkpointExec(it *store.Issue, res *ticketexec.Result) error {
	if it == nil {
		return nil
	}
	if h.issueStopped(it.ID) {
		return errExecStopped
	}
	ok, err := h.Store.UpdateIssueExecRunning(it.ID, ticketexec.ToJSON(res))
	if err != nil {
		return err
	}
	if !ok {
		return errExecStopped
	}
	return nil
}

func appendCompactDB(merged *ticketexec.Result, rec ticketexec.Record, ok bool, successN, failN int) {
	if merged == nil {
		return
	}
	if ok {
		rec.Status = "success"
		if strings.TrimSpace(rec.SQL) == "" {
			rec.SQL = fmt.Sprintf("成功 %d 条", successN)
		}
	} else {
		rec.Status = "failed"
		merged.Success = false
		if strings.TrimSpace(rec.SQL) == "" {
			if failN > 0 || successN > 0 {
				rec.SQL = fmt.Sprintf("成功 %d 条，失败 %d 条", successN, failN)
			} else {
				rec.SQL = "执行失败"
			}
		}
	}
	rec.Order = len(merged.Records) + 1
	merged.Records = append(merged.Records, rec)
	merged.SuccessCount += successN
	merged.FailCount += failN
}

// watchSession 在语句开始前把当前库会话号写入进度，供终止命令使用。不改 DDL 的库级进度。
func (h *TicketHandler) watchSession(it *store.Issue, inst *store.Instance, host, dbName string, t *ticketexec.Target) {
	if h == nil || it == nil || inst == nil || t == nil || t.OnProgress != nil {
		return
	}
	var last time.Time
	var mu sync.Mutex
	t.OnProgress = func(stmtIndex, stmtTotal int, sessionID int64) {
		now := time.Now()
		mu.Lock()
		write := stmtIndex == 1 || sessionID > 0 && last.IsZero() || now.Sub(last) >= 2*time.Second
		if write {
			last = now
		}
		mu.Unlock()
		if !write {
			return
		}
		engine := ticketexec.EngineOf(inst.DBType)
		if strings.EqualFold(strings.TrimSpace(inst.DBType), "redis") {
			engine = "redis"
		}
		h.saveProgress(it.ID, &store.ExecProgress{
			CurrentInstance: inst.Name,
			CurrentDB:       dbName,
			InstanceID:      inst.ID,
			Host:            host,
			DBType:          inst.DBType,
			StmtIndex:       stmtIndex,
			StmtTotal:       stmtTotal,
			SessionID:       sessionID,
			Engine:          engine,
		})
	}
}
