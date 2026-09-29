package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

const cdcReinitForbidden = "仅超级管理员、平台管理员或全局审核白名单用户可重新初始化 CDC 任务"

func (h *CDCHandler) canReinitUser(c *gin.Context) bool {
	if middleware.IsAdminRole(middleware.Role(c)) {
		return true
	}
	return h.Store.IsGlobalApprover(middleware.Username(c))
}

func (h *CDCHandler) requireReinitUser(c *gin.Context) bool {
	if h.canReinitUser(c) {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"message": cdcReinitForbidden})
	return false
}

// bindReinitRequest 空请求体合法；connector_id 兼容数字与字符串。
func bindReinitRequest(c *gin.Context) (cdc.ReinitRequest, map[string]any, error) {
	var raw map[string]any
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&raw); err != nil {
			return cdc.ReinitRequest{}, nil, fmt.Errorf("请求体无效")
		}
	}
	req := cdc.ReinitRequest{
		SnapshotMode: jsonAnyString(raw["snapshot_mode"]),
		ConnectorID:  jsonAnyString(raw["connector_id"]),
	}
	if req.ConnectorID == "0" {
		req.ConnectorID = ""
	}
	if v, ok := raw["resume_routine_loads"]; ok && v != nil {
		b := jsonAnyBool(v)
		req.ResumeRoutineLoads = &b
	}
	op, ip := cdcRequestActor(c)
	req.Operator, req.OperatorIP = op, ip
	return req, raw, nil
}

func (h *CDCHandler) loadReinitTask(c *gin.Context) *store.CDCTask {
	t, err := h.Store.GetCDC(c.Param("id"))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return nil
	}
	return t
}

func (h *CDCHandler) ReinitPreview(c *gin.Context) {
	if !h.requireReinitUser(c) {
		return
	}
	t := h.loadReinitTask(c)
	if t == nil {
		return
	}
	req, _, err := bindReinitRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	o := h.orch()
	if err := o.ReinitGate(ctx, t, req.ConnectorID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	res, err := o.ReinitPreview(ctx, t.ID, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *CDCHandler) Reinit(c *gin.Context) {
	if !h.requireReinitUser(c) {
		return
	}
	t := h.loadReinitTask(c)
	if t == nil {
		return
	}
	req, _, err := bindReinitRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if _, err := cdc.ResolveReinitSnapshotMode(req.SnapshotMode, ""); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	o := h.orch()
	if err := o.ReinitGate(ctx, t, req.ConnectorID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := o.StartReinit(t.ID, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	h.logReinitSubmit(t.ID, req, "")
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"task_id": t.ID, "task_name": t.Name, "accepted": true, "status": store.StatusReinitializing,
	}})
}

// retryReinit 上次重新初始化失败（error_step=reinit）后的「重试」：默认参数异步重做，不校验 Connector 缺失。
func (h *CDCHandler) retryReinit(c *gin.Context, t *store.CDCTask) {
	if !h.requireReinitUser(c) {
		return
	}
	if ok, msg := cdc.ReinitEligibleStatus(t); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": msg})
		return
	}
	op, ip := cdcRequestActor(c)
	req := cdc.ReinitRequest{Operator: op, OperatorIP: ip}
	if err := h.orch().StartReinit(t.ID, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	h.logReinitSubmit(t.ID, req, "重试上次失败的重新初始化")
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{
		"task_id": t.ID, "task_name": t.Name, "accepted": true, "status": store.StatusReinitializing,
	}})
}

func (h *CDCHandler) logReinitSubmit(taskID string, req cdc.ReinitRequest, via string) {
	snap := req.SnapshotMode
	if snap == "" {
		snap = "沿用原配置"
	}
	msg := fmt.Sprintf("用户 %s 发起重新初始化（snapshot_mode=%s，恢复 Routine Load=%v", firstNonEmpty(req.Operator, "unknown"), snap, req.ResumeRoutineLoads == nil || *req.ResumeRoutineLoads)
	if req.ConnectorID != "" {
		msg += "，换绑 connector_id=" + req.ConnectorID
	}
	if via != "" {
		msg += "，" + via
	}
	msg += "）"
	_ = h.Store.InsertCDCLog(&store.CDCSyncLog{TaskID: taskID, Level: "INFO", Step: "manual", Message: msg, Operator: req.Operator})
}

type reinitCandidate struct {
	TaskID          string   `json:"task_id"`
	TaskName        string   `json:"task_name"`
	Env             string   `json:"env"`
	Status          string   `json:"status"`
	ConnectorName   string   `json:"connector_name"`
	ConnectorNames  []string `json:"connector_names"`
	SourceDBName    string   `json:"source_db_name"`
	ConnectorExists bool     `json:"connector_exists"`
	CanReinit       bool     `json:"can_reinit"`
	Message         string   `json:"message,omitempty"`
}

type reinitCandidates struct {
	ConnectorID      string            `json:"connector_id"`
	ConnectBaseURL   string            `json:"connect_base_url"`
	ConnectReachable bool              `json:"connect_reachable"`
	Message          string            `json:"message"`
	List             []reinitCandidate `json:"list"`
	tasks            map[string]*store.CDCTask
}

func (h *CDCHandler) collectReinitCandidates(ctx context.Context, connectorID string) (*reinitCandidates, error) {
	conn, err := h.Store.GetConnector(connectorID)
	if err != nil || conn == nil {
		return nil, fmt.Errorf("Debezium 服务不存在：%s", connectorID)
	}
	out := &reinitCandidates{
		ConnectorID:    conn.ID,
		ConnectBaseURL: cdc.NormalizeConnectBaseURL(conn.DBZHost),
		List:           []reinitCandidate{},
		tasks:          map[string]*store.CDCTask{},
	}
	var existing map[string]bool
	if out.ConnectBaseURL == "" {
		out.Message = "该 Debezium 服务未配置 dbz_host"
	} else {
		cdc.InvalidateConnectorList(out.ConnectBaseURL)
		if existing, err = cdc.ListConnectConnectors(ctx, out.ConnectBaseURL); err != nil {
			out.Message = "Debezium Connect 不可达：" + err.Error()
		} else {
			out.ConnectReachable = true
		}
	}
	list, err := h.Store.ListCDC("", "init")
	if err != nil {
		return nil, err
	}
	o := h.orch()
	for i := range list {
		t := &list[i]
		if strings.TrimSpace(t.ParentID) != "" {
			continue
		}
		extra := cdc.ParseTaskExtra(t.Description)
		if strings.TrimSpace(extra["connector_id"]) != conn.ID {
			continue
		}
		if extra["deleted"] == "true" || t.Status == store.StatusDeleted {
			continue
		}
		names := o.TaskConnectorNames(t)
		row := reinitCandidate{
			TaskID: t.ID, TaskName: t.Name, Env: extra["env"], Status: t.Status,
			ConnectorName: extra["connector_name"], ConnectorNames: names, SourceDBName: t.SourceDB,
		}
		if out.ConnectReachable && len(names) > 0 {
			row.ConnectorExists = true
			for _, n := range names {
				if !existing[n] {
					row.ConnectorExists = false
					break
				}
			}
		}
		eligible, msg := cdc.ReinitEligibleStatus(t)
		retry := extra["error_step"] == "reinit"
		switch {
		case !eligible:
			row.Message = msg
		case retry:
			row.CanReinit = true
		case !out.ConnectReachable:
			row.Message = "Connect 不可达，无法确认 Connector 缺失"
		case row.ConnectorExists:
			row.Message = "Connector 仍存在，无需重新初始化"
		default:
			row.CanReinit = true
		}
		out.List = append(out.List, row)
		out.tasks[t.ID] = t
	}
	return out, nil
}

func (h *CDCHandler) ReinitCandidates(c *gin.Context) {
	id := strings.TrimSpace(c.Query("connector_id"))
	if id == "" || id == "0" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "connector_id 必填"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	out, err := h.collectReinitCandidates(ctx, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *CDCHandler) ReinitBatch(c *gin.Context) {
	if !h.requireReinitUser(c) {
		return
	}
	req, raw, err := bindReinitRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	connectorID := req.ConnectorID
	req.ConnectorID = ""
	if connectorID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "connector_id 必填"})
		return
	}
	if _, err := cdc.ResolveReinitSnapshotMode(req.SnapshotMode, ""); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	var taskIDs []string
	if arr, ok := raw["task_ids"].([]any); ok {
		for _, v := range arr {
			taskIDs = append(taskIDs, jsonAnyString(v))
		}
	}
	taskIDs = uniqueNonEmpty(taskIDs)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	cands, err := h.collectReinitCandidates(ctx, connectorID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	byID := map[string]reinitCandidate{}
	for _, row := range cands.List {
		byID[row.TaskID] = row
	}
	if len(taskIDs) == 0 {
		if !cands.ConnectReachable {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Debezium Connect 不可达，请指定 task_ids：" + cands.Message})
			return
		}
		for _, row := range cands.List {
			if row.CanReinit && !row.ConnectorExists {
				taskIDs = append(taskIDs, row.TaskID)
			}
		}
	}
	results := make([]cdc.ReinitBatchResult, 0, len(taskIDs))
	var accept []string
	for _, id := range taskIDs {
		row, ok := byID[id]
		switch {
		case !ok:
			status := ""
			if t, _ := h.Store.GetCDC(id); t != nil {
				status = t.Status
			}
			results = append(results, cdc.ReinitBatchResult{TaskID: id, Status: status, Message: "任务不属于该 Debezium 服务或不是 init 主任务"})
		case !row.CanReinit:
			results = append(results, cdc.ReinitBatchResult{TaskID: id, TaskName: row.TaskName, Status: row.Status, Message: row.Message})
		default:
			accept = append(accept, id)
		}
	}
	o := h.orch()
	for _, r := range o.StartReinitBatch(accept, req) {
		if r.Accepted {
			h.logReinitSubmit(r.TaskID, req, "按 Debezium 服务批量 connector_id="+connectorID)
		} else if row, ok := byID[r.TaskID]; ok {
			r.TaskName, r.Status = row.TaskName, row.Status
		}
		results = append(results, r)
	}
	accepted := 0
	for _, r := range results {
		if r.Accepted {
			accepted++
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"accepted": accepted, "rejected": len(results) - accepted, "list": results,
	}})
}
