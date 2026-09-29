package cdc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/store"
)

const reinitTimeout = 15 * time.Minute

var (
	reinitRunning sync.Map

	ErrReinitBusy = errors.New("任务正在重新初始化，请勿重复提交")
)

// ReinitRunning 任务是否持有重新初始化锁（本进程内）。
func ReinitRunning(taskID string) bool {
	_, ok := reinitRunning.Load(taskID)
	return ok
}

func (o *Orchestrator) acquireReinit(taskID string) (*store.CDCTask, error) {
	if _, loaded := reinitRunning.LoadOrStore(taskID, struct{}{}); loaded {
		return nil, ErrReinitBusy
	}
	t, err := o.Store.GetCDC(taskID)
	if err != nil || t == nil {
		reinitRunning.Delete(taskID)
		return nil, fmt.Errorf("任务不存在")
	}
	if err := o.Store.UpdateCDCStatus(t.ID, store.StatusReinitializing, t.ApprovalStatus); err != nil {
		reinitRunning.Delete(taskID)
		return nil, err
	}
	return t, nil
}

func (o *Orchestrator) runReinitLocked(taskID string, req ReinitRequest) {
	defer reinitRunning.Delete(taskID)
	ctx, cancel := context.WithTimeout(context.Background(), reinitTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[cdc] reinit task=%s panic: %v", taskID, r)
			o.markReinitFailed(taskID, fmt.Sprintf("重新初始化失败[重新初始化]：内部错误 %v", r))
		}
	}()
	if err := o.Reinit(ctx, taskID, req); err != nil {
		log.Printf("[cdc] reinit task=%s failed: %v", taskID, err)
	}
}

// StartReinit 占任务锁、状态置 reinitializing，后台执行。
func (o *Orchestrator) StartReinit(taskID string, req ReinitRequest) error {
	if _, err := o.acquireReinit(taskID); err != nil {
		return err
	}
	go o.runReinitLocked(taskID, req)
	return nil
}

type ReinitBatchResult struct {
	TaskID   string `json:"task_id"`
	TaskName string `json:"task_name,omitempty"`
	Accepted bool   `json:"accepted"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

// StartReinitBatch 逐个占锁并置 reinitializing，受理的任务在同一 goroutine 中串行执行。
func (o *Orchestrator) StartReinitBatch(taskIDs []string, req ReinitRequest) []ReinitBatchResult {
	out := make([]ReinitBatchResult, 0, len(taskIDs))
	var accepted []string
	for _, id := range taskIDs {
		t, err := o.acquireReinit(id)
		if err != nil {
			out = append(out, ReinitBatchResult{TaskID: id, Accepted: false, Message: err.Error()})
			continue
		}
		accepted = append(accepted, id)
		out = append(out, ReinitBatchResult{TaskID: id, TaskName: t.Name, Accepted: true, Status: store.StatusReinitializing})
	}
	if len(accepted) > 0 {
		go func() {
			for _, id := range accepted {
				o.runReinitLocked(id, req)
			}
		}()
	}
	return out
}

func (o *Orchestrator) markReinitFailed(taskID, msg string) {
	t, err := o.Store.GetCDC(taskID)
	if err != nil || t == nil {
		return
	}
	extra := parseTaskExtra(t.Description)
	extra["error_step"] = "reinit"
	extra["error_message"] = msg
	extra["pipeline_status"] = "failed"
	o.saveExtra(t, extra)
	o.reinitLog(t, "ERROR", msg, "")
	_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
}

// RecoverStaleReinit 进程重启后回收卡在 reinitializing 的任务（置 failed + error_step=reinit，可重试）。
func RecoverStaleReinit(st *store.DB) int {
	if st == nil {
		return 0
	}
	list, err := st.ListCDC(store.StatusReinitializing, "")
	if err != nil {
		return 0
	}
	o := &Orchestrator{Store: st}
	n := 0
	for i := range list {
		id := strings.TrimSpace(list[i].ID)
		if id == "" || ReinitRunning(id) {
			continue
		}
		o.markReinitFailed(id, "重新初始化失败[重新初始化]：服务重启导致执行中断，可点击「重试」重新发起")
		n++
	}
	return n
}
