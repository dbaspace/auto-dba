package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/archive"
	"auto-dba/internal/store"
)

func (h *ArchiveHandler) engine() *archive.Engine {
	if h != nil && h.Engine != nil {
		return h.Engine
	}
	if h == nil || h.Store == nil {
		return nil
	}
	return archive.New(h.Store, h.Secret)
}

func (h *ArchiveHandler) Get(c *gin.Context) {
	t, err := h.Store.GetArchiveInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": decorateArchive(t)})
}

func (h *ArchiveHandler) Update(c *gin.Context) {
	t, err := h.Store.GetArchiveInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	var req store.ArchiveTask
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if req.Name != "" {
		t.Name = req.Name
	}
	if req.Condition != "" {
		t.Condition = req.Condition
		t.ScheduleExpr = strings.TrimSpace(req.ScheduleExpr)
		if rejectArchiveScript(c, t) {
			return
		}
	} else if req.ScheduleExpr != "" {
		t.ScheduleExpr = req.ScheduleExpr
	}
	if req.TableName != "" {
		t.TableName = req.TableName
	}
	if req.SourceDB != "" {
		t.SourceDB = req.SourceDB
	}
	if req.AssignedTo != "" {
		t.AssignedTo = req.AssignedTo
	}
	if err := h.Store.UpdateArchiveFields(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": decorateArchive(t)})
}

func (h *ArchiveHandler) Run(c *gin.Context) {
	if err := h.engine().Start(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ArchiveHandler) Pause(c *gin.Context) {
	if err := h.engine().Pause(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ArchiveHandler) Resume(c *gin.Context) {
	t, err := h.Store.GetArchiveInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	if t.Status != store.StatusPaused {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅已停用任务可启用"})
		return
	}
	next := store.StatusApproved
	if store.ArchiveRunMode(t) == "scheduled" {
		next = "scheduled"
	}
	if err := h.Store.UpdateArchiveState(t.ID, next, t.ApprovalStatus); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ArchiveHandler) StopRun(c *gin.Context) {
	ok := h.engine().Stop(c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": ok})
}

func (h *ArchiveHandler) ListRuns(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		taskID = c.Query("task_id")
	}
	list, err := h.Store.ListArchiveRunsInTenant(effectiveListTenantID(h.Store, c), taskID, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.ArchiveRun{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *ArchiveHandler) Meta(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"strategies": []gin.H{
			{"value": "delete_only", "label": "直接删除"},
			{"value": "backup_target", "label": "备份到目标实例"},
			{"value": "archive_oss", "label": "归档到 OSS"},
			{"value": "custom_script", "label": "自定义脚本"},
		},
		"run_modes": []gin.H{
			{"value": "immediate", "label": "立即执行"},
			{"value": "scheduled", "label": "定时任务"},
		},
		"pg_delete_modes": []gin.H{
			{"value": "auto", "label": "自动（按预估数据量）"},
			{"value": "cte", "label": "CTE"},
			{"value": "shard", "label": "Shard"},
		},
		"script_guide": gin.H{
			"sql":    "填写 DELETE/归档 SQL；由执行器分批执行。",
			"python": "定义 archive_delete() 并返回删除行数。提交时用 python3 检查语法，执行节点需要 python3。",
			"go":     "定义 archiveDelete() (int64, error)，或按参数生成。提交时会 go build，执行节点需要 Go。",
			"shell":  "当前不支持 Shell 脚本。",
		},
	}})
}

func (h *ArchiveHandler) GetSettings(c *gin.Context) {
	raw, _ := h.Store.GetGlobalArg(store.GlobalArgArchiveSettings)
	var m map[string]any
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &m)
	}
	if m == nil {
		m = map[string]any{
			"mail":    map[string]any{"host": "", "port": 25, "account": "", "sender": "", "timeout": 10},
			"webhook": map[string]any{"url": ""},
			"storage": map[string]any{"availableTb": 10},
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": m})
}

func (h *ArchiveHandler) PutSettings(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	b, _ := json.Marshal(raw)
	if err := h.Store.SetGlobalArg(store.GlobalArgArchiveSettings, string(b), "归档系统设置（邮件/Webhook/存储）"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": raw})
}

func (h *ArchiveHandler) TestSettings(c *gin.Context) {
	kind := strings.ToLower(c.Param("kind"))
	var body map[string]any
	_ = c.ShouldBindJSON(&body)
	raw, _ := h.Store.GetGlobalArg(store.GlobalArgArchiveSettings)
	saved := archive.ParseAlertSettings(raw)
	switch kind {
	case "mail":
		cfg := archive.MergeMail(saved.Mail, body)
		to := strings.TrimSpace(archiveField(body, "to"))
		if err := archive.SendMail(c.Request.Context(), cfg, to, "归档告警测试", "这是一封归档告警测试邮件。"); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if to == "" {
			to = cfg.Sender
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "测试邮件已发送到 " + to})
	case "webhook":
		cfg := archive.MergeWebhook(saved.Webhook, body)
		if err := archive.SendWebhook(c.Request.Context(), cfg, "归档告警测试"); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Webhook 测试已发送"})
	case "storage":
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "存储配额已记录"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "未知的测试类型"})
	}
}

func archiveField(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return strings.TrimSpace(s)
}

func decorateArchive(t *store.ArchiveTask) gin.H {
	if t == nil {
		return gin.H{}
	}
	extra := store.ParseArchiveExtra(t)
	return gin.H{
		"id": t.ID, "name": t.Name, "status": t.Status,
		"source_instance_id": t.SourceID, "source_db": t.SourceDB, "table_name": t.TableName,
		"condition": t.Condition, "issue_id": t.IssueID, "approval_status": t.ApprovalStatus,
		"assigned_to": t.AssignedTo, "schedule_expr": t.ScheduleExpr,
		"created_at": t.CreatedAt, "updated_at": t.UpdatedAt,
		"strategy": extra.Strategy, "run_mode": extra.RunMode,
		"target_instance_id": extra.TargetInstID, "target_db": extra.TargetDB, "target_table": extra.TargetTable,
		"pg_delete_mode": extra.PGDeleteMode, "script_type": extra.ScriptType,
	}
}

func (h *ArchiveHandler) Check(c *gin.Context) {
	var req store.ArchiveTask
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": err.Error()})
		return
	}
	cond := store.ArchiveConditionSQL(&req)
	if store.ArchiveStrategy(&req) == "custom_script" {
		extra := store.ParseArchiveExtra(&req)
		if err := archive.PrepareScript(extra.ScriptType, cond); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "校验通过"})
		return
	}
	if err := archive.ValidateDeleteCondition(cond); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "校验通过"})
}

func (h *ArchiveHandler) RenderScript(c *gin.Context) {
	var req struct {
		ScriptType   string         `json:"script_type"`
		ScriptParams map[string]any `json:"script_params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.ToLower(strings.TrimSpace(req.ScriptType)) != "go" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 Go 脚本可按参数生成"})
		return
	}
	src, err := archive.RenderGoScript(archive.ScriptParamsFromMap(req.ScriptParams))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"script_content": src}})
}

func (h *ArchiveHandler) Monitor(c *gin.Context) {
	if err := archive.SeedAlertRules(h.Store); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	archive.EvaluateAlerts(c.Request.Context(), h.Store)
	rules, err := h.Store.ListArchiveAlertRules()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	events, err := h.Store.ListArchiveAlertEvents(50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	open := map[string]bool{}
	for _, ev := range events {
		if !ev.Acked {
			open[ev.RuleID] = true
		}
	}
	for i := range rules {
		if open[rules[i].ID] {
			rules[i].Status = "triggered"
		} else if rules[i].Status == "" {
			rules[i].Status = "normal"
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"rules": rules, "alerts": events}})
}

func (h *ArchiveHandler) CreateAlertRule(c *gin.Context) {
	var req struct {
		RuleType  string  `json:"rule_type"`
		Title     string  `json:"title"`
		Threshold float64 `json:"threshold"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	kind := archive.NormalizeAlertRuleType(req.RuleType)
	if kind == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择规则类型"})
		return
	}
	exist, err := h.Store.GetArchiveAlertRuleByType(kind)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if exist != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "该规则类型已存在，请编辑已有规则"})
		return
	}
	th := req.Threshold
	if th <= 0 {
		th = archive.DefaultAlertThreshold(kind)
	}
	rule := &store.ArchiveAlertRule{
		RuleType: kind, Title: archive.AlertRuleTitle(kind, th, req.Title),
		Threshold: th, Severity: archive.DefaultAlertSeverity(kind), Enabled: true,
	}
	if err := h.Store.InsertArchiveAlertRule(rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rule})
}

func (h *ArchiveHandler) UpdateAlertRule(c *gin.Context) {
	rules, err := h.Store.ListArchiveAlertRules()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	var cur *store.ArchiveAlertRule
	for i := range rules {
		if rules[i].ID == c.Param("id") {
			cur = &rules[i]
			break
		}
	}
	if cur == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "规则不存在"})
		return
	}
	var req struct {
		Title     string  `json:"title"`
		Threshold float64 `json:"threshold"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Title) != "" {
		cur.Title = strings.TrimSpace(req.Title)
	}
	if req.Threshold > 0 {
		cur.Threshold = req.Threshold
	}
	cur.Enabled = true
	if err := h.Store.UpdateArchiveAlertRule(cur); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cur})
}

func (h *ArchiveHandler) DeleteAlertRule(c *gin.Context) {
	if err := h.Store.DeleteArchiveAlertRule(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ArchiveHandler) AckAlert(c *gin.Context) {
	if err := h.Store.AckArchiveAlertEvent(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func rejectArchiveScript(c *gin.Context, t *store.ArchiveTask) bool {
	if store.ArchiveStrategy(t) != "custom_script" {
		return false
	}
	extra := store.ParseArchiveExtra(t)
	if strings.TrimSpace(extra.ScriptType) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择脚本类型"})
		return true
	}
	if err := archive.PrepareScript(extra.ScriptType, store.ArchiveConditionSQL(t)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return true
	}
	return false
}
