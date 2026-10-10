package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/dbprobe"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type MigrationHandler struct {
	Store  *store.DB
	Cfg    *config.Config
	Secret string
	Engine *dbmig.Engine
}

func (h *MigrationHandler) engine() *dbmig.Engine {
	if h.Engine == nil {
		h.Engine = dbmig.New(h.Store, h.Secret)
	}
	return h.Engine
}

type migCreateReq struct {
	Name             string                `json:"name"`
	Description      string                `json:"description"`
	DBType           string                `json:"db_type"`
	Env              string                `json:"env"`
	SourceID         string                `json:"source_instance_id"`
	TargetID         string                `json:"target_instance_id"`
	SourceDB         string                `json:"source_db"`
	TargetDB         string                `json:"target_db"`
	Tables           []string              `json:"tables"`
	Objects          []dbmig.ObjectSel     `json:"objects"`
	MigrationMode    string                `json:"migration_mode"`
	SchemaStrategy   string                `json:"schema_strategy"`
	ConflictStrategy string                `json:"conflict_strategy"`
	TransferMode     string                `json:"transfer_mode"`
	StartMode        string                `json:"start_mode"`
	ScheduledAt      string                `json:"scheduled_at"`
	AssignedApprover string                `json:"assigned_approver"`
	Approver         string                `json:"approver"`
	Reviewer         string                `json:"reviewer"`
	AssignedTo       string                `json:"assigned_to"`
	BatchSize        int                   `json:"batch_size"`
	SpeedLimit       int                   `json:"speed_limit"`
	WholeDBs         []string              `json:"whole_dbs"`
	WholeDBTargets   []dbmig.WholeDBTarget `json:"whole_db_targets"`
	ExcludeTables    []string              `json:"exclude_tables"`
	SampleDBs        []string              `json:"sample_dbs"`
}

func (h *MigrationHandler) List(c *gin.Context) {
	list, err := h.Store.ListMigrationsInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.MigrationTask{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *MigrationHandler) Get(c *gin.Context) {
	t, err := h.Store.GetMigrationInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	h.decorateTask(t)
	c.JSON(http.StatusOK, gin.H{"data": t})
}

// fillEndpoints 创建路径：源/目标实例须属于当前租户（或已授权）。
func (h *MigrationHandler) fillEndpoints(c *gin.Context, t *store.MigrationTask) error {
	if src, err := resolveInstanceInTenant(h.Store, c, t.SourceID); err != nil || src == nil {
		return errOr("源实例不存在")
	}
	if tgt, err := resolveInstanceInTenant(h.Store, c, t.TargetID); err != nil || tgt == nil {
		return errOr("目标实例不存在")
	}
	return h.fillEndpointsRaw(t)
}

// fillEndpointsRaw 仅补全主机端口（调用方已校验任务归属）。
func (h *MigrationHandler) fillEndpointsRaw(t *store.MigrationTask) error {
	src, err := h.Store.GetInstance(t.SourceID)
	if err != nil || src == nil {
		return errOr("源实例不存在")
	}
	tgt, err := h.Store.GetInstance(t.TargetID)
	if err != nil || tgt == nil {
		return errOr("目标实例不存在")
	}
	t.SourceHost = strings.TrimSpace(src.Host)
	if t.SourceHost == "" {
		t.SourceHost = src.DomainName
	}
	t.SourcePort = src.Port
	t.TargetHost = strings.TrimSpace(tgt.Host)
	if t.TargetHost == "" {
		t.TargetHost = tgt.DomainName
	}
	t.TargetPort = tgt.Port
	if t.DBType == "" {
		t.DBType = store.NormalizeDBType(src.DBType)
	}
	return nil
}

func errOr(msg string) error {
	return &plainError{msg}
}

type plainError struct{ s string }

func (e *plainError) Error() string { return e.s }

func (h *MigrationHandler) Create(c *gin.Context) {
	var req migCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	if len(req.Objects) > 0 {
		if strings.TrimSpace(req.SourceDB) == "" {
			req.SourceDB = strings.TrimSpace(req.Objects[0].SourceDB)
		}
		if strings.TrimSpace(req.TargetDB) == "" {
			req.TargetDB = strings.TrimSpace(req.Objects[0].TargetDB)
		}
	}
	if strings.TrimSpace(req.SourceDB) == "" && len(req.WholeDBs) > 0 {
		req.SourceDB = strings.TrimSpace(req.WholeDBs[0])
	}
	if strings.TrimSpace(req.TargetDB) == "" && len(req.WholeDBTargets) > 0 {
		req.TargetDB = strings.TrimSpace(req.WholeDBTargets[0].TargetDB)
		if req.TargetDB == "" {
			req.TargetDB = strings.TrimSpace(req.WholeDBTargets[0].SourceDB)
		}
	}
	if strings.TrimSpace(req.TargetDB) == "" && len(req.WholeDBs) > 0 {
		req.TargetDB = strings.TrimSpace(req.WholeDBs[0])
	}
	if strings.TrimSpace(req.Name) == "" || req.SourceID == "" || req.TargetID == "" || (req.SourceDB == "" && len(req.WholeDBs) == 0 && len(req.Objects) == 0) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "任务名称、源/目标实例、源库必填"})
		return
	}
	t := &store.MigrationTask{
		Name: req.Name, Description: req.Description, DBType: store.NormalizeDBType(req.DBType), Env: req.Env,
		SourceID: req.SourceID, TargetID: req.TargetID, SourceDB: req.SourceDB, TargetDB: req.TargetDB,
		MigrationMode: req.MigrationMode, SchemaStrategy: req.SchemaStrategy, ConflictStrategy: req.ConflictStrategy,
		TransferMode: req.TransferMode, StartMode: req.StartMode, ScheduledAt: req.ScheduledAt,
		AssignedApprover: req.AssignedApprover, BatchSize: req.BatchSize, SpeedLimit: req.SpeedLimit,
		CreatedBy: middleware.Username(c),
	}
	if t.TargetDB == "" {
		t.TargetDB = t.SourceDB
	}
	perf := h.Store.GetMigrationSettings().Performance
	if t.BatchSize <= 0 {
		t.BatchSize = perf.BatchSize
	}
	if t.SpeedLimit <= 0 {
		t.SpeedLimit = perf.DefaultSpeedLimit
	}
	if t.StartMode == "scheduled" {
		t.ScheduledAt = normalizeScheduledAt(t.ScheduledAt)
		if t.ScheduledAt == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "定时启动请填写启动时间"})
			return
		}
	} else {
		t.ScheduledAt = ""
	}
	if err := h.fillEndpoints(c, t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	preferred := strings.TrimSpace(req.AssignedApprover)
	if preferred == "" {
		preferred = strings.TrimSpace(req.Reviewer)
	}
	if preferred == "" {
		preferred = strings.TrimSpace(req.Approver)
	}
	if preferred == "" {
		preferred = strings.TrimSpace(req.AssignedTo)
	}
	if preferred == "" {
		def, _ := h.Store.GetGlobalArg("migration_approver_user_ids")
		if def == "" {
			def, _ = h.Store.GetGlobalArg("sync_default_approver")
		}
		preferred = strings.TrimSpace(def)
		if i := strings.IndexAny(preferred, ",，"); i >= 0 {
			preferred = strings.TrimSpace(preferred[:i])
		}
	}
	reviewer, aerr := resolveTenantAssignee(h.Store, middleware.TenantID(c), preferred)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	th := &TicketHandler{Store: h.Store}
	if approvers := th.resolveApprovers(t.SourceID, t.SourceDB); len(approvers) > 0 && !usernameInList(reviewer, approvers) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "审核人不在实例/库审批人名单中"})
		return
	}
	t.AssignedApprover = reviewer
	t.Status = store.StatusPending
	t.ApprovalStatus = store.StatusPending
	if err := h.Store.CreateMigration(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	objs := dbmig.BuildObjectsFromSels(t.DBType, req.Objects)
	if len(objs) == 0 {
		objs = dbmig.BuildObjects(t.DBType, t.SourceDB, t.TargetDB, req.Tables)
	}
	if len(req.WholeDBs) > 0 {
		expandCtx, expandCancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
		expanded, err := dbmig.ExpandWholeDBs(expandCtx, h.Store, h.Secret, t.DBType, t.SourceID, req.WholeDBs, req.WholeDBTargets, req.ExcludeTables, objs)
		expandCancel()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "展开整库对象失败: " + err.Error()})
			_ = h.Store.DeleteMigration(t.ID)
			return
		}
		objs = expanded
	}
	if len(objs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请至少选择一张表或整个库"})
		_ = h.Store.DeleteMigration(t.ID)
		return
	}
	if strings.TrimSpace(t.SourceDB) == "" && len(objs) > 0 {
		t.SourceDB = objs[0].SourceDB
	}
	if strings.TrimSpace(t.TargetDB) == "" && len(objs) > 0 {
		t.TargetDB = objs[0].TargetDB
	}
	if err := h.Store.ReplaceMigrationObjects(t.ID, objs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	t.TotalObjects = len(objs)
	_ = h.Store.UpdateMigration(t)
	dbs := map[string]struct{}{}
	var estBytes int64
	for _, o := range objs {
		if o.SourceDB != "" {
			dbs[o.SourceDB] = struct{}{}
		}
		estBytes += o.TotalBytes
	}
	_ = h.Store.SetMigrationTags(t.ID, map[string]any{
		"selection": map[string]any{
			"db_count":              len(dbs),
			"table_count":           len(objs),
			"estimated_total_bytes": estBytes,
		},
	})
	_ = h.Store.AddMigrationLog(t.ID, "INFO", "任务已创建")

	pctx, pcancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	items := dbmig.Precheck(pctx, h.Store, h.Secret, t)
	pcancel()
	_ = h.Store.ReplaceMigrationPrechecks(t.ID, items)
	if !dbmig.PrecheckPassed(items) {
		t.Status = store.StatusPreCheckFailed
		t.ErrorMessage = "预检未通过"
		_ = h.Store.UpdateMigration(t)
		_ = h.Store.AddMigrationLog(t.ID, "ERROR", "创建后预检未通过")
		saved, _ := h.Store.GetMigrationInTenant(t.ID, effectiveListTenantID(h.Store, c))
		c.JSON(http.StatusOK, gin.H{"data": saved, "ok": false, "items": items, "message": "预检未通过"})
		return
	}

	issue := &store.Issue{
		IssueType:   store.IssueTypeMigration,
		Type:        store.IssueTypeMigration,
		InstanceID:  t.SourceID,
		DBName:      t.SourceDB,
		SQLContent:  "migration " + t.ID + " " + t.Name,
		Description: t.Description,
		Submitter:   middleware.Username(c),
		AssignedTo:  reviewer,
	}
	stampTenantID(c, func(tid string) {
		if issue != nil { issue.TenantID = tid }
	})
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	t.IssueID = issue.ID
	_ = h.Store.UpdateMigration(t)
	_ = h.Store.AddMigrationLog(t.ID, "INFO", "已提交审批，通过后自动执行")
	saved, _ := h.Store.GetMigrationInTenant(t.ID, effectiveListTenantID(h.Store, c))
	c.JSON(http.StatusOK, gin.H{"data": saved, "ok": true, "items": items})
}

func (h *MigrationHandler) loadTask(c *gin.Context) *store.MigrationTask {
	t, err := h.Store.GetMigrationInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return nil
	}
	return t
}

func (h *MigrationHandler) Precheck(c *gin.Context) {
	var req migCreateReq
	_ = c.ShouldBindJSON(&req)
	t := &store.MigrationTask{
		SourceID: req.SourceID, TargetID: req.TargetID, SourceDB: req.SourceDB, TargetDB: req.TargetDB,
		DBType: req.DBType, TransferMode: req.TransferMode, ConflictStrategy: req.ConflictStrategy,
		SchemaStrategy: req.SchemaStrategy, MigrationMode: req.MigrationMode,
	}
	if id := c.Param("id"); id != "" && id != "precheck" {
		if exist, _ := h.Store.GetMigrationInTenant(id, effectiveListTenantID(h.Store, c)); exist != nil {
			t = exist
		}
	}
	if t.SourceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "source_instance_id 必填"})
		return
	}
	if t.DBType == "" {
		if inst, _ := resolveInstanceInTenant(h.Store, c, t.SourceID); inst != nil {
			t.DBType = inst.DBType
		}
	}
	// 预检：带上请求中的表对象（含 schema），供 source_tables / schema_compatibility 使用
	if len(t.Objects) == 0 {
		objs := dbmig.BuildObjectsFromSels(t.DBType, req.Objects)
		if len(objs) == 0 {
			samples := req.SampleDBs
			if len(samples) == 0 {
				samples = req.WholeDBs
			}
			for i, db := range samples {
				if i >= 3 {
					break
				}
				db = strings.TrimSpace(db)
				if db == "" {
					continue
				}
				objs = append(objs, store.MigrationObject{SourceDB: db, TargetDB: db})
			}
		}
		t.Objects = objs
		if t.SourceDB == "" && len(t.Objects) > 0 {
			t.SourceDB = t.Objects[0].SourceDB
		}
		if t.TargetDB == "" && len(t.Objects) > 0 {
			t.TargetDB = firstNonEmptyMig(t.Objects[0].TargetDB, t.Objects[0].SourceDB)
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	items := dbmig.Precheck(ctx, h.Store, h.Secret, t)
	ok := dbmig.PrecheckPassed(items)
	if t.ID != "" {
		_ = h.Store.ReplaceMigrationPrechecks(t.ID, items)
	}
	c.JSON(http.StatusOK, gin.H{"ok": ok, "items": items})
}

func firstNonEmptyMig(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (h *MigrationHandler) Start(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	if !store.MigrationApproved(t) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "迁移任务须审批通过后才能执行"})
		return
	}
	if err := h.engine().Start(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) Pause(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	if t.Status != store.StatusRunning && t.Status != store.StatusPreChecking {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅运行中任务可暂停"})
		return
	}
	if err := h.engine().Pause(t.ID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AddMigrationLog(t.ID, "WARN", "用户暂停任务")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) Resume(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	if t.Status != store.StatusPaused {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅已暂停任务可恢复"})
		return
	}
	if err := h.engine().Resume(t.ID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AddMigrationLog(t.ID, "INFO", "用户恢复任务")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) Schedule(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	var req struct {
		ScheduledAt string `json:"scheduled_at"`
	}
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.ScheduledAt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "scheduled_at 必填"})
		return
	}
	t.StartMode = "scheduled"
	t.ScheduledAt = strings.TrimSpace(req.ScheduledAt)
	t.Status = "scheduled"
	if err := h.Store.UpdateMigration(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AddMigrationLog(t.ID, "INFO", "已设置定时 "+t.ScheduledAt)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": t})
}

func (h *MigrationHandler) CancelSchedule(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	t.StartMode = "immediate"
	t.ScheduledAt = ""
	if t.Status == "scheduled" {
		t.Status = store.StatusPending
	}
	if err := h.Store.UpdateMigration(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AddMigrationLog(t.ID, "INFO", "已取消定时")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) Terminate(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	h.engine().Cancel(t.ID)
	t.Status = store.StatusCanceled
	t.ErrorMessage = "已强制终止"
	t.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = h.Store.UpdateMigration(t)
	_ = h.Store.AddMigrationLog(t.ID, "WARN", "用户强制终止任务")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) Cancel(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	h.engine().Cancel(t.ID)
	t.Status = store.StatusCanceled
	t.ErrorMessage = "已取消"
	t.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = h.Store.UpdateMigration(t)
	_ = h.Store.AddMigrationLog(t.ID, "WARN", "用户取消任务")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) Retry(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	if t.Status != store.StatusFailed && t.Status != store.StatusPreCheckFailed && t.Status != store.StatusCanceled {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅失败/取消任务可重试"})
		return
	}
	if !store.MigrationApproved(t) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "迁移任务须审批通过后才能执行"})
		return
	}
	if err := h.engine().Start(t.ID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) Delete(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	if t.Status == store.StatusRunning || t.Status == store.StatusPreChecking {
		c.JSON(http.StatusBadRequest, gin.H{"message": "运行中不能删除"})
		return
	}
	if err := h.Store.DeleteMigration(t.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) SubmitApproval(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	if t.CreatedBy != middleware.Username(c) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅创建人可提交审批"})
		return
	}
	items, _ := h.Store.ListMigrationPrechecks(t.ID)
	if !dbmig.PrecheckPassed(items) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先预检并通过"})
		return
	}
	t.ApprovalStatus = "pending"
	_ = h.Store.UpdateMigration(t)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) Approve(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	user := middleware.Username(c)
	if user == t.CreatedBy {
		c.JSON(http.StatusForbidden, gin.H{"message": "创建人不能审批自己的任务"})
		return
	}
	priv := middleware.IsPrivileged(middleware.Role(c))
	okAssignee := t.AssignedApprover == "" || strings.EqualFold(t.AssignedApprover, user) ||
		(t.AssignedApproverLogin != "" && strings.EqualFold(t.AssignedApproverLogin, user))
	if !okAssignee && !priv && !h.Store.IsGlobalApprover(user) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅指定审核人、超级审核白名单或管理员可审批"})
		return
	}
	var req struct {
		Comment string `json:"comment"`
	}
	_ = c.ShouldBindJSON(&req)
	t.ApprovalStatus = "approved"
	t.ApprovedBy = user
	t.ApprovalComment = req.Comment
	_ = h.Store.UpdateMigration(t)
	_ = h.Store.AddMigrationLog(t.ID, "INFO", "审批通过: "+user)
	if t.StartMode != "scheduled" {
		_ = h.engine().Start(t.ID)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) Reject(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	user := middleware.Username(c)
	priv := middleware.IsPrivileged(middleware.Role(c))
	okAssignee := t.AssignedApprover == "" || strings.EqualFold(t.AssignedApprover, user) ||
		(t.AssignedApproverLogin != "" && strings.EqualFold(t.AssignedApproverLogin, user))
	if !okAssignee && !priv && !h.Store.IsGlobalApprover(user) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅指定审核人、超级审核白名单或管理员可驳回"})
		return
	}
	var req struct {
		Comment string `json:"comment"`
	}
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.Comment) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "驳回原因必填"})
		return
	}
	t.ApprovalStatus = "rejected"
	t.Status = store.StatusCanceled
	t.ApprovalComment = req.Comment
	t.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = h.Store.UpdateMigration(t)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) Logs(c *gin.Context) {
	since, _ := strconv.ParseInt(c.Query("since_id"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Store.ListMigrationLogs(c.Param("id"), since, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *MigrationHandler) Objects(c *gin.Context) {
	list, err := h.Store.ListMigrationObjects(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	want := strings.TrimSpace(c.Query("status"))
	if want != "" {
		filtered := list[:0]
		for _, o := range list {
			if o.Status == want {
				filtered = append(filtered, o)
			}
		}
		list = filtered
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *MigrationHandler) Prechecks(c *gin.Context) {
	list, err := h.Store.ListMigrationPrechecks(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *MigrationHandler) Progress(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.buildProgress(t)})
}

func (h *MigrationHandler) Connect(c *gin.Context) {
	inst, err := resolveInstanceInTenant(h.Store, c, c.Param("id"))
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	db, err := dbmig.OpenInstance(ctx, inst, h.Secret, "")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"connected": false, "error_message": err.Error()})
		return
	}
	db.Close()
	c.JSON(http.StatusOK, gin.H{"connected": true, "host": inst.Host, "port": inst.Port, "username": inst.Username})
}

func (h *MigrationHandler) Databases(c *gin.Context) {
	inst, err := resolveInstanceInTenant(h.Store, c, c.Param("id"))
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	plain, decErr := secret.Decrypt(h.Secret, inst.Password)
	if decErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "解密实例密码失败"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	list, err := dbprobe.List(ctx, inst, plain)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *MigrationHandler) Tables(c *gin.Context) {
	inst, err := resolveInstanceInTenant(h.Store, c, c.Param("id"))
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	plain, decErr := secret.Decrypt(h.Secret, inst.Password)
	if decErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "解密实例密码失败"})
		return
	}
	namesOnly := c.Query("names_only") == "1" || c.Query("names_only") == "true"
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	if namesOnly {
		list, err := dbprobe.ListTables(ctx, inst, plain, c.Param("db"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		out := make([]gin.H, 0, len(list))
		for _, n := range list {
			out = append(out, gin.H{"table_name": n, "name": n, "total_bytes": 0})
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
		return
	}
	list, err := dbprobe.ListTablesMeta(ctx, inst, plain, c.Param("db"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *MigrationHandler) BatchTables(c *gin.Context) {
	inst, err := resolveInstanceInTenant(h.Store, c, c.Param("id"))
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	plain, decErr := secret.Decrypt(h.Secret, inst.Password)
	if decErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "解密实例密码失败"})
		return
	}
	var req struct {
		Databases []string `json:"databases"`
		NamesOnly bool     `json:"names_only"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Databases) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "databases 必填"})
		return
	}
	if len(req.Databases) > 50 {
		req.Databases = req.Databases[:50]
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	type one struct {
		DB     string `json:"db"`
		Tables any    `json:"tables"`
		Error  string `json:"error,omitempty"`
	}
	out := make([]one, len(req.Databases))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i, dbName := range req.Databases {
		wg.Add(1)
		go func(i int, dbName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			dbName = strings.TrimSpace(dbName)
			if dbName == "" {
				out[i] = one{DB: dbName, Error: "空库名"}
				return
			}
			if req.NamesOnly {
				list, err := dbprobe.ListTables(ctx, inst, plain, dbName)
				if err != nil {
					out[i] = one{DB: dbName, Error: err.Error()}
					return
				}
				rows := make([]gin.H, 0, len(list))
				for _, n := range list {
					rows = append(rows, gin.H{"table_name": n, "name": n, "total_bytes": 0})
				}
				out[i] = one{DB: dbName, Tables: rows}
				return
			}
			list, err := dbprobe.ListTablesMeta(ctx, inst, plain, dbName)
			if err != nil {
				out[i] = one{DB: dbName, Error: err.Error()}
				return
			}
			out[i] = one{DB: dbName, Tables: list}
		}(i, dbName)
	}
	wg.Wait()
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *MigrationHandler) Action(status string) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch status {
		case "running", "approved":
			h.Start(c)
		default:
			_ = h.Store.UpdateMigrationStatus(c.Param("id"), status)
			c.JSON(http.StatusOK, gin.H{"ok": true})
		}
	}
}

func (h *MigrationHandler) Update(c *gin.Context) {
	t := h.loadTask(c)
	if t == nil {
		return
	}
	var req struct {
		AssignedApprover      string `json:"assigned_approver"`
		AssignedApproverLogin string `json:"assigned_approver_login"`
		Description           string `json:"description"`
		Name                  string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	newApprover := strings.TrimSpace(req.AssignedApprover)
	if newApprover == "" {
		newApprover = strings.TrimSpace(req.AssignedApproverLogin)
	}
	user := middleware.Username(c)
	approval := strings.ToLower(strings.TrimSpace(t.ApprovalStatus))
	if newApprover != "" {
		switch approval {
		case "pending":
			if !strings.EqualFold(t.CreatedBy, user) {
				c.JSON(http.StatusForbidden, gin.H{"message": "待审批时仅创建人可转移审核人"})
				return
			}
			cur := strings.TrimSpace(t.AssignedApprover)
			if cur == "" {
				cur = strings.TrimSpace(t.AssignedApproverLogin)
			}
			if strings.EqualFold(cur, newApprover) {
				c.JSON(http.StatusBadRequest, gin.H{"message": "新审核人与当前审核人相同"})
				return
			}
			if strings.EqualFold(strings.TrimSpace(t.CreatedBy), newApprover) {
				c.JSON(http.StatusBadRequest, gin.H{"message": "不能将审核人转给提交人本人"})
				return
			}
			th := &TicketHandler{Store: h.Store}
			if !th.migrationApproverAllowed(newApprover) {
				c.JSON(http.StatusBadRequest, gin.H{"message": "审核人不在迁移审批人列表"})
				return
			}
			if approvers := th.resolveApprovers(t.SourceID, t.SourceDB); len(approvers) > 0 && !usernameInList(newApprover, approvers) {
				c.JSON(http.StatusBadRequest, gin.H{"message": "审核人不在实例/库审批人名单中"})
				return
			}
			t.AssignedApprover = newApprover
			t.AssignedApproverLogin = newApprover
			if t.IssueID != "" {
				if issue, _ := h.Store.GetIssue(t.IssueID); issue != nil && issue.Status == store.StatusPending {
					hist := store.AppendAssigneeTransferHistory(
						issue.AssigneeTransferHistory,
						store.FormatAssigneeTransferLine(issue.AssignedTo, newApprover, user),
					)
					_ = h.Store.UpdateIssueAssignee(issue.ID, newApprover, hist)
				}
			}
		case "", "draft", "none":
			t.AssignedApprover = newApprover
			t.AssignedApproverLogin = newApprover
		default:
			c.JSON(http.StatusBadRequest, gin.H{"message": "当前审批状态不可修改审核人"})
			return
		}
	}
	if req.Description != "" {
		t.Description = req.Description
	}
	if req.Name != "" {
		t.Name = req.Name
	}
	if err := h.Store.UpdateMigration(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *MigrationHandler) GetSettings(c *gin.Context) {
	s := h.Store.GetMigrationSettings()
	env := strings.TrimSpace(c.Query("env"))
	if env == "" {
		env = strings.TrimSpace(c.Query("environment"))
	}
	oss := s.OSS
	ossConfigured := s.OSS.Configured
	if env != "" {
		ossConfigured = h.Store.ResolveMigrationOSSConfiguredInTenant(effectiveListTenantID(h.Store, c), env)
		if row, err := h.Store.GetOSSSettingByEnvInTenant(env, effectiveListTenantID(h.Store, c)); err == nil && row != nil {
			oss = store.MigrationOSSSettings{
				Endpoint:      row.Endpoint,
				Bucket:        row.Bucket,
				Prefix:        row.Prefix,
				AccessKey:     row.AccessKey,
				SecretKey:     row.SecretKey,
				RetentionDays: row.RetentionDays,
				ChunkSizeMB:   row.ChunkSizeMB,
				Configured:    row.Configured,
			}
		} else if !ossConfigured {
			oss = store.MigrationOSSSettings{Configured: false}
		} else {
			// 回退 legacy：脱敏
			if oss.AccessKey != "" {
				oss.AccessKey = "********"
			}
			if oss.SecretKey != "" {
				oss.SecretKey = "********"
			}
			oss.Configured = true
		}
	} else {
		if oss.AccessKey != "" {
			oss.AccessKey = "********"
		}
		if oss.SecretKey != "" {
			oss.SecretKey = "********"
		}
		// 无 env 时：任一环境或 legacy 已配置即视为可用摘要
		if envs, err := h.Store.ListConfiguredOSSEnvs(); err == nil && len(envs) > 0 {
			ossConfigured = true
		}
	}
	ossEnvs, _ := h.Store.ListConfiguredOSSEnvs()
	if ossEnvs == nil {
		ossEnvs = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"approval_enabled":  s.ApprovalEnabled,
		"approver_user_ids": s.ApproverUserIDs,
		"oss":               oss,
		"oss_configured":    ossConfigured,
		"oss_envs":          ossEnvs,
		"performance":       s.Performance,
	}})
}

func (h *MigrationHandler) PutSettings(c *gin.Context) {
	var req struct {
		ApprovalEnabled *bool                        `json:"approval_enabled"`
		ApproverUserIDs *string                      `json:"approver_user_ids"`
		OSS             *store.MigrationOSSSettings  `json:"oss"`
		Performance     *store.MigrationPerfSettings `json:"performance"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	if req.ApprovalEnabled == nil && req.ApproverUserIDs == nil && req.OSS == nil && req.Performance == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	if req.ApprovalEnabled != nil {
		val := "false"
		if *req.ApprovalEnabled {
			val = "true"
		}
		if err := h.Store.SetGlobalArg(store.ArgMigrationApprovalEnabled, val, "数据迁移是否走审批"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	if req.ApproverUserIDs != nil {
		if err := h.Store.SetGlobalArg(store.ArgMigrationApproverUserIDs, strings.TrimSpace(*req.ApproverUserIDs), "数据迁移审核人登录名，逗号分隔"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	if req.OSS != nil {
		if err := h.Store.SaveMigrationOSS(*req.OSS); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	if req.Performance != nil {
		if err := h.Store.SaveMigrationPerf(*req.Performance); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) TestOSS(c *gin.Context) {
	var cfg store.MigrationOSSSettings
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	ok, msg := dbmig.TestOSS(c.Request.Context(), cfg)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": msg})
}

func normalizeScheduledAt(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return s
}
