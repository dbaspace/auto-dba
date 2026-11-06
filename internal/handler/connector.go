package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type ConnectorHandler struct {
	Store *store.DB
}

func (h *ConnectorHandler) List(c *gin.Context) {
	env := c.Query("env")
	scope := effectiveListTenantID(h.Store, c)
	list, err := h.Store.ListConnectorsInTenant(scope, env)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.Connector{}
	}
	if scope != "" {
		extra, gerr := h.Store.ListGrantedConnectors(middleware.TenantID(c), middleware.UserID(c), env)
		if gerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": gerr.Error()})
			return
		}
		seen := map[string]struct{}{}
		for i := range list {
			seen[list[i].ID] = struct{}{}
		}
		for _, it := range extra {
			if _, ok := seen[it.ID]; ok {
				continue
			}
			list = append(list, it)
		}
	}
	h.enrichConnectorTenants(list)
	for i := range list {
		list[i].KafkaHost = cdc.NormalizeKafkaBootstrap(list[i].KafkaHost)
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *ConnectorHandler) enrichConnectorTenants(list []store.Connector) {
	names := map[string]string{}
	if tenants, err := h.Store.ListTenants(); err == nil {
		for _, t := range tenants {
			name := strings.TrimSpace(t.Name)
			if name == "" {
				name = t.Code
			}
			names[t.ID] = name
		}
	}
	for i := range list {
		list[i].TenantName = names[list[i].TenantID]
	}
}

func (h *ConnectorHandler) Get(c *gin.Context) {
	it, err := h.Store.GetConnectorInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "连接器不存在"})
		return
	}
	it.KafkaHost = cdc.NormalizeKafkaBootstrap(it.KafkaHost)
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func normalizeConnector(it *store.Connector) error {
	it.DBZHost = strings.TrimSpace(it.DBZHost)
	it.Env = strings.TrimSpace(it.Env)
	it.KafkaHost = cdc.NormalizeKafkaBootstrap(strings.TrimSpace(it.KafkaHost))
	it.Remark = strings.TrimSpace(it.Remark)
	it.MessageSchemaMode = store.NormalizeMessageSchemaMode(it.MessageSchemaMode)
	if it.DBZHost == "" {
		return errBad("请输入 dbz_host")
	}
	if it.Env == "" {
		return errBad("请选择环境")
	}
	if it.KafkaHost == "" {
		return errBad("请选择 Kafka 资源地址")
	}
	return nil
}

type badReq struct{ msg string }

func (e badReq) Error() string { return e.msg }

func errBad(msg string) error { return badReq{msg: msg} }

func (h *ConnectorHandler) Create(c *gin.Context) {
	var it store.Connector
	if err := c.ShouldBindJSON(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := normalizeConnector(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	stampTenantID(c, func(tid string) { it.TenantID = tid })
	if err := h.Store.CreateConnector(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *ConnectorHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetConnectorInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "连接器不存在"})
		return
	}
	var it store.Connector
	if err := c.ShouldBindJSON(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it.ID = exist.ID
	if err := normalizeConnector(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpdateConnector(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *ConnectorHandler) Delete(c *gin.Context) {
	if it, err := h.Store.GetConnectorInTenant(c.Param("id"), effectiveListTenantID(h.Store, c)); err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "连接器不存在"})
		return
	}
	if err := h.Store.DeleteConnector(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AssignTenantBatch POST /connectors/assign-tenant — 批量把连接器划到目标租户。
func (h *ConnectorHandler) AssignTenantBatch(c *gin.Context) {
	if !platformAdminAuditing(h.Store, c) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅平台管理员在租户模式下可划拨资源"})
		return
	}
	var req struct {
		TenantID string   `json:"tenant_id"`
		IDs      []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	to := strings.TrimSpace(req.TenantID)
	if to == "" {
		to = middleware.TenantID(c)
	}
	if to == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "目标租户必填"})
		return
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "ids 必填"})
		return
	}
	type failItem struct {
		ID      string `json:"id"`
		DBZHost string `json:"dbz_host,omitempty"`
		Message string `json:"message"`
	}
	okCount := 0
	failed := make([]failItem, 0)
	actor := middleware.UserID(c)
	for _, id := range ids {
		host := ""
		if it, _ := h.Store.GetConnector(id); it != nil {
			host = it.DBZHost
		}
		if err := h.Store.AssignConnectorTenant(id, to); err != nil {
			failed = append(failed, failItem{ID: id, DBZHost: host, Message: err.Error()})
			continue
		}
		okCount++
		_ = h.Store.AppendTenantAudit(to, actor, "resource.assign", "connector:"+id)
	}
	c.JSON(http.StatusOK, gin.H{
		"ok_count":   okCount,
		"fail_count": len(failed),
		"failed":     failed,
	})
}
