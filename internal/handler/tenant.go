package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/sessionstore"
	"auto-dba/internal/store"
)

type TenantHandler struct {
	Store    *store.DB
	CfgTTL   func() time.Duration
	Secret   func() string
	Sessions *sessionstore.Store
}

func (h *TenantHandler) bindSession(c *gin.Context, u *store.User, tid, jti string, impersonate bool, ttl time.Duration) error {
	if h.Sessions == nil || !h.Sessions.Enabled() || jti == "" {
		return nil
	}
	if old := middleware.JTI(c); old != "" {
		_ = h.Sessions.RevokeJTI(c.Request.Context(), u.ID, old)
		_ = h.Store.MarkUserSessionOffline(old, "tenant_reissue", u.ID)
	}
	now := time.Now().UTC()
	kind := "normal"
	if impersonate {
		kind = "impersonate"
	}
	sess := sessionstore.Session{
		JTI: jti, UserID: u.ID, TenantID: tid, Username: u.Username,
		IP: clientLoginIP(c), UA: truncateUA(c.Request.UserAgent()),
		OnlineAt: now.Format(time.RFC3339), ExpireAt: now.Add(ttl).Format(time.RFC3339),
		Kind: kind,
	}
	if err := h.Sessions.Create(c.Request.Context(), sess); err != nil {
		return err
	}
	_ = h.Store.MarkUserSessionsOfflineAll(u.ID, "replaced_by_login", u.ID)
	if err := h.Store.UpsertUserSessionOnline(&store.UserSession{
		UserID: u.ID, Username: u.Username, TenantID: tid, JTI: jti,
		IP: sess.IP, UserAgent: sess.UA, Kind: kind,
		OnlineAt: sess.OnlineAt, ExpireAt: sess.ExpireAt,
	}); err != nil {
		log.Printf("tenant session db mirror: %v", err)
	}
	return nil
}

func (h *TenantHandler) List(c *gin.Context) {
	list, err := h.Store.ListTenants()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *TenantHandler) PlanOptions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": store.TenantPlanOptions})
}

func (h *TenantHandler) Mine(c *gin.Context) {
	list, err := h.Store.ListUserTenants(middleware.UserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "current_tenant_id": middleware.TenantID(c)})
}

func (h *TenantHandler) Create(c *gin.Context) {
	var req store.Tenant
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	if err := h.Store.CreateTenant(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(req.ID, middleware.UserID(c), "tenant.create", req.Code)
	// 创建者自动加入为租户 admin（若有 admin 角色）
	if r, _ := h.Store.GetRoleByCode("admin"); r != nil {
		_ = h.Store.UpsertTenantMember(&store.TenantMember{
			TenantID: req.ID,
			UserID:   middleware.UserID(c),
			RoleIDs:  []string{r.ID},
			Status:   store.MemberStatusActive,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": req})
}

func (h *TenantHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetTenant(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "租户不存在"})
		return
	}
	var req store.Tenant
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	req.ID = exist.ID
	req.Code = exist.Code
	if err := h.Store.UpdateTenant(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(req.ID, middleware.UserID(c), "tenant.update", req.Status)
	c.JSON(http.StatusOK, gin.H{"data": req})
}

// Delete DELETE /tenants/:id — 仅无成员且无绑定资源时可删。
func (h *TenantHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	exist, err := h.Store.GetTenant(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "租户不存在"})
		return
	}
	if err := h.Store.DeleteTenant(id); err != nil {
		var blocked *store.TenantDeleteBlockedError
		if errors.As(err, &blocked) {
			status := http.StatusConflict
			if blocked.Kind == "default" {
				status = http.StatusBadRequest
			}
			body := gin.H{"message": blocked.Message, "kind": blocked.Kind}
			if blocked.Members > 0 {
				body["members"] = blocked.Members
			}
			if len(blocked.Resources) > 0 {
				body["resources"] = blocked.Resources
			}
			c.JSON(status, body)
			return
		}
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *TenantHandler) ListMembers(c *gin.Context) {
	list, err := h.Store.ListTenantMembers(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *TenantHandler) UpsertMember(c *gin.Context) {
	tid := c.Param("id")
	var req struct {
		UserID  string   `json:"user_id"`
		RoleIDs []string `json:"role_ids"`
		Status  string   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	m := &store.TenantMember{
		TenantID: tid,
		UserID:   req.UserID,
		RoleIDs:  req.RoleIDs,
		Status:   req.Status,
	}
	if err := h.Store.UpsertTenantMember(m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(tid, middleware.UserID(c), "member.upsert", req.UserID)
	c.JSON(http.StatusOK, gin.H{"data": m})
}

func (h *TenantHandler) RemoveMember(c *gin.Context) {
	tid := c.Param("id")
	uid := c.Param("userId")
	if err := h.Store.RemoveTenantMember(tid, uid); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "不可移出") {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(tid, middleware.UserID(c), "member.remove", uid)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *TenantHandler) ListGrants(c *gin.Context) {
	list, err := h.Store.ListTenantGrantsFrom(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// ListGrantResources GET /tenants/:id/grant-resources?type=instance|database|domain|sr_cluster|db_deploy_cluster
func (h *TenantHandler) ListGrantResources(c *gin.Context) {
	typ := strings.TrimSpace(c.Query("type"))
	if typ == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "type 必填"})
		return
	}
	list, err := h.Store.ListGrantResources(c.Param("id"), typ)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.GrantResourceOption{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *TenantHandler) CreateGrant(c *gin.Context) {
	var g store.TenantGrant
	if err := c.ShouldBindJSON(&g); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	g.FromTenantID = c.Param("id")
	g.CreatedBy = middleware.Username(c)
	existed, _ := h.Store.FindActiveTenantGrant(g.ToTenantID, g.ToUserID, g.ResourceType, g.ResourceID)
	if err := h.Store.CreateTenantGrant(&g); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if existed == nil {
		_ = h.Store.AppendTenantAudit(g.FromTenantID, middleware.UserID(c), "grant.create", g.ResourceType+":"+g.ResourceID)
	}
	c.JSON(http.StatusOK, gin.H{"data": g, "skipped": existed != nil})
}

const maxBatchGrantIDs = 500

type batchGrantReq struct {
	ResourceType string   `json:"resource_type"`
	ResourceIDs  []string `json:"resource_ids"`
	ToTenantID   string   `json:"to_tenant_id"`
	ToUserID     string   `json:"to_user_id"`
	Permission   string   `json:"permission"`
}

type batchGrantFail struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// userCoveredByTenantGrant 用户所属任一活跃租户是否已有该资源的租户级授权。
func (h *TenantHandler) userCoveredByTenantGrant(userID, resourceType, resourceID string) (bool, error) {
	tens, err := h.Store.ListUserTenants(userID)
	if err != nil {
		return false, err
	}
	for i := range tens {
		ok, err := h.Store.HasResourceGrant(tens[i].ID, "", resourceType, resourceID)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// CreateGrantBatch POST /tenants/:id/grants/batch — 批量跨租授权（目标租户或指定用户，互斥），已覆盖则跳过。
func (h *TenantHandler) CreateGrantBatch(c *gin.Context) {
	var req batchGrantReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	fromID := strings.TrimSpace(c.Param("id"))
	toTenantID := strings.TrimSpace(req.ToTenantID)
	toUserID := strings.TrimSpace(req.ToUserID)
	typ := strings.TrimSpace(req.ResourceType)
	if fromID == "" || typ == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "from_tenant_id / resource_type 必填"})
		return
	}
	hasTenant := toTenantID != ""
	hasUser := toUserID != ""
	if hasTenant == hasUser {
		c.JSON(http.StatusBadRequest, gin.H{"message": "to_tenant_id 与 to_user_id 须恰好其一"})
		return
	}
	if hasTenant && fromID == toTenantID {
		c.JSON(http.StatusBadRequest, gin.H{"message": "目标租户不能与资源方相同"})
		return
	}
	if hasUser {
		u, err := h.Store.GetUserByID(toUserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if u == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "目标用户不存在"})
			return
		}
	}
	ids := uniqueTrimmedGrantIDs(req.ResourceIDs)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "resource_ids 不能为空"})
		return
	}
	if len(ids) > maxBatchGrantIDs {
		c.JSON(http.StatusBadRequest, gin.H{"message": "单次最多授权 500 个资源"})
		return
	}
	perm := strings.ToLower(strings.TrimSpace(req.Permission))
	if perm != "write" {
		perm = "read"
	}
	createdBy := middleware.Username(c)
	actorID := middleware.UserID(c)
	var created []store.TenantGrant
	var failed []batchGrantFail
	skipped := 0
	for _, rid := range ids {
		var covered bool
		var err error
		if hasTenant {
			var existing *store.TenantGrant
			existing, err = h.Store.FindActiveTenantGrant(toTenantID, "", typ, rid)
			covered = existing != nil
		} else {
			var existing *store.TenantGrant
			existing, err = h.Store.FindActiveTenantGrant("", toUserID, typ, rid)
			covered = existing != nil
			if err == nil && !covered {
				covered, err = h.userCoveredByTenantGrant(toUserID, typ, rid)
			}
		}
		if err != nil {
			failed = append(failed, batchGrantFail{ID: rid, Message: err.Error()})
			continue
		}
		if covered {
			skipped++
			continue
		}
		g := store.TenantGrant{
			FromTenantID: fromID,
			ToTenantID:   toTenantID,
			ToUserID:     toUserID,
			ResourceType: typ,
			ResourceID:   rid,
			Permission:   perm,
			CreatedBy:    createdBy,
		}
		if err := h.Store.CreateTenantGrant(&g); err != nil {
			failed = append(failed, batchGrantFail{ID: rid, Message: err.Error()})
			continue
		}
		detail := typ + ":" + rid
		if hasUser {
			detail += ":user:" + toUserID
		} else {
			detail += ":tenant:" + toTenantID
		}
		_ = h.Store.AppendTenantAudit(fromID, actorID, "grant.create", detail)
		created = append(created, g)
	}
	if created == nil {
		created = []store.TenantGrant{}
	}
	if failed == nil {
		failed = []batchGrantFail{}
	}
	c.JSON(http.StatusOK, gin.H{
		"created": len(created),
		"skipped": skipped,
		"failed":  failed,
		"data":    created,
	})
}

func uniqueTrimmedGrantIDs(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (h *TenantHandler) DeleteGrant(c *gin.Context) {
	if err := h.Store.DeleteTenantGrant(c.Param("grantId")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(c.Param("id"), middleware.UserID(c), "grant.delete", c.Param("grantId"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type batchDeleteGrantsReq struct {
	IDs []string `json:"ids"`
}

// DeleteGrantBatch POST /tenants/:id/grants/batch-delete — 批量撤销授权（须属该资源方租户）。
func (h *TenantHandler) DeleteGrantBatch(c *gin.Context) {
	var req batchDeleteGrantsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	fromID := strings.TrimSpace(c.Param("id"))
	ids := uniqueTrimmedGrantIDs(req.IDs)
	if fromID == "" || len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "ids 不能为空"})
		return
	}
	if len(ids) > maxBatchGrantIDs {
		c.JSON(http.StatusBadRequest, gin.H{"message": "单次最多撤销 500 条"})
		return
	}
	actorID := middleware.UserID(c)
	deleted := 0
	var failed []batchGrantFail
	for _, id := range ids {
		g, err := h.Store.GetTenantGrant(id)
		if err != nil {
			failed = append(failed, batchGrantFail{ID: id, Message: err.Error()})
			continue
		}
		if g == nil {
			failed = append(failed, batchGrantFail{ID: id, Message: "记录不存在"})
			continue
		}
		if g.FromTenantID != fromID {
			failed = append(failed, batchGrantFail{ID: id, Message: "不属于当前资源方租户"})
			continue
		}
		if err := h.Store.DeleteTenantGrant(id); err != nil {
			failed = append(failed, batchGrantFail{ID: id, Message: err.Error()})
			continue
		}
		_ = h.Store.AppendTenantAudit(fromID, actorID, "grant.delete", id)
		deleted++
	}
	if failed == nil {
		failed = []batchGrantFail{}
	}
	c.JSON(http.StatusOK, gin.H{"deleted": deleted, "failed": failed})
}

func (h *TenantHandler) AuditLogs(c *gin.Context) {
	list, err := h.Store.ListTenantAuditLogs(c.Param("id"), 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *TenantHandler) Switch(c *gin.Context) {
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.TenantID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "tenant_id 必填"})
		return
	}
	u, err := h.Store.GetUserByID(middleware.UserID(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}
	t, list, err := h.Store.PickLoginTenant(u.ID, req.TenantID)
	if err != nil || t == nil {
		msg := "无权进入该租户"
		if err != nil {
			msg = err.Error()
		}
		c.JSON(http.StatusForbidden, gin.H{"message": msg, "tenants": list})
		return
	}
	ttl := time.Hour
	if h.CfgTTL != nil {
		ttl = h.CfgTTL()
	}
	secret := ""
	if h.Secret != nil {
		secret = h.Secret()
	}
	tok, jti, err := middleware.SignSessionToken(secret, u.ID, u.Username, u.Role, t.ID, t.Code, false, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.bindSession(c, u, t.ID, jti, false, ttl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "写入登录会话失败: " + err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(t.ID, u.ID, "tenant.switch", t.Code)
	menus, _ := h.Store.MenusForUserInTenant(u.ID, t.ID, true)
	perms, _ := h.Store.ListUserURNsInTenant(u.ID, t.ID)
	plat, _ := h.Store.ListPlatformURNs(u.ID)
	c.JSON(http.StatusOK, gin.H{
		"token": tok,
		"user": gin.H{
			"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
			"role": u.Role, "role_id": u.RoleID, "menus": menus,
			"permissions": perms, "platform_permissions": plat,
			"current_tenant": t, "tenants": list,
		},
	})
}

func (h *TenantHandler) Impersonate(c *gin.Context) {
	tid := c.Param("id")
	ok, err := h.Store.UserHasURN(middleware.UserID(c), store.URNTenantsImpersonate)
	if err != nil || !ok {
		c.JSON(http.StatusForbidden, gin.H{"message": "无进入租户模式权限"})
		return
	}
	t, err := h.Store.GetTenant(tid)
	if err != nil || t == nil || t.Status != store.TenantStatusActive {
		c.JSON(http.StatusNotFound, gin.H{"message": "租户不存在或已停用"})
		return
	}
	u, err := h.Store.GetUserByID(middleware.UserID(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}
	ttl := time.Hour
	if h.CfgTTL != nil {
		ttl = h.CfgTTL()
	}
	secret := ""
	if h.Secret != nil {
		secret = h.Secret()
	}
	tok, jti, err := middleware.SignSessionToken(secret, u.ID, u.Username, u.Role, t.ID, t.Code, true, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.bindSession(c, u, t.ID, jti, true, ttl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "写入登录会话失败: " + err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(t.ID, u.ID, "tenant.impersonate", t.Code)
	menus, _ := h.Store.MenuTree(true)
	// 审计进入时用全量活跃租户列表，保证顶栏能显示名称（不仅是 UUID）
	tenants, _ := h.Store.ListTenants()
	active := make([]store.Tenant, 0, len(tenants))
	for _, it := range tenants {
		if it.Status == store.TenantStatusActive {
			active = append(active, it)
		}
	}
	if len(active) == 0 {
		active = []store.Tenant{*t}
	}
	c.JSON(http.StatusOK, gin.H{
		"token": tok,
		"user": gin.H{
			"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
			"role": u.Role, "menus": menus, "impersonating": true,
			"current_tenant": t, "tenants": active,
		},
	})
}

// ExitImpersonate POST /tenants/exit-impersonate — 结束租户模式，回到成员租户。
// 若当前已不在租户模式（重复点击 / 前后端态不一致），幂等返回成员会话，避免误报错误。
func (h *TenantHandler) ExitImpersonate(c *gin.Context) {
	ok, err := h.Store.UserHasURN(middleware.UserID(c), store.URNTenantsImpersonate)
	if err != nil || !ok {
		c.JSON(http.StatusForbidden, gin.H{"message": "无租户模式权限"})
		return
	}
	u, err := h.Store.GetUserByID(middleware.UserID(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}
	if !middleware.IsImpersonating(c) {
		t, list, perr := h.Store.PickLoginTenant(u.ID, middleware.TenantID(c))
		if perr != nil || t == nil {
			t, list, perr = h.Store.PickLoginTenant(u.ID, "")
		}
		if perr != nil || t == nil {
			c.JSON(http.StatusOK, gin.H{
				"already_exited": true,
				"user": gin.H{
					"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
					"role": u.Role, "role_id": u.RoleID, "impersonating": false,
					"tenants": list,
				},
			})
			return
		}
		menus, _ := h.Store.MenusForUserInTenant(u.ID, t.ID, true)
		perms, _ := h.Store.ListUserURNsInTenant(u.ID, t.ID)
		plat, _ := h.Store.ListPlatformURNs(u.ID)
		c.JSON(http.StatusOK, gin.H{
			"already_exited": true,
			"user": gin.H{
				"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
				"role": u.Role, "role_id": u.RoleID, "menus": menus,
				"permissions": perms, "platform_permissions": plat,
				"impersonating": false, "current_tenant": t, "tenants": list,
			},
		})
		return
	}
	fromTid := middleware.TenantID(c)
	detail := fromTid
	if from, _ := h.Store.GetTenant(fromTid); from != nil {
		detail = from.Code
		if detail == "" {
			detail = from.Name
		}
	}
	t, list, err := h.Store.PickLoginTenant(u.ID, "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "tenants": list})
		return
	}
	if t == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无可用成员租户，无法退出租户模式", "tenants": list})
		return
	}
	ttl := time.Hour
	if h.CfgTTL != nil {
		ttl = h.CfgTTL()
	}
	secret := ""
	if h.Secret != nil {
		secret = h.Secret()
	}
	tok, jti, err := middleware.SignSessionToken(secret, u.ID, u.Username, u.Role, t.ID, t.Code, false, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.bindSession(c, u, t.ID, jti, false, ttl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "写入登录会话失败: " + err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(fromTid, u.ID, "tenant.impersonate.end", detail)
	menus, _ := h.Store.MenusForUserInTenant(u.ID, t.ID, true)
	perms, _ := h.Store.ListUserURNsInTenant(u.ID, t.ID)
	plat, _ := h.Store.ListPlatformURNs(u.ID)
	c.JSON(http.StatusOK, gin.H{
		"token": tok,
		"user": gin.H{
			"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
			"role": u.Role, "role_id": u.RoleID, "menus": menus,
			"permissions": perms, "platform_permissions": plat,
			"impersonating": false, "current_tenant": t, "tenants": list,
		},
	})
}
