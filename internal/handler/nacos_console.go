package handler

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

const (
	nacosSessionTTL   = 20 * time.Minute
	nacosProxyTimeout = 30 * time.Second
	nacosProxyPrefix  = "/api/v1/nacos-proxy/"
	nacosBodyLimit    = 32 << 20
	// 默认本地 Nacos 控制台静态根（scripts/sync-nacos-console-assets.sh 输出目录）。
	nacosDefaultStaticDir = "assets/nacos-console"
)

type nacosCachedSession struct {
	AccessToken string
	Username    string
	GlobalAdmin bool
	Origin      string // scheme://host:port
	ConsolePath string // e.g. /next
	At          time.Time
}

var (
	nacosSessions sync.Map
	nacosHTTP     = &http.Client{
		Timeout: nacosProxyTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          64,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
)

func normalizeNacosConsoleURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	return raw
}

func nacosProxyEnvPrefix(env string) string {
	return nacosProxyPrefix + url.PathEscape(strings.TrimSpace(env))
}

// nacosSplitConsoleURL 从登记的控制台 URL 拆出 origin 与 console path（如 /next）。
func nacosSplitConsoleURL(consoleURL string) (origin, consolePath string, err error) {
	u, err := url.Parse(normalizeNacosConsoleURL(consoleURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", fmt.Errorf("控制台地址无效")
	}
	origin = u.Scheme + "://" + u.Host
	path := strings.TrimSuffix(u.Path, "/")
	if path == "" || path == "/" {
		consolePath = "/next"
	} else {
		consolePath = path
	}
	return origin, consolePath, nil
}

func nacosLogin(origin, username, password string) (accessToken string, globalAdmin bool, err error) {
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)
	candidates := []string{
		origin + "/v3/auth/user/login",
		origin + "/v1/auth/login",
	}
	var lastErr error
	for _, loginURL := range candidates {
		req, e := http.NewRequest(http.MethodPost, loginURL, strings.NewReader(form.Encode()))
		if e != nil {
			lastErr = e
			continue
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, e := nacosHTTP.Do(req)
		if e != nil {
			lastErr = e
			continue
		}
		raw, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if e != nil {
			lastErr = e
			continue
		}
		var parsed struct {
			AccessToken string `json:"accessToken"`
			Token       string `json:"token"`
			GlobalAdmin bool   `json:"globalAdmin"`
			Message     string `json:"message"`
			Msg         string `json:"msg"`
			Error       string `json:"error"`
		}
		if json.Unmarshal(raw, &parsed) != nil {
			lastErr = fmt.Errorf("Nacos 登录返回非 JSON（%s）", loginURL)
			continue
		}
		tok := strings.TrimSpace(parsed.AccessToken)
		if tok == "" {
			tok = strings.TrimSpace(parsed.Token)
		}
		if tok != "" && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return tok, parsed.GlobalAdmin, nil
		}
		msg := firstNonEmpty(parsed.Message, parsed.Msg, parsed.Error, strings.TrimSpace(string(raw)))
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		lastErr = fmt.Errorf("%s", msg)
		// 404/500 static resource → try next path；403 user not found 也是有效响应，可停止
		if resp.StatusCode == 403 || strings.Contains(strings.ToLower(msg), "user not found") ||
			strings.Contains(strings.ToLower(msg), "password") {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("Nacos 登录失败")
	}
	return "", false, lastErr
}

func (h *NacosAdminHandler) loadPlainCredential(env, tenantID string) (*store.NacosAdmin, string, error) {
	it, err := h.Store.GetNacosAdminByEnvInTenant(env, tenantID)
	if err != nil {
		return nil, "", err
	}
	if it == nil {
		return nil, "", fmt.Errorf("该环境未配置 Nacos 控制台")
	}
	plain, err := secret.Decrypt(h.Secret, it.PasswordEnc)
	if err != nil {
		return nil, "", fmt.Errorf("解密失败")
	}
	return it, plain, nil
}

func (h *NacosAdminHandler) ensureNacosSession(env, tenantID string) (*nacosCachedSession, *store.NacosAdmin, error) {
	it, plain, err := h.loadPlainCredential(env, tenantID)
	if err != nil {
		return nil, nil, err
	}
	origin, consolePath, err := nacosSplitConsoleURL(it.ConsoleURL)
	if err != nil {
		return nil, it, err
	}
	envKey := strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(it.Environment)
	if envKey == "|" {
		envKey = strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(env)
	}
	if cached, ok := nacosSessions.Load(envKey); ok {
		s := cached.(*nacosCachedSession)
		if s.AccessToken != "" && s.Origin == origin && time.Since(s.At) < nacosSessionTTL {
			return s, it, nil
		}
	}
	tok, admin, err := nacosLogin(origin, it.Username, plain)
	if err != nil {
		return nil, it, fmt.Errorf("Nacos 登录失败: %w", err)
	}
	next := &nacosCachedSession{
		AccessToken: tok,
		Username:    it.Username,
		GlobalAdmin: admin,
		Origin:      origin,
		ConsolePath: consolePath,
		At:          time.Now(),
	}
	nacosSessions.Store(envKey, next)
	return next, it, nil
}

// Warm pre-establishes Nacos session so iframe bootstrap skips cold login.
func (h *NacosAdminHandler) Warm(c *gin.Context) {
	env := strings.TrimSpace(c.Query("env"))
	if env == "" {
		env = strings.TrimSpace(c.Query("environment"))
	}
	if env == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请指定 env"})
		return
	}
	start := time.Now()
	_, _, err := h.ensureNacosSession(env, middleware.TenantID(c))
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		log.Printf("nacos_warm env=%s err=%v elapsed_ms=%d", env, err, elapsed)
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error(), "elapsed_ms": elapsed})
		return
	}
	log.Printf("nacos_warm env=%s ok elapsed_ms=%d", env, elapsed)
	c.JSON(http.StatusOK, gin.H{"ok": true, "elapsed_ms": elapsed})
}

// ConsoleBootstrap returns HTML that seeds token into localStorage and redirects into same-origin proxy.
func (h *NacosAdminHandler) ConsoleBootstrap(c *gin.Context) {
	start := time.Now()
	env := strings.TrimSpace(c.Param("env"))
	if env == "" {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:24px;color:#c00">缺少环境参数</body></html>`)
		return
	}
	sess, it, err := h.ensureNacosSession(env, middleware.TenantID(c))
	if err != nil {
		fallback := ""
		if it != nil {
			fallback = normalizeNacosConsoleURL(it.ConsoleURL)
		}
		log.Printf("nacos_console env=%s login_err=%v total_ms=%d", env, err, time.Since(start).Milliseconds())
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:24px">
<p style="color:#c00">无法代登 Nacos：%s</p>
%s
</body></html>`, html.EscapeString(err.Error()), nacosFallbackLinks(fallback))
		return
	}
	state, _ := json.Marshal(map[string]any{
		"accessToken": sess.AccessToken,
		"username":    sess.Username,
		"globalAdmin": sess.GlobalAdmin,
	})
	proxyHome := nacosProxyEnvPrefix(env) + sess.ConsolePath + "/#/configurationManagement"
	openURL := ""
	if it != nil {
		openURL = normalizeNacosConsoleURL(it.ConsoleURL) + "/#/configurationManagement"
	}
	log.Printf("nacos_console env=%s total_ms=%d", env, time.Since(start).Milliseconds())
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	setNacosEmbedFrameAncestors(c)
	// 会话写入 nacos_embed_token，避免覆盖平台 SPA 的 localStorage.token（JWT）。
	c.String(http.StatusOK, `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>Nacos Console</title>`+nacosTokenStorageShim+`</head>
<body style="font-family:sans-serif;padding:24px;color:#666">
正在打开 Nacos 控制台…
<script>
(function () {
  try { localStorage.setItem('nacos_embed_token', JSON.stringify(%s)); } catch (e) {}
  var next = %q;
  var fallback = %q;
  try {
    window.location.replace(next);
  } catch (e) {
    if (fallback) window.open(fallback, '_blank');
  }
})();
</script>
%s
</body></html>`, string(state), proxyHome, openURL, nacosFallbackLinks(openURL))
}

func nacosFallbackLinks(consoleURL string) string {
	if strings.TrimSpace(consoleURL) == "" {
		return `<p>请先到配置中心「Nacos 配置」补全控制台地址与账号。</p>`
	}
	u := html.EscapeString(consoleURL)
	return fmt.Sprintf(`<p><a href="%s" target="_blank" rel="noreferrer">新窗口打开控制台</a></p>`, u)
}

// shouldRewriteNacosBody 仅对需要改写的文本缓冲：HTML（注入 shim）与 JSON（可能含绝对 origin）。
// JS/CSS 等大静态不 rewrite，走流式转发。
func shouldRewriteNacosBody(ctype, pathname string) bool {
	if strings.Contains(ctype, "text/html") || strings.HasSuffix(pathname, ".html") {
		return true
	}
	if strings.Contains(ctype, "application/json") || strings.HasSuffix(pathname, ".json") {
		return true
	}
	return false
}

func nacosStaticExt(pathname string) bool {
	ext := strings.ToLower(filepath.Ext(pathname))
	switch ext {
	case ".js", ".css", ".map", ".woff", ".woff2", ".ttf", ".eot", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp":
		return true
	default:
		return false
	}
}

func (h *NacosAdminHandler) nacosConsoleStaticDir() string {
	if h == nil {
		return nacosDefaultStaticDir
	}
	s := strings.TrimSpace(h.StaticDir)
	if s == "-" {
		return "" // 显式关闭本地静态
	}
	if s != "" {
		return s
	}
	return nacosDefaultStaticDir
}

// resolveNacosLocalStatic 将 proxy path（如 /next/js/main.js）映射到本地静态文件；防目录穿越。
func resolveNacosLocalStatic(staticDir, rest string) (abs string, ok bool) {
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" || !nacosStaticExt(rest) {
		return "", false
	}
	rel := strings.TrimPrefix(rest, "/")
	if rel == "" || strings.Contains(rel, "..") {
		return "", false
	}
	root, err := filepath.Abs(staticDir)
	if err != nil {
		return "", false
	}
	candidate := filepath.Join(root, filepath.FromSlash(rel))
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	relToRoot, err := filepath.Rel(root, absCandidate)
	if err != nil || strings.HasPrefix(relToRoot, "..") || relToRoot == ".." {
		return "", false
	}
	st, err := os.Stat(absCandidate)
	if err != nil || st.IsDir() {
		return "", false
	}
	return absCandidate, true
}

func (h *NacosAdminHandler) tryServeNacosLocalStatic(c *gin.Context, env, rest string, start time.Time) bool {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}
	abs, ok := resolveNacosLocalStatic(h.nacosConsoleStaticDir(), rest)
	if !ok {
		return false
	}
	f, err := os.Open(abs)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false
	}
	c.Header("X-Nacos-Static", "local")
	http.ServeContent(c.Writer, c.Request, filepath.Base(abs), st.ModTime(), f)
	log.Printf("nacos_proxy env=%s path=%s status=%d local_bytes=%d total_ms=%d",
		env, rest, c.Writer.Status(), st.Size(), time.Since(start).Milliseconds())
	return true
}

// nacosEmbedChrome 仅注入样式；frame-ancestors 必须走 HTTP 头（meta 会被浏览器忽略并报错）。
const nacosEmbedChrome = `<style id="auto-dba-nacos-embed">
html, body { width:100% !important; height:100% !important; margin:0 !important; overflow:auto !important; }
</style>`

const nacosFrameAncestorsCSP = "frame-ancestors *"

func setNacosEmbedFrameAncestors(c *gin.Context) {
	c.Header("Content-Security-Policy", nacosFrameAncestorsCSP)
}

// nacosTokenStorageShim 把 iframe 内对 localStorage.token 的读写转到 nacos_embed_token，
// 避免与平台 SPA 共用同源 localStorage 时覆盖 JWT。
const nacosTokenStorageShim = `<script id="auto-dba-nacos-token-shim">
(function () {
  if (window.__autoDbaNacosTokenShim) return;
  window.__autoDbaNacosTokenShim = true;
  var NS = 'nacos_embed_token';
  var proto = Storage.prototype;
  var _get = proto.getItem;
  var _set = proto.setItem;
  var _remove = proto.removeItem;
  proto.getItem = function (k) {
    if (k === 'token' && this === localStorage) return _get.call(this, NS);
    return _get.call(this, k);
  };
  proto.setItem = function (k, v) {
    if (k === 'token' && this === localStorage) return _set.call(this, NS, v);
    return _set.call(this, k, v);
  };
  proto.removeItem = function (k) {
    if (k === 'token' && this === localStorage) return _remove.call(this, NS);
    return _remove.call(this, k);
  };
})();
</script>`

func rewriteNacosBody(raw []byte, ctype, origin, proxyBase string) ([]byte, bool) {
	isHTML := strings.Contains(ctype, "text/html")
	text := string(raw)
	changed := false
	if origin != "" && strings.Contains(text, origin) {
		text = strings.ReplaceAll(text, origin, proxyBase)
		changed = true
	}
	if isHTML {
		for {
			low := strings.ToLower(text)
			i := strings.Index(low, `http-equiv="content-security-policy"`)
			if i < 0 {
				i = strings.Index(low, `http-equiv='content-security-policy'`)
			}
			if i < 0 {
				break
			}
			start := strings.LastIndex(text[:i], "<")
			end := strings.Index(text[i:], ">")
			if start < 0 || end < 0 {
				break
			}
			text = text[:start] + text[i+end+1:]
			changed = true
		}
		headBits := ""
		if !strings.Contains(text, "auto-dba-nacos-token-shim") {
			headBits += nacosTokenStorageShim
		}
		if !strings.Contains(text, "auto-dba-nacos-embed") {
			headBits += nacosEmbedChrome
		}
		if headBits != "" {
			if strings.Contains(text, "</head>") {
				text = strings.Replace(text, "</head>", headBits+"</head>", 1)
			} else {
				text = insertAfterHeadOpen(text, headBits)
			}
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	return []byte(text), true
}

func copyNacosResponseHeaders(dst http.Header, src http.Header) {
	for k, vs := range src {
		lk := strings.ToLower(k)
		if lk == "content-length" || lk == "transfer-encoding" || lk == "x-frame-options" || lk == "content-security-policy" {
			continue
		}
		dst.Del(k)
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// Proxy reverse-proxies Nacos console UI/API under same origin with accessToken headers.
// GET 静态资源优先从 assets/nacos-console 本地提供；HTML/API 仍走远程。
func (h *NacosAdminHandler) Proxy(c *gin.Context) {
	start := time.Now()
	env := strings.TrimSpace(c.Param("env"))
	rest := c.Param("filepath")
	if rest != "" && !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	if rest != "" && h.tryServeNacosLocalStatic(c, env, rest, start) {
		return
	}

	sess, _, err := h.ensureNacosSession(env, middleware.TenantID(c))
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusBadGateway, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:24px;color:#c00">%s</body></html>`, html.EscapeString(err.Error()))
		return
	}
	if rest == "" {
		rest = sess.ConsolePath + "/"
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	// 会话就绪后再试一次本地（rest 可能刚补上 ConsolePath；静态后缀才会命中）
	if h.tryServeNacosLocalStatic(c, env, rest, start) {
		return
	}
	target := sess.Origin + rest
	if q := c.Request.URL.RawQuery; q != "" {
		target += "?" + q
	}

	var bodyBytes []byte
	if c.Request.Body != nil && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		bodyBytes, _ = io.ReadAll(io.LimitReader(c.Request.Body, nacosBodyLimit))
	}

	var rdr io.Reader
	if bodyBytes != nil {
		rdr = strings.NewReader(string(bodyBytes))
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, rdr)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "Nacos 代理失败: " + err.Error()})
		return
	}
	if ct := c.GetHeader("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if acc := c.GetHeader("Accept"); acc != "" {
		req.Header.Set("Accept", acc)
	}
	req.Header.Set("Authorization", "Bearer "+sess.AccessToken)
	req.Header.Set("accessToken", sess.AccessToken)

	upStart := time.Now()
	resp, err := nacosHTTP.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "Nacos 代理失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	ctype := resp.Header.Get("Content-Type")
	proxyBase := nacosProxyEnvPrefix(env)
	rewrite := shouldRewriteNacosBody(ctype, rest)
	upMs := time.Since(upStart).Milliseconds()

	if !rewrite {
		copyNacosResponseHeaders(c.Writer.Header(), resp.Header)
		c.Status(resp.StatusCode)
		n, _ := io.Copy(c.Writer, resp.Body)
		log.Printf("nacos_proxy env=%s path=%s status=%d stream_bytes=%d up_ms=%d total_ms=%d",
			env, rest, resp.StatusCode, n, upMs, time.Since(start).Milliseconds())
		return
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, nacosBodyLimit))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "Nacos 代理读取失败: " + err.Error()})
		return
	}
	rewritten, did := rewriteNacosBody(raw, ctype, sess.Origin, proxyBase)
	copyNacosResponseHeaders(c.Writer.Header(), resp.Header)
	if strings.Contains(ctype, "text/html") || strings.HasSuffix(rest, ".html") {
		setNacosEmbedFrameAncestors(c)
	}
	c.Data(resp.StatusCode, ctype, rewritten)
	log.Printf("nacos_proxy env=%s path=%s status=%d bytes=%d rewrite=%v up_ms=%d total_ms=%d",
		env, rest, resp.StatusCode, len(rewritten), did, upMs, time.Since(start).Milliseconds())
}
