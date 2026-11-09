package middleware

import (
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/clientaccess"
	"auto-dba/internal/notify"
	"auto-dba/internal/store"
)

// ClientAccessGate 机器通道（MCP/CLI/本地 AI）门禁：
// - 浏览器 JWT（无机器 Client-Type）一律放行，不改变现网；
// - 项目间/Copilot 等豁免前缀放行；
// - 机器通道须开启能力、持有效访问授权；enforce 时必须用 PAT，且路径在白名单内。
func ClientAccessGate(db *store.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db == nil {
			c.Next()
			return
		}
		isPAT := IsPATAuth(c)
		if !db.ClientAccessEnabled() {
			if isPAT {
				denyClientAccess(db, c, time.Now(), http.StatusForbidden, "client access disabled", "本地 AI/CLI/MCP 访问未开启", "client_access_disabled")
				return
			}
			c.Next()
			return
		}
		path := c.Request.URL.Path
		ct := ClientType(c)
		machine := isPAT || clientaccess.IsMachineClientType(ct)
		if !machine {
			c.Next()
			return
		}
		if !isPAT && clientaccess.IsExemptPath(path) {
			c.Next()
			return
		}
		start := time.Now()
		uid := UserID(c)
		grant, _ := db.ActiveClientAccessGrantForUser(uid)
		if grant == nil {
			denyClientAccess(db, c, start, http.StatusForbidden, "no client access grant", "未开通本地 AI/CLI/MCP 访问授权", "client_access_denied")
			return
		}
		if db.ClientAccessEnforceMachinePAT() && !isPAT {
			denyClientAccess(db, c, start, http.StatusForbidden, "machine channel requires PAT", "本地 AI/CLI/MCP 请使用个人访问令牌(PAT)，勿使用登录 JWT", "client_access_pat_required")
			return
		}
		if isPAT && !clientaccess.SourceAllows(grant.SourceCIDRs, observedClientIP(db, c)) {
			denyClientAccess(db, c, start, http.StatusForbidden, "source not allowed", "当前来源地址不在授权范围内", "client_access_source")
			return
		}
		scopes := c.GetString(CtxPATScopes)
		if isPAT && !store.ScopeHas(scopes, store.ClientScopeQuery) && !store.ScopeHas(scopes, store.ClientScopeCredential) {
			denyClientAccess(db, c, start, http.StatusForbidden, "scope missing", "令牌缺少 query/credential 权限", "client_access_scope")
			return
		}
		if isPAT && patScriptUpload(c) {
			denyClientAccess(db, c, start, http.StatusForbidden, "script upload denied", "不允许上传或提交脚本文件", "client_access_script_upload")
			return
		}
		allowCred := isPAT && store.ScopeHas(scopes, store.ClientScopeCredential) && db.AllowCredentialScope()
		if !clientaccess.WhitelistOKWithOpts(c.Request.Method, path, clientaccess.WhitelistOpts{AllowCredential: allowCred}) {
			denyClientAccess(db, c, start, http.StatusForbidden, "path not in whitelist", "该接口不在本地 AI/CLI/MCP 白名单内", "client_access_whitelist")
			return
		}
		c.Set(CtxGrantID, grant.ID)
		c.Next()
		writeClientAPIAudit(db, c, start, c.Writer.Status(), "")
	}
}

func observedClientIP(db *store.DB, c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	var trusted []*net.IPNet
	if db != nil {
		trusted = db.ClientAccessTrustedProxyNets()
	}
	return clientaccess.ObservedClientIP(c.Request.RemoteAddr, c.GetHeader("X-Forwarded-For"), trusted)
}

func patScriptUpload(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	ct := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.Contains(ct, "multipart/form-data") {
		return true
	}
	return clientaccess.ScriptFileName(c.GetHeader("Content-Disposition"))
}

func denyClientAccess(db *store.DB, c *gin.Context, start time.Time, status int, auditMsg, userMsg, code string) {
	writeClientAPIAudit(db, c, start, status, auditMsg)
	maybeSecurityAlert(db, c, auditMsg)
	c.JSON(status, gin.H{"message": userMsg, "code": code})
	c.Abort()
}

func maybeSecurityAlert(db *store.DB, c *gin.Context, reason string) {
	if db == nil {
		return
	}
	key := "deny:" + UserID(c) + ":" + ClientType(c)
	if !db.BumpSecurityDenial(key) {
		return
	}
	go notify.ClientAccessSecurityNotice(db, "", Username(c), ClientType(c), c.Request.URL.Path, reason)
}

func writeClientAPIAudit(db *store.DB, c *gin.Context, start time.Time, status int, msg string) {
	if db == nil {
		return
	}
	it := &store.ClientAPIAudit{
		UserID:     UserID(c),
		Username:   Username(c),
		TokenID:    TokenID(c),
		ClientType: ClientType(c),
		Method:     c.Request.Method,
		Path:       c.Request.URL.Path,
		IP:         observedClientIP(db, c),
		UserAgent:  c.Request.UserAgent(),
		Status:     status,
		LatencyMS:  time.Since(start).Milliseconds(),
		Message:    msg,
	}
	if it.ClientType == "" {
		it.ClientType = "machine"
	}
	if err := db.CreateClientAPIAudit(it); err != nil {
		log.Printf("client api audit: %v", err)
	}
}
