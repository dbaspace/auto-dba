package handler

import (
	"context"
	"log"
	"time"

	"auto-dba/internal/store"
)

// StartAwaitExecStuckNotify 已废弃：由 jobreg ticket.await_exec_notify 接管。
//
// Deprecated: 使用 jobboot.RegisterPlatformJobs。
func StartAwaitExecStuckNotify(ctx context.Context, h *TicketHandler) {
	_ = ctx
	_ = h
	log.Printf("DEPRECATED: StartAwaitExecStuckNotify ignored; scheduling is owned by jobreg (ticket.await_exec_notify)")
}

// RunAwaitExecStuckNotifyOnce 供 jobreg 调用。
func RunAwaitExecStuckNotifyOnce(ctx context.Context, h *TicketHandler) (int, error) {
	n, err := h.notifyStaleAwaitExecIssues(ctx, time.Now())
	if err != nil {
		log.Printf("await-exec stuck notify: %v", err)
		return 0, err
	}
	if n > 0 {
		log.Printf("await-exec stuck notify: sent %d", n)
	}
	return n, nil
}

func (h *TicketHandler) notifyStaleAwaitExecIssues(ctx context.Context, now time.Time) (int, error) {
	if h == nil || h.Store == nil {
		return 0, nil
	}
	_ = ctx
	ncfg, err := h.Store.GetNotifyConfig()
	if err != nil || ncfg == nil || !ncfg.EmailOnTicketAwaitStuck {
		return 0, err
	}
	hours := ncfg.AwaitStuckHours()
	before := now.Add(-time.Duration(hours) * time.Hour)
	list, err := h.Store.ListStaleApprovedIssues(before)
	if err != nil {
		return 0, err
	}
	sent := 0
	for i := range list {
		it := &list[i]
		if store.IssueAwaitStuckAlreadyNotified(it) {
			continue
		}
		if !store.ScheduledAtDueOrEmpty(it.ScheduledAt, now) {
			continue
		}
		if err := h.Store.MarkIssueAwaitStuckNotified(it.ID); err != nil {
			log.Printf("await-exec stuck mark %s: %v", it.ID, err)
			continue
		}
		NotifyIssueAwaitExecStuckAsync(h.Store, h.Secret, it)
		sent++
	}
	return sent, nil
}
