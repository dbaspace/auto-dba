package handler

import (
	"context"
	"log"
	"time"

	"auto-dba/internal/store"
)

const (
	issuePendingAutoCancelTTL      = 8 * time.Hour
	issuePendingAutoCancelInterval = 5 * time.Minute
	issuePendingAutoCancelReason   = "超过 8 小时未审批，系统自动取消"
)

func issuePendingAutoCancelDeadline(now time.Time) time.Time {
	return now.UTC().Add(-issuePendingAutoCancelTTL)
}

// StartIssueAutoCancel 已废弃：由 jobreg ticket.auto_cancel 接管。
//
// Deprecated: 使用 jobboot.RegisterPlatformJobs。
func StartIssueAutoCancel(ctx context.Context, h *TicketHandler) {
	_ = ctx
	_ = h
	log.Printf("DEPRECATED: StartIssueAutoCancel ignored; scheduling is owned by jobreg (ticket.auto_cancel)")
}

// RunIssueAutoCancelOnce 供 jobreg 调用。
func RunIssueAutoCancelOnce(ctx context.Context, h *TicketHandler) error {
	n, err := h.closeStalePendingIssues(ctx, time.Now())
	if err != nil {
		log.Printf("issue_pending_auto_cancel: %v", err)
		return err
	}
	if n > 0 {
		log.Printf("issue_pending_auto_cancel: closed=%d", n)
	}
	return nil
}

func (h *TicketHandler) closeStalePendingIssues(ctx context.Context, now time.Time) (int, error) {
	if h == nil || h.Store == nil {
		return 0, nil
	}
	items, err := h.Store.ListStalePendingIssues(issuePendingAutoCancelDeadline(now))
	if err != nil {
		return 0, err
	}
	closed := 0
	for i := range items {
		it := &items[i]
		if it.Status != store.StatusPending {
			continue
		}
		if err := h.cancelIssue(ctx, it, issuePendingAutoCancelReason); err != nil {
			log.Printf("issue_pending_auto_cancel_one_failed id=%s: %v", it.ID, err)
			continue
		}
		closed++
	}
	return closed, nil
}
