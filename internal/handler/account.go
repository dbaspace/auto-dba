package handler

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type AccountHandler struct {
	Store  *store.DB
	Secret string
}

func redactAccount(it *store.DBAccount) {
	if it == nil {
		return
	}
	it.PasswordSet = strings.TrimSpace(it.Password) != ""
	it.Password = ""
}

func enrichAccount(st *store.DB, it *store.DBAccount) {
	if it == nil || it.InstanceID == "" {
		return
	}
	inst, _ := st.GetInstance(it.InstanceID)
	if inst == nil {
		return
	}
	it.InstanceName = inst.Name
	it.Host = inst.Host
	it.Port = inst.Port
	it.DomainName = inst.DomainName
	if it.DBType == "" {
		it.DBType = inst.DBType
	}
	if it.IDC == "" {
		it.IDC = inst.IDC
	}
}

func (h *AccountHandler) enrichAccountTenants(list []store.DBAccount) {
	names := map[string]string{}
	if ts, err := h.Store.ListTenants(); err == nil {
		for _, t := range ts {
			n := t.Name
			if n == "" {
				n = t.Code
			}
			names[t.ID] = n
		}
	}
	for i := range list {
		if n := names[list[i].TenantID]; n != "" {
			list[i].TenantName = n
		}
	}
}

func (h *AccountHandler) List(c *gin.Context) {
	list, err := h.Store.ListAccountsInTenant(effectiveListTenantID(h.Store, c), c.Query("db_type"), c.Query("idc"), c.Query("instance_id"), c.Query("q"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	for i := range list {
		enrichAccount(h.Store, &list[i])
		redactAccount(&list[i])
	}
	if auditScope(h.Store, c) {
		h.enrichAccountTenants(list)
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "audit_scope": auditScope(h.Store, c)})
}

func (h *AccountHandler) Get(c *gin.Context) {
	it, err := h.Store.GetAccountInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "账号不存在"})
		return
	}
	enrichAccount(h.Store, it)
	out := *it
	redactAccount(&out)
	if c.Query("view_credential") == "1" || c.Query("view_credential") == "true" {
		if !h.allowViewCredential(c, it) {
			return
		}
		plain, err := secret.Decrypt(h.Secret, it.Password)
		if err != nil {
			h.recordCredAudit(c, it, false, "decrypt failed")
			c.JSON(http.StatusInternalServerError, gin.H{"message": "密码解密失败"})
			return
		}
		h.recordCredAudit(c, it, true, "")
		c.JSON(http.StatusOK, gin.H{"data": out, "real_password": plain})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *AccountHandler) allowViewCredential(c *gin.Context, it *store.DBAccount) bool {
	// PAT：须开 credential scope 开关且令牌含 credential
	if middleware.IsPATAuth(c) {
		if !h.Store.AllowCredentialScope() || !store.ScopeHas(c.GetString(middleware.CtxPATScopes), store.ClientScopeCredential) {
			h.recordCredAudit(c, it, false, "pat missing credential scope")
			c.JSON(http.StatusForbidden, gin.H{"message": "令牌无权查看明文密码", "code": "credential_scope_denied"})
			return false
		}
		return true
	}
	if h.Store.HardenViewCredential() {
		uid := middleware.UserID(c)
		ok, err := h.Store.UserHasURN(uid, store.URNAccountsViewCred)
		if err != nil || !ok {
			// 兼容：持有账号管理侧栏也可（避免角色未配动作 URN 时全员被挡）；平台 admin 已由 UserHasURN 短路
			ok2, _ := h.Store.UserHasURN(uid, "um.sidebar.config-accounts")
			if !ok2 {
				h.recordCredAudit(c, it, false, "missing view_credential urn")
				c.JSON(http.StatusForbidden, gin.H{
					"message":      "无查看密码权限",
					"code":         "credential_forbidden",
					"required_urn": store.URNAccountsViewCred,
				})
				return false
			}
		}
	}
	if h.Store.HardenStepUp() && !store.HasValidStepUp(middleware.UserID(c)) {
		h.recordCredAudit(c, it, false, "step_up required")
		c.JSON(http.StatusForbidden, gin.H{
			"message": "查看密码需二次验证",
			"code":    "step_up_required",
		})
		return false
	}
	return true
}

func (h *AccountHandler) recordCredAudit(c *gin.Context, it *store.DBAccount, ok bool, msg string) {
	if h.Store == nil {
		return
	}
	target := ""
	aid, iid := "", ""
	if it != nil {
		aid = it.ID
		iid = it.InstanceID
		target = strings.TrimSpace(it.Username) + "@" + strings.TrimSpace(it.DBName)
	}
	err := h.Store.CreateCredentialAudit(&store.CredentialAudit{
		UserID: middleware.UserID(c), Username: middleware.Username(c),
		AccountID: aid, InstanceID: iid, Target: target,
		Success: ok, Message: msg, IP: clientLoginIP(c), UserAgent: c.Request.UserAgent(),
		ClientType: middleware.ClientType(c),
	})
	if err != nil {
		log.Printf("credential audit: %v", err)
	}
}

type accountSaveReq struct {
	InstanceID  string   `json:"instance_id"`
	DatabaseID  string   `json:"database_id"`
	DBName      string   `json:"db_name"`
	DBType      string   `json:"db_type"`
	IDC         string   `json:"idc"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Permissions []string `json:"permissions"`
	Description string   `json:"description"`
}

func (h *AccountHandler) fillFromInstance(it *store.DBAccount) error {
	if it.InstanceID == "" {
		return nil
	}
	inst, err := h.Store.GetInstanceInTenant(it.InstanceID, it.TenantID)
	if err != nil {
		return err
	}
	if inst == nil {
		return errAccountInstanceMissing
	}
	if it.DBType == "" {
		it.DBType = inst.DBType
	}
	if it.IDC == "" {
		it.IDC = inst.IDC
	}
	if it.DatabaseID == "" && it.DBName != "" {
		if rec, _ := h.Store.FindDatabase(it.InstanceID, it.DBName); rec != nil {
			it.DatabaseID = rec.ID
		}
	}
	return nil
}

var errAccountInstanceMissing = errString("实例不存在")

type errString string

func (e errString) Error() string { return string(e) }

func (h *AccountHandler) Create(c *gin.Context) {
	var req accountSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.DBName = strings.TrimSpace(req.DBName)
	if req.Username == "" || req.InstanceID == "" || req.DBName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例、数据库、用户名必填"})
		return
	}
	plain := strings.TrimSpace(req.Password)
	if plain == "" {
		plain = randomAccountPassword(16)
	}
	enc, err := secret.Encrypt(h.Secret, plain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "密码加密失败"})
		return
	}
	it := &store.DBAccount{
		TenantID:    middleware.TenantID(c),
		InstanceID:  req.InstanceID,
		DatabaseID:  req.DatabaseID,
		DBName:      req.DBName,
		DBType:      req.DBType,
		IDC:         req.IDC,
		Username:    req.Username,
		Password:    enc,
		Permissions: req.Permissions,
		Description: req.Description,
		CreatedBy:   middleware.Username(c),
		UpdatedBy:   middleware.Username(c),
	}
	if err := h.fillFromInstance(it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if exist, _ := h.Store.FindAccount(it.InstanceID, it.Username, it.DBName); exist != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "该实例下已存在同名账号"})
		return
	}
	if err := h.Store.CreateAccount(it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "创建失败: " + err.Error()})
		return
	}
	enrichAccount(h.Store, it)
	redactAccount(it)
	payload := gin.H{"data": it}
	// 加固开启时不在创建响应回传明文，避免 MCP/脚本截获；前端可再调 view_credential
	if !h.Store.HardenViewCredential() {
		payload["real_password"] = plain
	}
	c.JSON(http.StatusOK, payload)
}

func (h *AccountHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetAccountInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "账号不存在"})
		return
	}
	var req accountSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Username) != "" {
		exist.Username = strings.TrimSpace(req.Username)
	}
	if strings.TrimSpace(req.DBName) != "" {
		exist.DBName = strings.TrimSpace(req.DBName)
	}
	if req.InstanceID != "" {
		exist.InstanceID = req.InstanceID
	}
	if req.DatabaseID != "" {
		exist.DatabaseID = req.DatabaseID
	}
	if req.DBType != "" {
		exist.DBType = req.DBType
	}
	if req.IDC != "" {
		exist.IDC = req.IDC
	}
	if req.Permissions != nil {
		exist.Permissions = req.Permissions
	}
	exist.Description = req.Description
	exist.UpdatedBy = middleware.Username(c)
	if strings.TrimSpace(req.Password) != "" {
		enc, err := secret.Encrypt(h.Secret, req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "密码加密失败"})
			return
		}
		exist.Password = enc
	}
	if err := h.fillFromInstance(exist); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpdateAccount(exist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	enrichAccount(h.Store, exist)
	redactAccount(exist)
	c.JSON(http.StatusOK, gin.H{"data": exist})
}

func (h *AccountHandler) Delete(c *gin.Context) {
	exist, err := h.Store.GetAccountInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "账号不存在"})
		return
	}
	if err := h.Store.DeleteAccount(exist.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
