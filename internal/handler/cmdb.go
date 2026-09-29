package handler

import (
	"io"
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
		Type: c.Query("type"), EnvID: c.Query("env_id"), RegionID: c.Query("region_id"),
		Q: c.Query("q"), Tag: c.Query("tag"), Limit: limit, Offset: offset,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) GetCI(c *gin.Context) {
	ci, err := h.svc().GetCI(c.Param("type"), c.Param("id"))
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
	g, err := h.svc().BuildGraph(c.Param("type"), c.Param("id"), depth)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": g})
}

func (h *CmdbHandler) ListRelations(c *gin.Context) {
	list, err := h.Store.ListCmdbRelations(c.Query("src_type"), c.Query("src_id"), c.Query("dst_type"), c.Query("dst_id"))
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
	if err := h.Store.CreateCmdbRelation(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendCmdbAudit(actorOf(c), "create", "relation", r.ID, "api", nil, r)
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *CmdbHandler) DeleteRelation(c *gin.Context) {
	id := c.Param("id")
	if err := h.Store.DeleteCmdbRelation(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendCmdbAudit(actorOf(c), "delete", "relation", id, "api", nil, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *CmdbHandler) ListTagDefs(c *gin.Context) {
	list, err := h.Store.ListCmdbTagDefs(c.Query("scope"))
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
	if err := h.Store.UpsertCmdbTagDef(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *CmdbHandler) DeleteTagDef(c *gin.Context) {
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

func (h *CmdbHandler) GetCITags(c *gin.Context) {
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
	if err := h.svc().SetTags(c.Param("type"), c.Param("id"), body.Tags, actorOf(c)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *CmdbHandler) ListAudit(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	list, err := h.Store.ListCmdbAuditLogs(c.Query("ci_type"), c.Query("ci_id"), c.Query("actor"), c.Query("q"), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CmdbHandler) DiscoverFromMonitor(c *gin.Context) {
	list, err := h.svc().DiscoverFromMonitor()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
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
