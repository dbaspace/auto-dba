package handler

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/secret"
)

const (
	srProxyPrefixFE       = "/api/v1/starrocks-fe-proxy/"
	srProxyPrefixBE       = "/api/v1/starrocks-be-proxy/"
	srLocalStaticURL      = "/starrocks-webroot/static"
	srLocalWebrootURL     = "/starrocks-webroot"
	srBeDefaultPort       = 8040
	srProxyBodyLim        = 8 << 20
	srWebrootRelDir       = "assets/starrocks-webroot"
)

var srProxyHTTP = &http.Client{
	Timeout: feWebTimeout,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		Proxy:           http.ProxyFromEnvironment,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		// 保留 Basic
		if auth := via[0].Header.Get("Authorization"); auth != "" {
			req.Header.Set("Authorization", auth)
		}
		return nil
	},
}

func srFeProxyPrefix(id string) string {
	return srProxyPrefixFE + url.PathEscape(strings.TrimSpace(id))
}

func srBeProxyPrefix(host string) string {
	return srProxyPrefixBE + url.PathEscape(strings.TrimSpace(host))
}

// ResolveStarRocksWebroot 解析 vendored webroot 目录（相对 cwd 或可执行文件旁）。
func ResolveStarRocksWebroot() string {
	cands := []string{srWebrootRelDir}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands,
			filepath.Join(filepath.Dir(exe), srWebrootRelDir),
			filepath.Join(filepath.Dir(exe), "..", srWebrootRelDir),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		cands = append(cands, filepath.Join(wd, srWebrootRelDir))
	}
	for _, p := range cands {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			return abs
		}
	}
	abs, _ := filepath.Abs(srWebrootRelDir)
	return abs
}

// MountStarRocksWebroot 托管本地 StarRocks webroot（css/js），供代理 HTML 改写引用。
func MountStarRocksWebroot(e *gin.Engine) {
	root := ResolveStarRocksWebroot()
	e.Static(srLocalWebrootURL, root)
}

func (h *StarRocksWebHandler) resolveFeUpstream(c *gin.Context, id string) (base, user, pass string, err error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", "", "", fmt.Errorf("缺少 FE 凭据 id")
	}
	it, e := h.Store.GetFeAdminInTenant(id, effectiveListTenantID(h.Store, c))
	if e != nil {
		return "", "", "", e
	}
	if it == nil {
		return "", "", "", fmt.Errorf("FE 凭据不存在")
	}
	plain, e := secret.Decrypt(h.Secret, it.PasswordEnc)
	if e != nil {
		return "", "", "", fmt.Errorf("解密失败")
	}
	host := firstFeNode(it.FeNode)
	if host == "" {
		host = strings.TrimSpace(it.Domain)
	}
	if host == "" {
		return "", "", "", fmt.Errorf("FE 节点地址为空")
	}
	port := it.Port
	if port <= 0 {
		port = feWebDefaultPort
	}
	base, err = normalizeStarrocksWebBase(host, port)
	if err != nil {
		return "", "", "", err
	}
	return base, strings.TrimSpace(it.Username), plain, nil
}

func normalizeBeHostURL(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("缺少 BE host")
	}
	// path 参数可能被 decode；兼容误传完整 URL
	if u, err := url.PathUnescape(host); err == nil && u != "" {
		host = u
	}
	if strings.Contains(host, "://") {
		return strings.TrimRight(host, "/"), nil
	}
	if strings.Contains(host, ":") {
		return "http://" + host, nil
	}
	return fmt.Sprintf("http://%s:%d", host, srBeDefaultPort), nil
}

// FeProxy 反向代理 FE Web（Basic 来自 FE管理）。
func (h *StarRocksWebHandler) FeProxy(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	base, user, pass, err := h.resolveFeUpstream(c, id)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusBadGateway, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:24px;color:#c00">%s</body></html>`, html.EscapeString(err.Error()))
		return
	}
	h.proxyStarRocks(c, base, user, pass, srFeProxyPrefix(id), "/system")
}

// BeProxy 反向代理 BE Web（无鉴权；host 为 ip:port）。
func (h *StarRocksWebHandler) BeProxy(c *gin.Context) {
	host := strings.TrimSpace(c.Param("host"))
	base, err := normalizeBeHostURL(host)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusBadRequest, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:24px;color:#c00">%s</body></html>`, html.EscapeString(err.Error()))
		return
	}
	h.proxyStarRocks(c, base, "", "", srBeProxyPrefix(host), "/mem_tracker")
}

func (h *StarRocksWebHandler) proxyStarRocks(c *gin.Context, upstreamBase, user, pass, proxyBase, defaultPath string) {
	rest := c.Param("filepath")
	if rest == "" || rest == "/" {
		rest = defaultPath
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	target := strings.TrimRight(upstreamBase, "/") + rest
	if q := c.Request.URL.RawQuery; q != "" {
		// 去掉平台 access_token，勿透传上游
		vals, _ := url.ParseQuery(q)
		vals.Del("access_token")
		if enc := vals.Encode(); enc != "" {
			target += "?" + enc
		}
	}

	var bodyBytes []byte
	if c.Request.Body != nil && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		bodyBytes, _ = io.ReadAll(io.LimitReader(c.Request.Body, srProxyBodyLim))
	}
	var rdr io.Reader
	if bodyBytes != nil {
		rdr = strings.NewReader(string(bodyBytes))
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, rdr)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "代理失败: " + err.Error()})
		return
	}
	if ct := c.GetHeader("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if acc := c.GetHeader("Accept"); acc != "" {
		req.Header.Set("Accept", acc)
	}
	req.Header.Set("User-Agent", "auto-dba/starrocks-proxy")
	if user != "" {
		token := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		req.Header.Set("Authorization", "Basic "+token)
	}

	resp, err := srProxyHTTP.Do(req)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusBadGateway, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:24px;color:#c00">上游不可达：%s</body></html>`, html.EscapeString(err.Error()))
		return
	}
	defer resp.Body.Close()

	// 上游鉴权失败：绝不回传 WWW-Authenticate，避免浏览器对本源弹 Basic 框
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, srProxyBodyLim))
		msg := fmt.Sprintf("上游拒绝访问（HTTP %d）", resp.StatusCode)
		if user != "" {
			msg += "。请到「配置 → FE管理」检查用户名、密码与节点地址是否正确。"
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(resp.StatusCode)
		_, _ = c.Writer.Write([]byte(fmt.Sprintf(
			`<!DOCTYPE html><html><head><meta charset="utf-8"><title>访问失败</title></head>`+
				`<body style="font-family:sans-serif;padding:24px;color:#b42318;line-height:1.5">%s</body></html>`,
			html.EscapeString(msg),
		)))
		return
	}

	ctype := resp.Header.Get("Content-Type")
	rewrite := shouldRewriteSRBody(ctype, rest)
	if !rewrite {
		copySRResponseHeaders(c.Writer.Header(), resp.Header)
		c.Status(resp.StatusCode)
		_, _ = io.Copy(c.Writer, io.LimitReader(resp.Body, srProxyBodyLim))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, srProxyBodyLim))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "读取上游失败: " + err.Error()})
		return
	}
	out := rewriteSRBody(raw, ctype, upstreamBase, proxyBase)
	copySRResponseHeaders(c.Writer.Header(), resp.Header)
	c.Header("Content-Type", ctype)
	if ctype == "" {
		c.Header("Content-Type", "text/html; charset=utf-8")
	}
	c.Status(resp.StatusCode)
	_, _ = c.Writer.Write(out)
}

func shouldRewriteSRBody(ctype, pathname string) bool {
	if strings.Contains(ctype, "text/html") || strings.Contains(ctype, "text/css") ||
		strings.Contains(ctype, "javascript") || strings.Contains(ctype, "application/json") {
		return true
	}
	return strings.HasSuffix(pathname, ".js") || strings.HasSuffix(pathname, ".css") ||
		strings.HasSuffix(pathname, ".html") || strings.HasSuffix(pathname, ".json")
}

func copySRResponseHeaders(dst http.Header, src http.Header) {
	for k, vs := range src {
		lk := strings.ToLower(k)
		if lk == "content-length" || lk == "transfer-encoding" ||
			lk == "x-frame-options" || lk == "content-security-policy" ||
			lk == "content-encoding" ||
			lk == "www-authenticate" || lk == "proxy-authenticate" {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

const srEmbedChrome = `<style id="auto-dba-sr-embed">
html, body { margin: 0 !important; padding: 8px !important; background: #fff !important; }
</style>
<meta http-equiv="Content-Security-Policy" content="frame-ancestors *">`

// 若上游 HTML 未引用样式，注入本仓 BE www css/js，避免裸表。
const srInjectAssets = `<link rel="stylesheet" href="/starrocks-webroot/be/bootstrap/css/bootstrap.min.css">
<link rel="stylesheet" href="/starrocks-webroot/be/starrocks.css">
<script src="/starrocks-webroot/be/jquery-3.5.0.min.js"></script>
<script src="/starrocks-webroot/be/bootstrap/js/bootstrap.min.js"></script>
<script src="/starrocks-webroot/be/starrocks.js"></script>
`

func rewriteSRBody(raw []byte, ctype, upstreamBase, proxyBase string) []byte {
	text := string(raw)
	changed := false

	if next, ok := rewriteStaticToLocal(text); ok {
		text = next
		changed = true
	}
	if next, ok := rewriteBeWwwToLocal(text); ok {
		text = next
		changed = true
	}

	up := strings.TrimRight(upstreamBase, "/")
	if up != "" && strings.Contains(text, up) {
		text = strings.ReplaceAll(text, up, proxyBase)
		changed = true
	}
	// 常见绝对路径挂到代理前缀（避免跳到站点根路径）；跳过本地 webroot / API
	for _, attr := range []string{`href="`, `src="`, `action="`, `href='`, `src='`, `action='`} {
		needle := attr + `/`
		if strings.Contains(text, needle) {
			text = rewriteRootAbsoluteAttr(text, attr, proxyBase)
			changed = true
		}
	}
	isHTML := strings.Contains(ctype, "text/html") || (!strings.Contains(ctype, "/") && looksLikeHTML(text))
	if isHTML {
		hasStyle := strings.Contains(text, "bootstrap") || strings.Contains(text, "starrocks-webroot")
		if !hasStyle {
			if strings.Contains(text, "</head>") {
				text = strings.Replace(text, "</head>", srInjectAssets+"</head>", 1)
			} else if strings.Contains(text, "<head") {
				text = strings.Replace(text, "<head", "<head>"+srInjectAssets, 1)
			} else if strings.Contains(text, "<body") {
				text = strings.Replace(text, "<body", srInjectAssets+"<body", 1)
			} else {
				text = srInjectAssets + text
			}
			changed = true
		}
		if !strings.Contains(text, "auto-dba-sr-embed") {
			if strings.Contains(text, "</head>") {
				text = strings.Replace(text, "</head>", srEmbedChrome+"</head>", 1)
			} else if strings.Contains(text, "<head") {
				text = strings.Replace(text, "<head", "<head>"+srEmbedChrome, 1)
			} else if strings.Contains(text, "<body") {
				text = strings.Replace(text, "<body", srEmbedChrome+"<body", 1)
			} else {
				text = srEmbedChrome + text
			}
			changed = true
		}
	}
	if !changed {
		return raw
	}
	return []byte(text)
}

func looksLikeHTML(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(l, "<!doctype html") || strings.HasPrefix(l, "<html") ||
		strings.Contains(l, "<body") || strings.Contains(l, "<table")
}

// rewriteStaticToLocal 将 FE /static/... 指到本仓托管路径。
func rewriteStaticToLocal(text string) (string, bool) {
	pairs := [][2]string{
		{`href="/static/`, `href="` + srLocalStaticURL + `/`},
		{`href='/static/`, `href='` + srLocalStaticURL + `/`},
		{`src="/static/`, `src="` + srLocalStaticURL + `/`},
		{`src='/static/`, `src='` + srLocalStaticURL + `/`},
		{`url("/static/`, `url("` + srLocalStaticURL + `/`},
		{`url('/static/`, `url('` + srLocalStaticURL + `/`},
		{`url(/static/`, `url(` + srLocalStaticURL + `/`},
		{`url("/static/fonts?res=`, `url("` + srLocalStaticURL + `/`},
		{`url('/static/fonts?res=`, `url('` + srLocalStaticURL + `/`},
	}
	return applyRewritePairs(text, pairs)
}

// rewriteBeWwwToLocal 将 BE www 根路径静态资源（/bootstrap/、/starrocks.css 等）指到本仓。
func rewriteBeWwwToLocal(text string) (string, bool) {
	be := srLocalWebrootURL + "/be"
	pairs := [][2]string{
		{`href="/bootstrap/`, `href="` + be + `/bootstrap/`},
		{`href='/bootstrap/`, `href='` + be + `/bootstrap/`},
		{`src="/bootstrap/`, `src="` + be + `/bootstrap/`},
		{`src='/bootstrap/`, `src='` + be + `/bootstrap/`},
		{`url("/bootstrap/`, `url("` + be + `/bootstrap/`},
		{`url('/bootstrap/`, `url('` + be + `/bootstrap/`},
	}
	files := []string{
		"jquery-3.5.0.min.js", "starrocks.css", "starrocks.js", "starrocks-logo.png",
		"d3.v2.js", "epoch.0.5.2.min.css", "epoch.0.5.2.min.js", "favicon.ico",
	}
	for _, name := range files {
		pairs = append(pairs,
			[2]string{`href="/` + name + `"`, `href="` + be + `/` + name + `"`},
			[2]string{`href='/` + name + `'`, `href='` + be + `/` + name + `'`},
			[2]string{`src="/` + name + `"`, `src="` + be + `/` + name + `"`},
			[2]string{`src='/` + name + `'`, `src='` + be + `/` + name + `'`},
		)
	}
	return applyRewritePairs(text, pairs)
}

func applyRewritePairs(text string, pairs [][2]string) (string, bool) {
	changed := false
	for _, p := range pairs {
		if strings.Contains(text, p[0]) {
			text = strings.ReplaceAll(text, p[0], p[1])
			changed = true
		}
	}
	return text, changed
}

// rewriteRootAbsoluteAttr 将 href="/xxx" 且非代理/本地静态前缀的改为 href="{proxyBase}/xxx"。
func rewriteRootAbsoluteAttr(text, attr, proxyBase string) string {
	prefix := strings.TrimRight(proxyBase, "/")
	needle := attr + `/`
	var b strings.Builder
	b.Grow(len(text))
	i := 0
	for {
		j := strings.Index(text[i:], needle)
		if j < 0 {
			b.WriteString(text[i:])
			break
		}
		j += i
		b.WriteString(text[i:j])
		rest := text[j+len(attr):] // starts with /
		if strings.HasPrefix(rest, prefix) ||
			strings.HasPrefix(rest, "/api/v1/starrocks-") ||
			strings.HasPrefix(rest, srLocalWebrootURL+"/") ||
			strings.HasPrefix(rest, "/static/") ||
			strings.HasPrefix(rest, "/bootstrap/") {
			b.WriteString(attr)
			i = j + len(attr)
			continue
		}
		b.WriteString(attr)
		b.WriteString(prefix)
		i = j + len(attr)
	}
	return b.String()
}
