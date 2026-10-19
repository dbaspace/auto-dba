package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"auto-dba/internal/ldapauth"
	"auto-dba/internal/middleware"
	"auto-dba/internal/notify"
	"auto-dba/internal/store"
)

type RBACHandler struct {
	Store  *store.DB
	Secret string
}

func (h *RBACHandler) requireAdmin(c *gin.Context) bool {
	u, err := h.Store.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return false
	}
	ok, err := h.Store.UserIsAdmin(u.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return false
	}
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅管理员可操作"})
		return false
	}
	return true
}

func (h *RBACHandler) MineMenus(c *gin.Context) {
	u, err := h.Store.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}
	tid := middleware.TenantID(c)
	if tid == "" {
		c.JSON(http.StatusOK, gin.H{"data": []store.MenuNode{}})
		return
	}
	var tree []store.MenuNode
	if middleware.IsImpersonating(c) {
		tree, err = h.Store.MenuTree(true)
	} else {
		tree, err = h.Store.MenusForUserInTenant(u.ID, tid, true)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if tree == nil {
		tree = []store.MenuNode{}
	}
	c.JSON(http.StatusOK, gin.H{"data": tree})
}

// PageActions GET /menus/page-actions?path= — 菜单登记的页面及其操作节点（granted 表示当前用户是否有权），
// 供尚未接入功能的页面展示占位。
func (h *RBACHandler) PageActions(c *gin.Context) {
	u, err := h.Store.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}
	page, actions, err := h.Store.PageActionMenus(c.Query("path"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if page == nil {
		c.JSON(http.StatusOK, gin.H{"data": nil})
		return
	}
	out := make([]gin.H, 0, len(actions))
	for _, a := range actions {
		ok, _ := h.Store.UserHasURNInTenant(u.ID, middleware.TenantID(c), a.URN)
		out = append(out, gin.H{"id": a.ID, "title": a.Title, "urn": a.URN, "granted": ok})
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"page":    gin.H{"id": page.ID, "title": page.Title, "path": page.Path, "urn": page.URN, "permission_desc": page.PermissionDesc},
		"actions": out,
	}})
}

func (h *RBACHandler) MenuTree(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	tree, err := h.Store.MenuAdminTree()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tree})
}

type menuBody struct {
	ParentID       string `json:"parent_id"`
	Title          string `json:"title"`
	URN            string `json:"urn"`
	Path           string `json:"path"`
	PermissionDesc string `json:"permission_desc"`
	Icon           string `json:"icon"`
	SortOrder      int    `json:"sort_order"`
	Enabled        *bool  `json:"enabled"`
}

func menuFromBody(body menuBody, fallbackEnabled bool) store.Menu {
	m := store.Menu{
		ParentID:       body.ParentID,
		Title:          body.Title,
		URN:            body.URN,
		Path:           body.Path,
		PermissionDesc: body.PermissionDesc,
		Icon:           strings.TrimSpace(body.Icon),
		SortOrder:      body.SortOrder,
		Enabled:        fallbackEnabled,
	}
	if body.Enabled != nil {
		m.Enabled = *body.Enabled
	}
	return m
}

func (h *RBACHandler) CreateMenu(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var body menuBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "名称必填"})
		return
	}
	m := menuFromBody(body, true)
	if err := h.Store.CreateMenu(&m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.GrantMenuFollowingParent(m.ID, m.ParentID)
	c.JSON(http.StatusOK, gin.H{"data": m})
}

func (h *RBACHandler) UpdateMenu(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	exist, err := h.Store.GetMenu(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "菜单不存在"})
		return
	}
	var body menuBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "名称必填"})
		return
	}
	m := menuFromBody(body, exist.Enabled)
	m.ID = exist.ID
	if err := h.Store.UpdateMenu(&m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": m})
}

func (h *RBACHandler) MoveMenu(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	exist, err := h.Store.GetMenu(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "菜单不存在"})
		return
	}
	var req struct {
		Dir string `json:"dir"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.Dir != "up" && req.Dir != "down") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "dir 须为 up 或 down"})
		return
	}
	if err := h.Store.MoveMenu(exist.ID, req.Dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *RBACHandler) DeleteMenu(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	if err := h.Store.DeleteMenu(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *RBACHandler) ListRoles(c *gin.Context) {
	list, err := h.Store.ListRoles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *RBACHandler) CreateRole(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var r store.Role
	if err := c.ShouldBindJSON(&r); err != nil || strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Code) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "name/code 必填"})
		return
	}
	if err := h.Store.CreateRole(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *RBACHandler) UpdateRole(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	exist, err := h.Store.GetRole(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "角色不存在"})
		return
	}
	var r store.Role
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	r.ID = exist.ID
	if err := h.Store.UpdateRole(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *RBACHandler) DeleteRole(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	if err := h.Store.DeleteRole(c.Param("id")); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "内置角色") || strings.Contains(msg, "不存在") {
			c.JSON(http.StatusBadRequest, gin.H{"message": msg})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *RBACHandler) ListRoleUsers(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	list, err := h.Store.ListRoleUsers(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, u := range list {
		out = append(out, gin.H{
			"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
			"role": u.Role, "role_id": u.RoleID, "source": u.Source,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *RBACHandler) GetRoleMenus(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	ids, err := h.Store.ListRoleMenuIDs(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if ids == nil {
		ids = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"data": ids})
}

func (h *RBACHandler) PutRoleMenus(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req struct {
		MenuIDs []string `json:"menu_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.ReplaceRoleMenus(c.Param("id"), req.MenuIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *RBACHandler) ListRoleOptions(c *gin.Context) {
	list, err := h.Store.ListRoles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.Role{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *RBACHandler) ListUsers(c *gin.Context) {
	list, err := h.Store.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *RBACHandler) CreateUser(c *gin.Context) {
	var req struct {
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		DisplayName string   `json:"display_name"`
		Email       string   `json:"email"`
		RoleID      string   `json:"role_id"`
		RoleIDs     []string `json:"role_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Password) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "username/password 必填"})
		return
	}
	email := strings.TrimSpace(req.Email)
	if email != "" && !strings.Contains(email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "邮箱格式不正确"})
		return
	}
	roleIDs := normalizeRoleIDs(req.RoleIDs, req.RoleID)
	if len(roleIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "role_ids 或 role_id 必填"})
		return
	}
	for _, rid := range roleIDs {
		role, err := h.Store.GetRole(rid)
		if err != nil || role == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "角色不存在"})
			return
		}
	}
	primary, _ := h.Store.GetRole(roleIDs[0])
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	u := &store.User{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Email:       email,
		Role:        primary.Code,
		RoleID:      primary.ID,
		RoleIDs:     roleIDs,
		Source:      store.UserSourcePlatform,
	}
	if u.DisplayName == "" {
		u.DisplayName = u.Username
	}
	if err := h.Store.CreateUser(u, string(hash)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "创建失败（用户名可能已存在）: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": u})
}

func (h *RBACHandler) UpdateUser(c *gin.Context) {
	exist, err := h.Store.GetUserByID(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return
	}
	var req struct {
		DisplayName string   `json:"display_name"`
		Email       string   `json:"email"`
		LarkEmail   *string  `json:"lark_email"`
		RoleID      string   `json:"role_id"`
		RoleIDs     []string `json:"role_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if req.LarkEmail != nil {
		if le := strings.TrimSpace(*req.LarkEmail); le != "" && !strings.Contains(le, "@") {
			c.JSON(http.StatusBadRequest, gin.H{"message": "飞书邮箱格式不正确"})
			return
		}
	}
	email := strings.TrimSpace(req.Email)
	if email != "" && !strings.Contains(email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "邮箱格式不正确"})
		return
	}
	roleIDs := normalizeRoleIDs(req.RoleIDs, req.RoleID)
	if len(roleIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "role_ids 或 role_id 必填"})
		return
	}
	for _, rid := range roleIDs {
		role, err := h.Store.GetRole(rid)
		if err != nil || role == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "角色不存在"})
			return
		}
	}
	name := req.DisplayName
	if name == "" {
		name = exist.DisplayName
	}
	if err := h.Store.UpdateUserRoles(exist.ID, roleIDs, name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	// 平台/LDAP 可改邮箱；SSO 也可补写便于识别（仍不能本系统重置密码）
	if err := h.Store.SetUserEmail(exist.ID, email); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if req.LarkEmail != nil {
		if err := h.Store.SetUserLarkEmail(exist.ID, *req.LarkEmail); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
	}
	updated, _ := h.Store.GetUserByID(exist.ID)
	if updated == nil {
		updated = exist
		updated.DisplayName = name
		updated.Email = email
		updated.RoleIDs = roleIDs
		if primary, _ := h.Store.GetRole(roleIDs[0]); primary != nil {
			updated.RoleID, updated.Role = primary.ID, primary.Code
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": updated})
}

func (h *RBACHandler) guardUserAction(c *gin.Context, target *store.User, allowSelf bool) bool {
	if target == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return false
	}
	if store.IsBuiltinAdminUser(target) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "内置管理员账号不可此操作"})
		return false
	}
	if !allowSelf && middleware.UserID(c) == target.ID {
		c.JSON(http.StatusBadRequest, gin.H{"message": "不能对自己执行此操作"})
		return false
	}
	return true
}

func (h *RBACHandler) ResetUserPassword(c *gin.Context) {
	exist, err := h.Store.GetUserByID(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return
	}
	src := strings.ToLower(strings.TrimSpace(exist.Source))
	if src == store.UserSourceSSO {
		c.JSON(http.StatusBadRequest, gin.H{"message": "SSO 账号请在统一认证中心修改密码"})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	pwd := strings.TrimSpace(req.Password)
	if len(pwd) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "新密码至少 6 位"})
		return
	}
	if src == store.UserSourceLDAP {
		cfg, cfgErr := h.Store.GetLdapConfig(h.Secret)
		if cfgErr != nil || cfg == nil || !cfg.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"message": "LDAP 未启用或配置不可用，无法改密"})
			return
		}
		if err := ldapauth.ResetPassword(cfg, exist.Username, pwd); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		_ = h.Store.UnlockUser(exist.ID)
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "LDAP 目录密码已更新"})
		return
	}
	// 平台账号
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpdateUserPassword(exist.ID, string(hash)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.UnlockUser(exist.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "密码已更新"})
}

func (h *RBACHandler) DisableUser(c *gin.Context) {
	exist, err := h.Store.GetUserByID(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return
	}
	if !h.guardUserAction(c, exist, false) {
		return
	}
	if err := h.Store.SetUserLoginDisabled(exist.ID, true); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	updated, _ := h.Store.GetUserByID(exist.ID)
	c.JSON(http.StatusOK, gin.H{"data": updated})
}

func (h *RBACHandler) EnableUser(c *gin.Context) {
	exist, err := h.Store.GetUserByID(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return
	}
	if !h.guardUserAction(c, exist, true) {
		return
	}
	if err := h.Store.SetUserLoginDisabled(exist.ID, false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	updated, _ := h.Store.GetUserByID(exist.ID)
	c.JSON(http.StatusOK, gin.H{"data": updated})
}

func (h *RBACHandler) UnlockUser(c *gin.Context) {
	exist, err := h.Store.GetUserByID(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return
	}
	if store.IsBuiltinAdminUser(exist) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "内置管理员账号不可此操作"})
		return
	}
	if err := h.Store.UnlockUser(exist.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	updated, _ := h.Store.GetUserByID(exist.ID)
	c.JSON(http.StatusOK, gin.H{"data": updated})
}

// SyncLarkUsers 对登录过本平台的用户批量点查飞书身份；不新建账号，不扫通讯录。
func (h *RBACHandler) SyncLarkUsers(c *gin.Context) {
	res, err := notify.SyncLarkUsers(h.Store)
	switch {
	case errors.Is(err, notify.ErrLarkSyncRunning):
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
		return
	case errors.Is(err, notify.ErrLarkSyncNotReady):
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	log.Printf("lark sync: total=%d synced=%d unmatched=%d requests=%d pages=%d msg=%s",
		res.Total, res.Synced, len(res.Unmatched), res.Requests, res.ScannedPages, res.Message)
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *RBACHandler) DeleteUser(c *gin.Context) {
	exist, err := h.Store.GetUserByID(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return
	}
	if !h.guardUserAction(c, exist, false) {
		return
	}
	if err := h.Store.DeleteUser(exist.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func normalizeRoleIDs(roleIDs []string, roleID string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(roleIDs)+1)
	for _, id := range roleIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		id := strings.TrimSpace(roleID)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}
