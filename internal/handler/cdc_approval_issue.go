package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/store"
)

// reconcileCDCApprovalIssue 任务仍待审批、但 issue_id 指向的工单不是 pending 时，改绑到本次提交的 pending 工单。
func (h *CDCHandler) reconcileCDCApprovalIssue(t *store.CDCTask) {
	if h == nil || h.Store == nil || t == nil {
		return
	}
	if t.ApprovalStatus != store.StatusPending && t.Status != store.StatusPending {
		return
	}
	if cdcIssueIsPending(h.Store, t.IssueID) {
		return
	}
	id := h.findPendingCDCIssue(t)
	if id == "" || id == strings.TrimSpace(t.IssueID) {
		return
	}
	if err := h.Store.UpdateCDCIssueID(t.ID, id); err != nil {
		return
	}
	t.IssueID = id
}

func cdcIssueIsPending(db *store.DB, id string) bool {
	id = strings.TrimSpace(id)
	if db == nil || id == "" {
		return false
	}
	it, err := db.GetIssue(id)
	return err == nil && it != nil && it.Status == store.StatusPending && it.IssueType == store.IssueTypeCDC
}

func (h *CDCHandler) findPendingCDCIssue(t *store.CDCTask) string {
	if t == nil || h == nil || h.Store == nil {
		return ""
	}
	if t.ParentID != "" {
		kids, _ := h.Store.ListCDCByParentID(t.ParentID, t.TaskType)
		for i := range kids {
			if kids[i].ID == t.ID {
				continue
			}
			if cdcIssueIsPending(h.Store, kids[i].IssueID) {
				return kids[i].IssueID
			}
		}
	}
	extra := cdc.ParseTaskExtra(t.Description)
	assignee := strings.TrimSpace(extra["assigned_to"])
	submitter := strings.TrimSpace(extra["created_by"])
	tid := strings.TrimSpace(t.TenantID)
	if tid == "" {
		tid = h.Store.RowTenantID("tbl_cdc_tasks", t.ID)
	}
	list, err := h.Store.ListIssuesInTenant(tid, store.IssueTypeCDC, store.StatusPending, "", "", true)
	if err != nil {
		return ""
	}
	taskAt := parseCDCTime(t.CreatedAt)
	bestID := ""
	bestScore := -1
	bestDelta := time.Duration(1<<63 - 1)
	prefix := "cdc_sync " + strings.TrimSpace(t.TaskType)
	for i := range list {
		it := &list[i]
		sql := strings.TrimSpace(it.SQLContent)
		if !strings.HasPrefix(sql, prefix) {
			continue
		}
		if t.SourceID != "" && it.InstanceID != "" && it.InstanceID != t.SourceID {
			continue
		}
		if t.SourceDB != "" && it.DBName != "" && !strings.EqualFold(strings.TrimSpace(it.DBName), strings.TrimSpace(t.SourceDB)) {
			continue
		}
		if assignee != "" && it.AssignedTo != "" && !strings.EqualFold(it.AssignedTo, assignee) {
			continue
		}
		if submitter != "" && it.Submitter != "" && !strings.EqualFold(it.Submitter, submitter) {
			continue
		}
		score := 0
		if t.ID != "" && strings.Contains(sql, t.ID) {
			score += 100
		}
		delta := bestDelta
		if at := parseCDCTime(it.CreatedAt); !taskAt.IsZero() && !at.IsZero() {
			delta = at.Sub(taskAt)
			if delta < 0 {
				delta = -delta
			}
		} else {
			delta = 0
		}
		if score > bestScore || (score == bestScore && delta < bestDelta) {
			bestScore = score
			bestDelta = delta
			bestID = it.ID
		}
	}
	return bestID
}

func parseCDCTime(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t
	}
	return time.Time{}
}

// ResolveApprovalIssue POST /cdc-sync/tasks/:id/resolve-approval-issue
// 把任务挂到仍为 pending 的 CDC 工单。找不到时不改批准接口。
func (h *CDCHandler) ResolveApprovalIssue(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	if strings.TrimSpace(t.TenantID) == "" {
		t.TenantID = h.Store.RowTenantID("tbl_cdc_tasks", t.ID)
	}
	h.reconcileCDCApprovalIssue(t)
	status := ""
	if it, _ := h.Store.GetIssue(t.IssueID); it != nil {
		status = it.Status
	}
	pending := status == store.StatusPending
	body := gin.H{"data": gin.H{"issue_id": t.IssueID, "issue_status": status, "pending": pending}}
	if !pending {
		body["message"] = "关联工单不是待审批"
	}
	c.JSON(http.StatusOK, body)
}
