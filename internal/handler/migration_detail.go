package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
)

func (h *MigrationHandler) decorateTask(t *store.MigrationTask) {
	if t == nil || h == nil {
		return
	}
	if h.Store != nil && (strings.TrimSpace(t.SourceHost) == "" || t.SourcePort == 0) {
		_ = h.fillEndpointsRaw(t)
	}
	t.TaskName = t.Name
	t.AssignedApproverLogin = t.AssignedApprover
	objs := t.Objects
	if len(objs) == 0 && h.Store != nil {
		objs, _ = h.Store.ListMigrationObjects(t.ID)
		t.Objects = objs
	}
	dbs := map[string]struct{}{}
	var sumBytes, doneBytes, sumRows int64
	for i := range objs {
		o := &objs[i]
		if o.SourceDB != "" {
			dbs[o.SourceDB] = struct{}{}
		}
		sumBytes += o.TotalBytes
		doneBytes += o.CompletedBytes
		sumRows += o.TotalRows
	}
	if t.TotalObjects == 0 {
		t.TotalObjects = len(objs)
	}
	if t.TotalRows == 0 {
		t.TotalRows = sumRows
	}
	if t.TotalBytes == 0 {
		t.TotalBytes = sumBytes
	}
	t.CompletedBytes = doneBytes

	tags := map[string]any{}
	if h.Store != nil {
		tags = h.Store.GetMigrationTags(t.ID)
	}
	sel, _ := tags["selection"].(map[string]any)
	if sel == nil {
		sel = map[string]any{}
	}
	if _, ok := sel["db_count"]; !ok {
		sel["db_count"] = len(dbs)
	}
	if _, ok := sel["table_count"]; !ok {
		sel["table_count"] = len(objs)
	}
	if _, ok := sel["estimated_total_bytes"]; !ok {
		sel["estimated_total_bytes"] = t.TotalBytes
	}
	tags["selection"] = sel
	t.Tags = tags

	if t.IssueID != "" && h.Store != nil {
		if iss, err := h.Store.GetIssue(t.IssueID); err == nil && iss != nil {
			t.SubmittedAt = iss.CreatedAt
			switch iss.Status {
			case store.StatusApproved, store.StatusExecuted, store.StatusFailed:
				t.ApprovedAt = iss.UpdatedAt
			case store.StatusRejected:
				t.RejectedAt = iss.UpdatedAt
			case store.StatusCanceled:
				if strings.Contains(iss.RejectReason, "驳回") {
					t.RejectedAt = iss.UpdatedAt
				}
			}
		}
	}
	if t.SubmittedAt == "" && t.ApprovalStatus != "" && t.ApprovalStatus != "draft" {
		t.SubmittedAt = t.CreatedAt
	}
	if t.ApprovedAt == "" && t.ApprovalStatus == store.StatusApproved && t.UpdatedAt != "" {
		t.ApprovedAt = t.UpdatedAt
	}
	if t.RejectedAt == "" && t.ApprovalStatus == store.StatusRejected && t.FinishedAt != "" {
		t.RejectedAt = t.FinishedAt
	}
	if t.StartMode == "scheduled" && strings.TrimSpace(t.ScheduledAt) != "" {
		if at, ok := parseFlexibleTime(t.ScheduledAt); ok {
			sec := int64(time.Until(at).Seconds())
			if sec < 0 {
				sec = 0
			}
			t.SecondsUntilScheduled = &sec
		}
	}
}

func parseFlexibleTime(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04",
	}
	for _, layout := range layouts {
		if tm, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return tm, true
		}
		if tm, err := time.Parse(layout, s); err == nil {
			return tm, true
		}
	}
	return time.Time{}, false
}

func migrationCurrentObject(objs []store.MigrationObject) string {
	for _, o := range objs {
		if o.Status == store.StatusRunning {
			return formatMigrationObjectName(o)
		}
	}
	return ""
}

func formatMigrationObjectName(o store.MigrationObject) string {
	parts := []string{o.SourceDB}
	if o.SourceSchema != "" {
		parts = append(parts, o.SourceSchema)
	}
	parts = append(parts, o.SourceTable)
	return strings.Join(parts, ".")
}

func (h *MigrationHandler) buildProgress(t *store.MigrationTask) gin.H {
	h.decorateTask(t)
	elapsed := 0
	if st, ok := parseFlexibleTime(t.StartedAt); ok {
		end := time.Now()
		if fin, ok := parseFlexibleTime(t.FinishedAt); ok {
			end = fin
		}
		if end.After(st) {
			elapsed = int(end.Sub(st).Seconds())
		}
	}
	speedRows := 0.0
	speedBytes := 0.0
	if elapsed > 0 {
		speedRows = float64(t.CompletedRows) / float64(elapsed)
		speedBytes = float64(t.CompletedBytes) / float64(elapsed)
	}
	remain := 0
	if t.CompletedRows > 0 && t.TotalRows > t.CompletedRows && speedRows > 0 {
		remain = int(float64(t.TotalRows-t.CompletedRows) / speedRows)
	}
	po, pr := 0.0, 0.0
	if t.TotalObjects > 0 {
		po = float64(t.CompletedObjects) * 100 / float64(t.TotalObjects)
	}
	if t.TotalRows > 0 {
		pr = float64(t.CompletedRows) * 100 / float64(t.TotalRows)
	}
	if t.Status == store.StatusCompleted {
		po = 100
	}
	return gin.H{
		"task_id":                     t.ID,
		"status":                      t.Status,
		"total_objects":               t.TotalObjects,
		"completed_objects":           t.CompletedObjects,
		"total_rows":                  t.TotalRows,
		"completed_rows":              t.CompletedRows,
		"total_bytes":                 t.TotalBytes,
		"completed_bytes":             t.CompletedBytes,
		"percent_objects":             po,
		"percent_rows":                pr,
		"current_object":              migrationCurrentObject(t.Objects),
		"speed_rows_per_sec":          speedRows,
		"speed_bytes_per_sec":         speedBytes,
		"elapsed_seconds":             elapsed,
		"estimated_remaining_seconds": remain,
	}
}

func (h *MigrationHandler) VerifyPost(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	var req dbmig.VerifyReq
	_ = c.ShouldBindJSON(&req)
	if !req.RowCount && !req.Schema {
		req.RowCount = true
	}
	res, err := dbmig.Verify(c.Request.Context(), h.Store, h.Secret, t, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	tags := h.Store.GetMigrationTags(t.ID)
	tags["migration_verify_result"] = res
	_ = h.Store.SetMigrationTags(t.ID, tags)
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *MigrationHandler) VerifyGet(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	tags := h.Store.GetMigrationTags(t.ID)
	c.JSON(http.StatusOK, gin.H{"data": tags["migration_verify_result"]})
}
