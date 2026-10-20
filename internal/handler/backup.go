package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/backup"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type BackupHandler struct {
	Store  *store.DB
	Secret string
	Engine *backup.Engine
}

func (h *BackupHandler) eng() *backup.Engine {
	if h != nil && h.Engine != nil {
		return h.Engine
	}
	return backup.New(h.Store, h.Secret, backup.Config{AllowEmbedded: true})
}

// Meta 返回引擎/方式枚举。
func (h *BackupHandler) Meta(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"engines": []string{"postgres", "mysql"},
		"methods": []string{
			store.BackupMethodLogicalFull,
			store.BackupMethodPhysicalFull,
			store.BackupMethodPhysicalIncr,
		},
		"method_labels": map[string]string{
			store.BackupMethodLogicalFull:  "逻辑全量",
			store.BackupMethodPhysicalFull: "物理全量",
			store.BackupMethodPhysicalIncr: "物理增量",
		},
		"storage_kinds": []string{store.BackupStorageLocal, store.BackupStorageS3},
		"triggers":      []string{store.BackupTriggerManual, store.BackupTriggerCron},
		"notes": map[string]string{
			"physical": "物理备份必须绑定 Backup Agent；PG 用 pg_basebackup，MySQL 用 xtrabackup/mariabackup",
		},
	}})
}

// --- Policies ---

func (h *BackupHandler) ListPolicies(c *gin.Context) {
	list, err := h.Store.ListBackupPoliciesInTenant(effectiveListTenantID(h.Store, c), c.Query("instance_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *BackupHandler) GetPolicy(c *gin.Context) {
	p, err := h.Store.GetBackupPolicyInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || p == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "策略不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": p})
}

func (h *BackupHandler) CreatePolicy(c *gin.Context) {
	var req store.BackupPolicy
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.validatePolicy(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	who := middleware.Username(c)
	req.CreatedBy, req.UpdatedBy = who, who
	req.Enabled = true
	stampTenantID(c, func(tid string) { req.TenantID = tid })
	if err := h.Store.CreateBackupPolicy(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": req})
}

func (h *BackupHandler) UpdatePolicy(c *gin.Context) {
	exist, err := h.Store.GetBackupPolicyInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "策略不存在"})
		return
	}
	var req store.BackupPolicy
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	req.ID = exist.ID
	if err := h.validatePolicy(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	req.CreatedBy = exist.CreatedBy
	req.UpdatedBy = middleware.Username(c)
	req.Enabled = exist.Enabled
	if err := h.Store.UpdateBackupPolicy(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": req})
}

func (h *BackupHandler) policyInTenant(c *gin.Context) (*store.BackupPolicy, bool) {
	p, err := h.Store.GetBackupPolicyInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || p == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "策略不存在"})
		return nil, false
	}
	return p, true
}

func (h *BackupHandler) DeletePolicy(c *gin.Context) {
	if _, ok := h.policyInTenant(c); !ok {
		return
	}
	if err := h.Store.DeleteBackupPolicy(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *BackupHandler) SetPolicyEnabled(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if _, ok := h.policyInTenant(c); !ok {
		return
	}
	if err := h.Store.SetBackupPolicyEnabled(c.Param("id"), req.Enabled, middleware.Username(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *BackupHandler) RunPolicy(c *gin.Context) {
	if _, ok := h.policyInTenant(c); !ok {
		return
	}
	runID, err := h.eng().Start(c.Param("id"), store.BackupTriggerManual, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"run_id": runID}})
}

func (h *BackupHandler) validatePolicy(c *gin.Context, p *store.BackupPolicy) error {
	tid := middleware.TenantID(c)
	p.Name = strings.TrimSpace(p.Name)
	p.InstanceID = strings.TrimSpace(p.InstanceID)
	p.StorageID = strings.TrimSpace(p.StorageID)
	if p.Name == "" || p.InstanceID == "" || p.StorageID == "" {
		return errMsg("名称、实例、存储必填")
	}
	inst, err := resolveInstanceInTenant(h.Store, c, p.InstanceID)
	if err != nil || inst == nil {
		return errMsg("实例不存在")
	}
	engine := store.NormalizeDBType(inst.DBType)
	if engine != "postgres" && engine != "mysql" && engine != "tidb" && engine != "oceanbase" {
		return errMsg("仅支持 PostgreSQL / MySQL 实例")
	}
	p.Engine = engine
	if p.Method == "" {
		p.Method = store.BackupMethodLogicalFull
	}
	if !store.ValidBackupMethod(p.Method) {
		return errMsg("不支持的备份方式")
	}
	st, err := h.Store.GetBackupStorageInTenant(p.StorageID, tid)
	if err != nil || st == nil {
		return errMsg("存储不存在")
	}
	_ = st
	if store.IsPhysicalBackupMethod(p.Method) {
		if strings.TrimSpace(p.AgentID) == "" {
			return errMsg("物理备份必须绑定 Backup Agent")
		}
		if p.Method == store.BackupMethodPhysicalIncr && engine == "postgres" {
			return errMsg("PostgreSQL 暂不支持 physical_incr（请用 physical_full + WAL/pgBackRest）；MySQL 可用物理增量")
		}
	}
	if aid := strings.TrimSpace(p.AgentID); aid != "" {
		a, err := h.Store.GetBackupAgentInTenant(aid, tid)
		if err != nil || a == nil {
			return errMsg("Agent 不存在")
		}
	}
	return nil
}

type strErr string

func (e strErr) Error() string { return string(e) }

func errMsg(s string) error { return strErr(s) }

// --- Runs ---

func (h *BackupHandler) ListRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Store.ListBackupRunsInTenant(effectiveListTenantID(h.Store, c), c.Query("policy_id"), c.Query("instance_id"), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *BackupHandler) GetRun(c *gin.Context) {
	r, err := h.Store.GetBackupRunInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || r == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "记录不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

// --- Storages ---

func (h *BackupHandler) ListStorages(c *gin.Context) {
	list, err := h.Store.ListBackupStoragesInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *BackupHandler) CreateStorage(c *gin.Context) {
	var req struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Endpoint  string `json:"endpoint"`
		Bucket    string `json:"bucket"`
		Prefix    string `json:"prefix"`
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
		LocalPath string `json:"local_path"`
		Remark    string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = store.BackupStorageLocal
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "名称必填"})
		return
	}
	enc := ""
	if strings.TrimSpace(req.SecretKey) != "" {
		var err error
		enc, err = secret.Encrypt(h.Secret, req.SecretKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	who := middleware.Username(c)
	it := &store.BackupStorage{
		Name: req.Name, Kind: kind, Endpoint: req.Endpoint, Bucket: req.Bucket, Prefix: req.Prefix,
		AccessKey: req.AccessKey, SecretKeyEnc: enc, LocalPath: req.LocalPath, Remark: req.Remark,
		CreatedBy: who, UpdatedBy: who,
	}
	stampTenantID(c, func(tid string) { it.TenantID = tid })
	if err := h.Store.CreateBackupStorage(it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	it.SecretKeyEnc = ""
	it.SecretKeySet = enc != ""
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *BackupHandler) UpdateStorage(c *gin.Context) {
	exist, err := h.Store.GetBackupStorageInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "存储不存在"})
		return
	}
	var req struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Endpoint  string `json:"endpoint"`
		Bucket    string `json:"bucket"`
		Prefix    string `json:"prefix"`
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
		LocalPath string `json:"local_path"`
		Remark    string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		exist.Name = req.Name
	}
	if strings.TrimSpace(req.Kind) != "" {
		exist.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	}
	exist.Endpoint = req.Endpoint
	exist.Bucket = req.Bucket
	exist.Prefix = req.Prefix
	exist.AccessKey = req.AccessKey
	exist.LocalPath = req.LocalPath
	exist.Remark = req.Remark
	if strings.TrimSpace(req.SecretKey) != "" {
		enc, err := secret.Encrypt(h.Secret, req.SecretKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		exist.SecretKeyEnc = enc
	}
	exist.UpdatedBy = middleware.Username(c)
	if err := h.Store.UpdateBackupStorage(exist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	exist.SecretKeyEnc = ""
	exist.SecretKeySet = true
	c.JSON(http.StatusOK, gin.H{"data": exist})
}

func (h *BackupHandler) DeleteStorage(c *gin.Context) {
	if st, err := h.Store.GetBackupStorageInTenant(c.Param("id"), effectiveListTenantID(h.Store, c)); err != nil || st == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "存储不存在"})
		return
	}
	if err := h.Store.DeleteBackupStorage(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- Agents ---

func (h *BackupHandler) ListAgents(c *gin.Context) {
	list, err := h.Store.ListBackupAgentsInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	scope := strings.ToLower(strings.TrimSpace(c.Query("scope")))
	if scope != "" {
		filtered := make([]store.BackupAgent, 0, len(list))
		for _, a := range list {
			if agentHasScope(a, scope) {
				filtered = append(filtered, a)
			}
		}
		list = filtered
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func agentHasScope(a store.BackupAgent, scope string) bool {
	groups := backup.ParseGroupsCSV(a.EnabledGroups)
	if len(groups) == 0 {
		r := backup.DecodeCapabilities(a.Capabilities)
		if len(r.Groups) == 0 {
			// 旧登记未写归属：兼容放行，避免策略/HA 选不到历史 Agent
			return true
		}
		return backup.HasGroupEnabled(r, scope)
	}
	for _, g := range groups {
		if g == scope {
			return true
		}
	}
	return false
}

func (h *BackupHandler) CreateAgent(c *gin.Context) {
	var req struct {
		Name           string   `json:"name"`
		BaseURL        string   `json:"base_url"`
		Username       string   `json:"username"`
		Password       string   `json:"password"`
		Engines        string   `json:"engines"`
		EnabledGroups  []string `json:"enabled_groups"`
		BackupEngines  []string `json:"backup_engines"`
		Tags           string   `json:"tags"`
		Remark         string   `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.BaseURL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "名称与 Base URL 必填"})
		return
	}
	groups := backup.NormalizeGroups(req.EnabledGroups)
	if len(groups) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请至少选择一个归属（backup/rdb/ha/k8s）"})
		return
	}
	bakEngines := backup.NormalizeBackupEngines(req.BackupEngines)
	if len(bakEngines) == 0 && strings.TrimSpace(req.Engines) != "" {
		bakEngines = backup.ParseBackupEnginesCSV(req.Engines)
	}
	for _, g := range groups {
		if g == backup.GroupBackup && len(bakEngines) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "勾选备份控制时请至少选择一种实例类型"})
			return
		}
	}
	enc := ""
	if strings.TrimSpace(req.Password) != "" {
		var err error
		enc, err = secret.Encrypt(h.Secret, req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	who := middleware.Username(c)
	enginesField := backup.ComposeAgentEngines(bakEngines, groups)
	if strings.TrimSpace(req.Engines) != "" && len(bakEngines) == 0 {
		enginesField = req.Engines
	}
	it := &store.BackupAgent{
		Name: req.Name, BaseURL: req.BaseURL, Username: req.Username, PasswordEnc: enc,
		Engines: enginesField, EnabledGroups: backup.GroupsCSV(groups),
		Tags: req.Tags, Remark: req.Remark, CreatedBy: who, UpdatedBy: who,
	}
	stampTenantID(c, func(tid string) { it.TenantID = tid })
	if err := h.Store.CreateBackupAgent(it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	it.PasswordEnc = ""
	it.PasswordSet = enc != ""
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *BackupHandler) DeleteAgent(c *gin.Context) {
	id := c.Param("id")
	a, err := h.Store.GetBackupAgentInTenant(id, effectiveListTenantID(h.Store, c))
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Agent 不存在"})
		return
	}
	force := c.Query("force") == "1" || c.Query("force") == "true"
	a.HostInstance = store.ParseBackupAgentHostTag(a.Tags)
	if a.HostInstance != "" {
		if err := h.eng().StopRemoteAgent(a); err != nil {
			if !force {
				c.JSON(http.StatusBadRequest, gin.H{
					"message": err.Error() + "；若仅删登记可加 force=1",
				})
				return
			}
		}
	}
	if err := h.Store.DeleteBackupAgent(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "stopped_remote": a.HostInstance != ""})
}

func (h *BackupHandler) ProbeAgent(c *gin.Context) {
	a, err := h.Store.GetBackupAgentInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Agent 不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	st := h.eng().ProbeAgentStatus(ctx, a)
	c.JSON(http.StatusOK, gin.H{
		"ok":      st.Status == backup.AgentStatusRunning,
		"status":  st.Status,
		"message": st.Message,
		"data":    st,
	})
}

// ListAgentStatus GET /backup/agents/status — 批量探活运行状态。
func (h *BackupHandler) ListAgentStatus(c *gin.Context) {
	list, err := h.Store.ListBackupAgentsInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	eng := h.eng()
	type item struct {
		st backup.AgentRuntimeStatus
	}
	ch := make(chan item, len(list))
	for i := range list {
		id := list[i].ID
		go func(agentID string) {
			a, _ := h.Store.GetBackupAgent(agentID)
			if a == nil {
				ch <- item{st: backup.AgentRuntimeStatus{ID: agentID, Status: backup.AgentStatusUnknown, Message: "不存在"}}
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			ch <- item{st: eng.ProbeAgentStatus(ctx, a)}
		}(id)
	}
	out := make([]backup.AgentRuntimeStatus, 0, len(list))
	for range list {
		out = append(out, (<-ch).st)
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// StartAgent POST /backup/agents/:id/start
func (h *BackupHandler) StartAgent(c *gin.Context) {
	a, err := h.Store.GetBackupAgentInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Agent 不存在"})
		return
	}
	if err := h.eng().StartRemoteAgent(a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	st := h.eng().ProbeAgentStatus(ctx, a)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": st})
}

// StopAgent POST /backup/agents/:id/stop
func (h *BackupHandler) StopAgent(c *gin.Context) {
	a, err := h.Store.GetBackupAgentInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Agent 不存在"})
		return
	}
	if err := h.eng().StopRemoteAgentOpt(a, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	st := h.eng().ProbeAgentStatus(ctx, a)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": st})
}

// DeployAgent POST /backup/agents/deploy — SSH 下发到 host 实例。
func (h *BackupHandler) DeployAgent(c *gin.Context) {
	var req struct {
		HostInstanceID string   `json:"host_instance_id"`
		ListenPort     int      `json:"listen_port"`
		RemoteDir      string   `json:"remote_dir"`
		Workdir        string   `json:"workdir"`
		EnabledGroups  []string `json:"enabled_groups"`
		BackupEngines  []string `json:"backup_engines"`
		Escalate       string   `json:"escalate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	id, err := h.eng().StartDeploy(backup.DeployOpts{
		HostInstanceID: req.HostInstanceID,
		ListenPort:     req.ListenPort,
		RemoteDir:      req.RemoteDir,
		Workdir:        req.Workdir,
		EnabledGroups:  req.EnabledGroups,
		BackupEngines:  req.BackupEngines,
		Escalate:       req.Escalate,
		Operator:       middleware.Username(c),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deploy_id": id}})
}

// PreflightAgent POST /backup/agents/preflight — 下发前按能力组检查目标机。
func (h *BackupHandler) PreflightAgent(c *gin.Context) {
	var req struct {
		HostInstanceID string   `json:"host_instance_id"`
		RemoteDir      string   `json:"remote_dir"`
		Workdir        string   `json:"workdir"`
		EnabledGroups  []string `json:"enabled_groups"`
		BackupEngines  []string `json:"backup_engines"`
		Escalate       string   `json:"escalate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	rep, err := h.eng().PreflightHost(c.Request.Context(), backup.DeployOpts{
		HostInstanceID: req.HostInstanceID,
		RemoteDir:      req.RemoteDir,
		Workdir:        req.Workdir,
		EnabledGroups:  req.EnabledGroups,
		BackupEngines:  req.BackupEngines,
		Escalate:       req.Escalate,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	missing := backup.MissingRequired(rep)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"report":  rep,
		"missing": missing,
		"ok":      len(missing) == 0,
		"summary": backup.FormatPreflightSummary(rep),
	}})
}

func (h *BackupHandler) GetDeploy(c *gin.Context) {
	it, err := h.Store.GetBackupAgentDeployInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "部署记录不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *BackupHandler) ListDeploys(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Store.ListBackupAgentDeploysInTenant(effectiveListTenantID(h.Store, c), c.Query("host_instance_id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// RedeployAgent 对已登记 Agent 按绑定 host 重新下发。
func (h *BackupHandler) RedeployAgent(c *gin.Context) {
	a, err := h.Store.GetBackupAgentInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || a == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Agent 不存在"})
		return
	}
	hostID := store.ParseBackupAgentHostTag(a.Tags)
	if hostID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "该 Agent 非平台下发（无 host 绑定），请手动维护"})
		return
	}
	var req struct {
		ListenPort    int      `json:"listen_port"`
		RemoteDir     string   `json:"remote_dir"`
		Workdir       string   `json:"workdir"`
		EnabledGroups []string `json:"enabled_groups"`
		BackupEngines []string `json:"backup_engines"`
		Escalate      string   `json:"escalate"`
	}
	_ = c.ShouldBindJSON(&req)
	groups := req.EnabledGroups
	if len(groups) == 0 {
		groups = backup.ParseGroupsCSV(a.EnabledGroups)
	}
	bakEngines := backup.NormalizeBackupEngines(req.BackupEngines)
	if len(bakEngines) == 0 {
		bakEngines = backup.ParseBackupEnginesCSV(a.Engines)
	}
	escalate := req.Escalate
	if strings.TrimSpace(escalate) == "" {
		escalate = store.ParseBackupAgentEscalateTag(a.Tags)
	}
	id, err := h.eng().StartDeploy(backup.DeployOpts{
		HostInstanceID: hostID,
		ListenPort:     req.ListenPort,
		RemoteDir:      req.RemoteDir,
		Workdir:        req.Workdir,
		EnabledGroups:  groups,
		BackupEngines:  bakEngines,
		Escalate:       escalate,
		Operator:       middleware.Username(c),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deploy_id": id}})
}

// CreateRestore 从成功的备份记录发起恢复工单（逻辑 或 physical_full）。
func (h *BackupHandler) CreateRestore(c *gin.Context) {
	var req struct {
		SourceRunID      string   `json:"source_run_id"`
		TargetInstanceID string   `json:"target_instance_id"`
		TargetDB         string   `json:"target_db"`
		Databases        []string `json:"databases"`
		AgentID          string   `json:"agent_id"`
		AssignedTo       string   `json:"assigned_to"`
		Description      string   `json:"description"`
		ConfirmProd      bool     `json:"confirm_prod"`
		RestoreKind      string   `json:"restore_kind"`
		DataDir          string   `json:"data_dir"`
		ConfirmStop      bool     `json:"confirm_stop"`
		RecoverTo        string   `json:"recover_to"`
		WalOrBinlogDir   string   `json:"wal_or_binlog_dir"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	run, err := h.Store.GetBackupRun(strings.TrimSpace(req.SourceRunID))
	if err != nil || run == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "备份记录不存在"})
		return
	}
	if !strings.EqualFold(run.Status, store.BackupRunSuccess) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅成功的备份记录可发起恢复"})
		return
	}
	method := strings.TrimSpace(run.Method)
	if method == "" {
		method = store.BackupMethodLogicalFull
	}
	kind := strings.ToLower(strings.TrimSpace(req.RestoreKind))
	if kind == "" {
		if store.IsPhysicalBackupMethod(method) {
			kind = "physical"
		} else {
			kind = "logical"
		}
	}
	switch kind {
	case "physical":
		if !store.IsPhysicalBackupMethod(method) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "物理恢复须选择物理备份记录"})
			return
		}
		if !req.ConfirmStop || strings.TrimSpace(req.DataDir) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "物理恢复须填写 data_dir 并勾选 confirm_stop（确认已停库）"})
			return
		}
	case "pitr":
		if !store.IsPhysicalBackupMethod(method) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "PITR 须选择物理备份记录"})
			return
		}
		if !req.ConfirmStop || strings.TrimSpace(req.DataDir) == "" || strings.TrimSpace(req.RecoverTo) == "" || strings.TrimSpace(req.WalOrBinlogDir) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "PITR 须填写 data_dir、recover_to、wal_or_binlog_dir 并勾选 confirm_stop"})
			return
		}
	default:
		if store.IsPhysicalBackupMethod(method) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "该记录为物理备份，请选择物理恢复或 PITR"})
			return
		}
	}
	tid := effectiveListTenantID(h.Store, c)
	target, err := h.Store.GetInstanceUsableInTenant(strings.TrimSpace(req.TargetInstanceID), tid)
	if err != nil || target == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "目标实例不可用或不在本租户"})
		return
	}
	targetDB := strings.TrimSpace(req.TargetDB)
	if targetDB == "" && len(req.Databases) > 0 {
		targetDB = strings.TrimSpace(req.Databases[0])
	}
	if kind == "logical" && targetDB == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请指定目标库 target_db"})
		return
	}
	preferred := strings.TrimSpace(req.AssignedTo)
	if preferred == "" {
		if def, _ := h.Store.GetGlobalArg("sync_default_approver"); def != "" {
			preferred = def
		}
	}
	assigned, aerr := resolveTenantAssignee(h.Store, middleware.TenantID(c), preferred)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	payload := backup.LogicalRestorePayload{
		SourceRunID:      run.ID,
		TargetInstanceID: target.ID,
		TargetDB:         targetDB,
		Databases:        req.Databases,
		AgentID:          strings.TrimSpace(req.AgentID),
		LocalPath:        run.LocalPath,
		ObjectKey:        run.ObjectKey,
		Engine:           run.Engine,
		ConfirmProd:      req.ConfirmProd,
		RestoreKind:      kind,
		DataDir:          strings.TrimSpace(req.DataDir),
		ConfirmStop:      req.ConfirmStop,
		RecoverTo:        strings.TrimSpace(req.RecoverTo),
		WalOrBinlogDir:   strings.TrimSpace(req.WalOrBinlogDir),
	}
	if payload.AgentID == "" {
		if p, _ := h.Store.GetBackupPolicy(run.PolicyID); p != nil {
			payload.AgentID = strings.TrimSpace(p.AgentID)
		}
	}
	body, _ := json.Marshal(payload)
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		switch kind {
		case "physical":
			desc = "物理恢复 · run=" + run.ID + " → " + target.Name + " data_dir=" + payload.DataDir
		case "pitr":
			desc = "PITR · run=" + run.ID + " → " + target.Name + " recover_to=" + payload.RecoverTo
		default:
			desc = "逻辑恢复 · run=" + run.ID + " → " + target.Name + "/" + targetDB
		}
	}
	issue := &store.Issue{
		ID:           uuid.NewString(),
		TicketNo:     "R" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10],
		IssueType:    store.IssueTypeBackupRestore,
		Type:         store.IssueTypeBackupRestore,
		InstanceID:   target.ID,
		DBName:       targetDB,
		DBType:       target.DBType,
		SQLContent:   string(body),
		Description:  desc,
		Status:       store.StatusPending,
		Submitter:    middleware.Username(c),
		AssignedTo:   assigned,
		NeedRollback: `{"kind":"backup_restore"}`,
	}
	stampTenantID(c, func(t string) {
		if issue != nil {
			issue.TenantID = t
		}
	})
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"issue": issue, "run": run}})
}

