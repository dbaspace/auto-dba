package handler

import (
	"context"
	"log"
	"time"

	"auto-dba/internal/ticketexec"
)

// RunDueScheduledIssuesOnce 扫描预约到期的已通过工单并启动后台执行（供 jobreg 调用）。
func RunDueScheduledIssuesOnce(ctx context.Context, h *TicketHandler) error {
	if h == nil || h.Store == nil {
		return nil
	}
	_ = ctx
	list, err := h.Store.ListDueScheduledIssues(time.Now(), 50)
	if err != nil {
		return err
	}
	started := 0
	for i := range list {
		it := &list[i]
		if !ticketexec.ShouldAutoRun(it.IssueType) {
			continue
		}
		if ticketexec.IsExecuting(it.Status) {
			continue
		}
		if err := h.beginAsyncAutoExec(it); err != nil {
			log.Printf("ticket_scheduled_exec: id=%s: %v", it.ID, err)
			continue
		}
		started++
		log.Printf("ticket_scheduled_exec: started id=%s ticket=%s scheduled_at=%s", it.ID, it.TicketNo, it.ScheduledAt)
	}
	if started > 0 {
		log.Printf("ticket_scheduled_exec: started=%d", started)
	}
	return nil
}
