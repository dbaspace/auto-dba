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
	t, err := h.Store.GetArchive(c.Param("id"))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": decorateArchive(t)})
}

func (h *ArchiveHandler) Update(c *gin.Context) {
	t, err := h.Store.GetArchive(c.Param("id"))
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
	}
	if req.TableName != "" {
		t.TableName = req.TableName
	}
	if req.SourceDB != "" {
		t.SourceDB = req.SourceDB
	}
	if req.ScheduleExpr != "" {
		t.ScheduleExpr = req.ScheduleExpr
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

func (h *ArchiveHandler) StopRun(c *gin.Context) {
	ok := h.engine().Stop(c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": ok})
}

func (h *ArchiveHandler) ListRuns(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		taskID = c.Query("task_id")
	}
	list, err := h.Store.ListArchiveRuns(taskID, 100)
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
			"python": "当前独立部署仅支持 SQL 脚本。",
			"go":     "当前独立部署仅支持 SQL 脚本。",
			"shell":  "当前独立部署仅支持 SQL 脚本。",
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
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已记录测试请求（" + kind + "），独立部署未连接真实 SMTP/Webhook"})
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
		if strings.TrimSpace(cond) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": "脚本内容不能为空"})
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
