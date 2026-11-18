package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type AccessDomainHandler struct {
	Store *store.DB
}

func (h *AccessDomainHandler) tenantNameMap() map[string]string {
	out := map[string]string{}
	list, err := h.Store.ListTenants()
	if err != nil {
		return out
	}
	for _, t := range list {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			name = t.Code
		}
		out[t.ID] = name
	}
	return out
}

func (h *AccessDomainHandler) enrichTenants(list []store.AccessDomain) {
	names := h.tenantNameMap()
	for i := range list {
		list[i].TenantName = names[list[i].TenantID]
	}
}

func (h *AccessDomainHandler) enrichInstanceNames(list []store.AccessDomain) {
	instCache := map[string]*store.Instance{}
	for i := range list {
		iid := list[i].InstanceID
		if iid == "" {
			continue
		}
		inst := instCache[iid]
		if inst == nil {
			inst, _ = h.Store.GetInstance(iid)
			if inst != nil {
				instCache[iid] = inst
			}
		}
		if inst != nil {
			list[i].InstanceName = inst.Name
			list[i].Port = inst.Port
		}
	}
}

func (h *AccessDomainHandler) enrichList(list []store.AccessDomain) {
	h.enrichTenants(list)
	h.enrichInstanceNames(list)
}

// List GET /access-domains?db_type=&env_code=&q=
func (h *AccessDomainHandler) List(c *gin.Context) {
	tid := middleware.TenantID(c)
	dbType := c.Query("db_type")
	envCode := c.Query("env_code")
	q := c.Query("q")
	var list []store.AccessDomain
	var err error
	if platformAdminAuditing(h.Store, c) {
		list, err = h.Store.ListAccessDomains(dbType, envCode, q)
	} else {
		list, err = h.Store.ListAccessDomainsInTenant(tid, dbType, envCode, q)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if !platformAdminAuditing(h.Store, c) {
		extra, gerr := h.Store.ListGrantedAccessDomains(tid, middleware.UserID(c), dbType, envCode, q)
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
	h.enrichList(list)
	c.JSON(http.StatusOK, gin.H{"data": list, "audit_scope": platformAdminAuditing(h.Store, c)})
}

// Sync POST /access-domains/sync  body: { "db_type": "redis" } 可选
func (h *AccessDomainHandler) Sync(c *gin.Context) {
	var req struct {
		DBType string `json:"db_type"`
	}
	_ = c.ShouldBindJSON(&req)
	res, err := h.Store.SyncAllAccessDomains(req.DBType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res, "ok": true})
}

// UpdateApprovers PUT /access-domains/:id/approvers 仅 Redis 域名。
func (h *AccessDomainHandler) UpdateApprovers(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	var req struct {
		Approvers []string `json:"approvers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	row, err := h.Store.GetAccessDomain(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "域名不存在"})
		return
	}
	if store.NormalizeEngineType(row.DBType) != "redis" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 Redis 域名可设置审核人"})
		return
	}
	if !platformAdminAuditing(h.Store, c) && strings.TrimSpace(row.TenantID) != strings.TrimSpace(middleware.TenantID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"message": "只能编辑本租户的 Redis 域名审核人"})
		return
	}
	if err := h.Store.UpdateAccessDomainApprovers(id, req.Approvers); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it, _ := h.Store.GetAccessDomain(id)
	if it != nil {
		list := []store.AccessDomain{*it}
		h.enrichList(list)
		c.JSON(http.StatusOK, gin.H{"data": list[0], "ok": true})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type domainApproverBatchReq struct {
	IDs       []string `json:"ids"`
	Approvers []string `json:"approvers"`
}

// BatchApprovers POST /access-domains/batch-approvers — 向所选 Redis 域名追加审核人。
func (h *AccessDomainHandler) BatchApprovers(c *gin.Context) {
	var req domainApproverBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	add := uniqueTrimmedNames(req.Approvers)
	if len(add) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择要设置的审核人"})
		return
	}
	list, tid, ok := h.loadRedisApproverBatch(c, req.IDs)
	if !ok {
		return
	}
	members, err := loadTenantMemberUsernameSet(h.Store, tid)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	res, err := h.Store.BatchAppendAccessDomainApprovers(domainIDs(list), add, members)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	writeDomainApproverBatch(c, res)
}

// BatchRemoveApprovers POST /access-domains/batch-remove-approvers — 从所选 Redis 域名撤销审核人。
func (h *AccessDomainHandler) BatchRemoveApprovers(c *gin.Context) {
	var req domainApproverBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	remove := uniqueTrimmedNames(req.Approvers)
	if len(remove) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择要撤销的审核人"})
		return
	}
	list, _, ok := h.loadRedisApproverBatch(c, req.IDs)
	if !ok {
		return
	}
	res, err := h.Store.BatchRemoveAccessDomainApprovers(domainIDs(list), remove)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	writeDomainApproverBatch(c, res)
}

func (h *AccessDomainHandler) loadRedisApproverBatch(c *gin.Context, ids []string) ([]store.AccessDomain, string, bool) {
	list, tid, err := h.Store.LoadRedisDomainsSameTenant(ids)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return nil, "", false
	}
	if strings.TrimSpace(tid) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择同一租户下的域名"})
		return nil, "", false
	}
	if !platformAdminAuditing(h.Store, c) && strings.TrimSpace(tid) != strings.TrimSpace(middleware.TenantID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"message": "只能编辑本租户的 Redis 域名审核人"})
		return nil, "", false
	}
	return list, tid, true
}

func domainIDs(list []store.AccessDomain) []string {
	ids := make([]string, 0, len(list))
	for _, row := range list {
		ids = append(ids, row.ID)
	}
	return ids
}

func writeDomainApproverBatch(c *gin.Context, res store.DomainApproverBatchResult) {
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"ok_count":   res.OKCount,
		"skip_count": len(res.Skipped),
		"skipped":    res.Skipped,
		"dropped":    res.Dropped,
	})
}

// AssignTenant POST /access-domains/:id/assign-tenant
func (h *AccessDomainHandler) AssignTenant(c *gin.Context) {
	to, ok := parseAssignTenantID(h.Store, c)
	if !ok {
		return
	}
	id := c.Param("id")
	if err := h.Store.AssignAccessDomainTenant(id, to); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(to, middleware.UserID(c), "resource.assign", "domain:"+id)
	it, _ := h.Store.GetAccessDomain(id)
	if it != nil {
		list := []store.AccessDomain{*it}
		h.enrichList(list)
		c.JSON(http.StatusOK, gin.H{"data": list[0], "ok": true})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AssignTenantBatch POST /access-domains/assign-tenant — 批量指定域名归属租户。
func (h *AccessDomainHandler) AssignTenantBatch(c *gin.Context) {
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
		Domain  string `json:"domain,omitempty"`
		DBType  string `json:"db_type,omitempty"`
		Message string `json:"message"`
	}
	okCount := 0
	failed := make([]failItem, 0)
	actor := middleware.UserID(c)
	for _, id := range ids {
		domain, dbType := "", ""
		if it, _ := h.Store.GetAccessDomain(id); it != nil {
			domain, dbType = it.Domain, it.DBType
		}
		if err := h.Store.AssignAccessDomainTenant(id, to); err != nil {
			failed = append(failed, failItem{ID: id, Domain: domain, DBType: dbType, Message: err.Error()})
			continue
		}
		okCount++
		_ = h.Store.AppendTenantAudit(to, actor, "resource.assign", "domain:"+id)
	}
	c.JSON(http.StatusOK, gin.H{
		"ok_count":   okCount,
		"fail_count": len(failed),
		"failed":     failed,
	})
}

func (h *AccessDomainHandler) deleteAllowed(c *gin.Context, row *store.AccessDomain) error {
	if row == nil {
		return errAccessDomainMissing
	}
	if platformAdminAuditing(h.Store, c) {
		return nil
	}
	if strings.TrimSpace(row.TenantID) != strings.TrimSpace(middleware.TenantID(c)) {
		return errAccessDomainForbidden
	}
	return nil
}

var (
	errAccessDomainMissing   = errors.New("域名不存在")
	errAccessDomainForbidden = errors.New("只能删除本租户的域名")
)

// Delete DELETE /access-domains/:id
func (h *AccessDomainHandler) Delete(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	row, err := h.Store.GetAccessDomain(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.deleteAllowed(c, row); err != nil {
		status := http.StatusForbidden
		if errors.Is(err, errAccessDomainMissing) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.DeleteAccessDomain(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteBatch POST /access-domains/batch-delete
func (h *AccessDomainHandler) DeleteBatch(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
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
		Domain  string `json:"domain,omitempty"`
		DBType  string `json:"db_type,omitempty"`
		Message string `json:"message"`
	}
	okCount := 0
	failed := make([]failItem, 0)
	for _, id := range ids {
		row, err := h.Store.GetAccessDomain(id)
		domain, dbType := "", ""
		if row != nil {
			domain, dbType = row.Domain, row.DBType
		}
		if err != nil {
			failed = append(failed, failItem{ID: id, Message: err.Error()})
			continue
		}
		if err := h.deleteAllowed(c, row); err != nil {
			failed = append(failed, failItem{ID: id, Domain: domain, DBType: dbType, Message: err.Error()})
			continue
		}
		if err := h.Store.DeleteAccessDomain(id); err != nil {
			failed = append(failed, failItem{ID: id, Domain: domain, DBType: dbType, Message: err.Error()})
			continue
		}
		okCount++
	}
	c.JSON(http.StatusOK, gin.H{
		"ok_count":   okCount,
		"fail_count": len(failed),
		"failed":     failed,
	})
}
