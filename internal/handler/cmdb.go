package handler

import (
	"database/sql"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cmdb"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type CmdbHandler struct {
	Store   *store.DB
	Secret  string
	Service *cmdb.Service
}

func (h *CmdbHandler) svc() *cmdb.Service {
	if h.Service != nil {
		return h.Service
	}
	h.Service = cmdb.New(h.Store)
	return h.Service
}

func actorOf(c *gin.Context) string {
	if v, ok := c.Get(middleware.CtxUsername); ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return ""
}

func (h *CmdbHandler) ListCIs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	list, err := h.svc().ListCIs(cmdb.ListOpts{
		Type: c.Query("type"), TenantID: middleware.TenantID(c), EnvID: c.Query("env_id"), RegionID: c.Query("region_id"),
		Q: c.Query("q"), Tag: c.Query("tag"), Limit: limit, Offset: offset,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) GetCI(c *gin.Context) {
	ci, err := h.svc().GetCIInTenant(c.Param("type"), c.Param("id"), middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if ci == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "配置项不存在"})
		return
	}
	tags, kv, _ := h.svc().EffectiveTags(ci.Type, ci.ID)
	c.JSON(http.StatusOK, gin.H{"data": ci, "tags": tags, "tag_map": kv})
}

func (h *CmdbHandler) Graph(c *gin.Context) {
	depth, _ := strconv.Atoi(c.DefaultQuery("depth", "1"))
	g, err := h.svc().BuildGraphInTenant(c.Param("type"), c.Param("id"), middleware.TenantID(c), depth)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": g})
}

func (h *CmdbHandler) Impact(c *gin.Context) {
	depth, _ := strconv.Atoi(c.DefaultQuery("depth", "2"))
	rep, err := h.svc().ImpactInTenant(c.Param("type"), c.Param("id"), middleware.TenantID(c), c.DefaultQuery("direction", "down"), depth)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rep})
}

func (h *CmdbHandler) ListRelations(c *gin.Context) {
	list, err := h.Store.ListCmdbRelationsInTenant(effectiveListTenantID(h.Store, c), c.Query("src_type"), c.Query("src_id"), c.Query("dst_type"), c.Query("dst_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) CreateRelation(c *gin.Context) {
	var r store.CmdbRelation
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	r.Source = "manual"
	stampTenantID(c, func(tid string) { r.TenantID = tid })
	if err := h.Store.CreateCmdbRelation(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendCmdbAudit(actorOf(c), "create", "relation", r.ID, "api", nil, r)
	h.svc().EmitWebhook("relation.create", r.SrcType, r.SrcID, actorOf(c), r)
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *CmdbHandler) DeleteRelation(c *gin.Context) {
	id := c.Param("id")
	if err := h.Store.DeleteCmdbRelationInTenant(id, middleware.TenantID(c)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"message": "关系不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendCmdbAudit(actorOf(c), "delete", "relation", id, "api", nil, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *CmdbHandler) ListTagDefs(c *gin.Context) {
	// 种子写当前审计租户；列表在审计会话下看全平台。
	if seedTID := middleware.TenantID(c); seedTID != "" {
		if err := h.Store.EnsureDefaultCmdbTagDefsForTenant(seedTID); err != nil {
			log.Printf("cmdb: seed tag defs tenant=%s: %v", seedTID, err)
		}
	}
	list, err := h.Store.ListCmdbTagDefsInTenant(effectiveListTenantID(h.Store, c), c.Query("scope"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) UpsertTagDef(c *gin.Context) {
	var r store.CmdbTagDef
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if id := c.Param("id"); id != "" {
		r.ID = id
	}
	if r.ID != "" {
		if old, err := h.Store.GetCmdbTagDefInTenant(r.ID, effectiveListTenantID(h.Store, c)); err != nil || old == nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "标签定义不存在"})
			return
		}
	}
	r.TenantID = middleware.TenantID(c)
	if err := h.Store.UpsertCmdbTagDef(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *CmdbHandler) DeleteTagDef(c *gin.Context) {
	if old, err := h.Store.GetCmdbTagDefInTenant(c.Param("id"), effectiveListTenantID(h.Store, c)); err != nil || old == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "标签定义不存在"})
		return
	}
	if err := h.Store.DeleteCmdbTagDef(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *CmdbHandler) SyncMonitorTags(c *gin.Context) {
	if err := h.svc().SyncMonitorTagsIntoCmdb(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ciInTenant 校验 :type/:id 指向的 CI 属于当前租户，否则写 404。
func (h *CmdbHandler) ciInTenant(c *gin.Context) bool {
	ci, err := h.svc().GetCIInTenant(c.Param("type"), c.Param("id"), middleware.TenantID(c))
	if err != nil || ci == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "配置项不存在"})
		return false
	}
	return true
}

func (h *CmdbHandler) GetCITags(c *gin.Context) {
	if !h.ciInTenant(c) {
		return
	}
	tags, kv, err := h.svc().EffectiveTags(c.Param("type"), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tags, "tag_map": kv})
}

func (h *CmdbHandler) SetCITags(c *gin.Context) {
	var body struct {
		Tags map[string]string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if !h.ciInTenant(c) {
		return
	}
	if err := h.svc().SetTags(c.Param("type"), c.Param("id"), body.Tags, actorOf(c)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	h.svc().EmitWebhook("ci.tags_update", c.Param("type"), c.Param("id"), actorOf(c), body.Tags)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *CmdbHandler) ListAudit(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	list, err := h.Store.ListCmdbAuditLogsInTenant(effectiveListTenantID(h.Store, c), c.Query("ci_type"), c.Query("ci_id"), c.Query("actor"), c.Query("q"), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) DiscoverFromMonitor(c *gin.Context) {
	list, err := h.svc().DiscoverFromMonitorInTenant(middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) DiscoverFromClusters(c *gin.Context) {
	list, err := h.svc().DiscoverFromClusters(middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) HealthOverview(c *gin.Context) {
	ov, err := h.svc().HealthOverview(middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ov})
}

func (h *CmdbHandler) ListSnapshots(c *gin.Context) {
	if !h.ciInTenant(c) {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	list, err := h.Store.ListCmdbCISnapshots(c.Param("type"), c.Param("id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) CreateSnapshot(c *gin.Context) {
	if !h.ciInTenant(c) {
		return
	}
	ciType, ciID := c.Param("type"), c.Param("id")
	var payload any
	var asBaseline bool
	switch ciType {
	case cmdb.TypeInstance:
		it, err := h.Store.GetInstance(ciID)
		if err != nil || it == nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
			return
		}
		payload = store.SnapshotInstanceMeta(it)
	case cmdb.TypeDatabase:
		db, err := h.Store.GetDatabase(ciID)
		if err != nil || db == nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "库不存在"})
			return
		}
		payload = store.SnapshotDatabaseMeta(db)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅支持 instance/database 快照"})
		return
	}
	if c.Query("baseline") == "1" || c.Query("baseline") == "true" {
		asBaseline = true
	}
	snap, err := h.Store.AppendCmdbCISnapshot(ciType, ciID, actorOf(c), payload, asBaseline)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	h.svc().EmitWebhook("ci.snapshot", ciType, ciID, actorOf(c), snap)
	c.JSON(http.StatusOK, gin.H{"data": snap})
}

func (h *CmdbHandler) ListDrifts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	list, err := h.Store.ListCmdbDrifts(middleware.TenantID(c), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) ListByApp(c *gin.Context) {
	tag := strings.TrimSpace(c.Query("tag"))
	if tag == "" {
		tag = strings.TrimSpace(c.Query("app"))
		if tag != "" && !strings.Contains(tag, "=") {
			tag = "app=" + tag
		}
	}
	if tag == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "tag 或 app 必填，如 app=foo"})
		return
	}
	var out []cmdb.CI
	for _, t := range []string{cmdb.TypeInstance, cmdb.TypeDatabase} {
		list, err := h.svc().ListCIs(cmdb.ListOpts{
			Type: t, TenantID: middleware.TenantID(c), Tag: tag, Limit: 500,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		out = append(out, list...)
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "tag": tag})
}

func (h *CmdbHandler) DiscoverDatabases(c *gin.Context) {
	// 复用已有 live list：前端也可直接调 /instances/:id/databases
	c.JSON(http.StatusOK, gin.H{"message": "请使用 GET /api/v1/instances/:id/databases 预览；登记请 POST /cmdb/discover/databases/register"})
}

func (h *CmdbHandler) RegisterDatabases(c *gin.Context) {
	var body struct {
		InstanceID string   `json:"instance_id"`
		DBNames    []string `json:"db_names"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if it, err := resolveInstanceInTenant(h.Store, c, body.InstanceID); err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	created, skipped, err := h.svc().RegisterDatabases(body.InstanceID, body.DBNames, actorOf(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"created": created, "skipped": skipped}})
}

func (h *CmdbHandler) Export(c *gin.Context) {
	format := strings.ToLower(c.DefaultQuery("format", "json"))
	types := c.DefaultQuery("types", "instance")
	if !strings.Contains(types, "instance") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "一期仅支持 types 含 instance"})
		return
	}
	if format == "csv" {
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename="cmdb-instances.csv"`)
		if err := h.svc().ExportInstancesCSV(c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		}
		return
	}
	rows, err := h.svc().ExportInstances()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (h *CmdbHandler) Import(c *gin.Context) {
	dryRun := c.DefaultQuery("dry_run", "1") == "1" || c.Query("dry_run") == "true"
	conflict := c.DefaultQuery("conflict", "skip")
	ct := c.ContentType()
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	var rows []cmdb.ImportInstanceRow
	if strings.Contains(ct, "csv") || strings.HasPrefix(string(raw), "id,") || strings.HasPrefix(string(raw), "name,") {
		rows, err = cmdb.ParseImportCSV(strings.NewReader(string(raw)))
	} else {
		rows, err = cmdb.ParseImportJSON(raw)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "解析失败: " + err.Error()})
		return
	}
	enc := func(plain string) (string, error) {
		return secret.Encrypt(h.Secret, plain)
	}
	res, err := h.svc().ImportInstances(rows, dryRun, conflict, actorOf(c), enc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *CmdbHandler) ListSyncJobs(c *gin.Context) {
	list, err := h.Store.ListCmdbSyncJobs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) UpsertSyncJob(c *gin.Context) {
	var j store.CmdbSyncJob
	if err := c.ShouldBindJSON(&j); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if id := c.Param("id"); id != "" {
		j.ID = id
	}
	if err := h.Store.UpsertCmdbSyncJob(&j); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": j})
}

func (h *CmdbHandler) DeleteSyncJob(c *gin.Context) {
	if err := h.Store.DeleteCmdbSyncJob(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *CmdbHandler) RunSyncJob(c *gin.Context) {
	run, cands, err := h.svc().RunSyncJob(c.Request.Context(), c.Param("id"), actorOf(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": run})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"run": run, "candidates": cands}})
}

func (h *CmdbHandler) ListSyncRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := h.Store.ListCmdbSyncRuns(c.Query("job_id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}
