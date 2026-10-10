package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbdeploy"
	"auto-dba/internal/store"
)

func templatedEngineParam(c *gin.Context, engine string) (string, bool) {
	e := strings.ToLower(strings.TrimSpace(engine))
	if e == "pg" || e == "postgresql" {
		e = store.DbDeployEnginePostgres
	}
	if !dbdeploy.TemplatedConfEngine(e) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数模板仅支持 mysql / postgres"})
		return "", false
	}
	return e, true
}

// ListConfTemplates GET /conf-templates?engine= 平台模板（含未自定义的内置大版本）。
func (h *DbDeployHandler) ListConfTemplates(c *gin.Context) {
	eng, ok := templatedEngineParam(c, c.Query("engine"))
	if !ok {
		return
	}
	list, err := dbdeploy.ListConfTemplateViews(h.Store, eng)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "meta": gin.H{
		"vars":           dbdeploy.ConfTplVarNames,
		"managed_keys":   dbdeploy.ManagedConfKeys(eng),
		"builtin_majors": dbdeploy.BuiltinConfMajors(eng),
	}})
}

// PutConfTemplate PUT /conf-templates {engine, major, content}
func (h *DbDeployHandler) PutConfTemplate(c *gin.Context) {
	var req struct {
		Engine  string `json:"engine"`
		Major   string `json:"major"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	eng, ok := templatedEngineParam(c, req.Engine)
	if !ok {
		return
	}
	major := strings.TrimSpace(req.Major)
	if major == "" {
		major = store.DbDeployConfTemplateAnyMajor
	}
	if !dbdeploy.ValidConfMajor(major) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "大版本格式应为 * 或 8.0 / 16"})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "模板内容为空；如需恢复内置请用「恢复内置」"})
		return
	}
	if err := dbdeploy.ValidateConfTemplate(eng, req.Content); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	t := &store.DbDeployConfTemplate{Engine: eng, Major: major, Content: req.Content, UpdatedBy: actorOfDb(c)}
	if err := h.Store.UpsertDbDeployConfTemplate(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

// ResetConfTemplate POST /conf-templates/reset {engine, major} 删除平台模板：内置大版本恢复为内置，其余直接移除。
func (h *DbDeployHandler) ResetConfTemplate(c *gin.Context) {
	var req struct {
		Engine string `json:"engine"`
		Major  string `json:"major"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	eng, ok := templatedEngineParam(c, req.Engine)
	if !ok {
		return
	}
	if err := h.Store.DeleteDbDeployConfTemplate(eng, req.Major); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *DbDeployHandler) templatedCluster(c *gin.Context) (*store.DbDeployCluster, bool) {
	cl, err := h.Store.GetDbDeployClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return nil, false
	}
	if cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return nil, false
	}
	if !dbdeploy.TemplatedConfEngine(cl.Engine) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数模板仅支持 mysql / postgres 集群"})
		return nil, false
	}
	return cl, true
}

// GetClusterConf GET /clusters/:id/conf 集群参数覆盖。
func (h *DbDeployHandler) GetClusterConf(c *gin.Context) {
	cl, ok := h.templatedCluster(c)
	if !ok {
		return
	}
	cc, err := h.Store.GetDbDeployClusterConf(cl.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if cc == nil {
		cc = &store.DbDeployClusterConf{ClusterID: cl.ID}
	}
	c.JSON(http.StatusOK, gin.H{"data": cc, "managed_keys": dbdeploy.ManagedConfKeys(cl.Engine)})
}

// PutClusterConf PUT /clusters/:id/conf {content}；内容为空即清除覆盖。
func (h *DbDeployHandler) PutClusterConf(c *gin.Context) {
	cl, ok := h.templatedCluster(c)
	if !ok {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := dbdeploy.ValidateClusterConf(cl.Engine, req.Content); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cc := &store.DbDeployClusterConf{ClusterID: cl.ID, Content: req.Content, UpdatedBy: actorOfDb(c)}
	if err := h.Store.UpsertDbDeployClusterConf(cc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cc})
}

// PreviewClusterConf GET|POST /clusters/:id/conf/preview 各节点最终 conf；POST 可带未保存的 {content} 覆盖试算。
func (h *DbDeployHandler) PreviewClusterConf(c *gin.Context) {
	cl, ok := h.templatedCluster(c)
	if !ok {
		return
	}
	var override *string
	if c.Request.Method == http.MethodPost {
		var req struct {
			Content *string `json:"content"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		override = req.Content
	}
	nodes, err := h.Store.ListDbDeployNodes(cl.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	primary := dbdeploy.PickPrimary(nodes)
	type item struct {
		NodeID  string                 `json:"node_id"`
		Host    string                 `json:"host"`
		Role    string                 `json:"role"`
		Path    string                 `json:"path,omitempty"`
		Content string                 `json:"content,omitempty"`
		Source  dbdeploy.ConfTplSource `json:"source"`
		Error   string                 `json:"error,omitempty"`
	}
	out := make([]item, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		p, body, src, err := dbdeploy.RenderTemplatedConf(h.Store, cl, n, primary, override)
		it := item{NodeID: n.ID, Host: n.Host, Role: n.Role, Path: p, Content: body, Source: src}
		if err != nil {
			it.Error = err.Error()
		}
		out = append(out, it)
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "major": dbdeploy.MajorVersion(cl.Engine, cl.Version)})
}
