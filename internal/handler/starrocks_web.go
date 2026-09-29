package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

const (
	feWebDefaultPort  = 8030
	feWebTimeout      = 30 * time.Second
	feWebMaxBodyBytes = 8 << 20
)

type feWebTabDef struct {
	Path     string
	TabKey   string
	TabLabel string
	Title    string
}

var feWebTabDefs = []feWebTabDef{
	{"/system", "system", "system", "系统信息"},
	{"/backend", "backends", "backends", "BE 节点"},
	{"/log", "logs", "logs", "日志"},
	{"/query", "queries", "queries", "查询列表"},
	{"/session", "sessions", "sessions", "会话"},
	{"/variable", "variables", "variables", "变量"},
	{"/ha", "ha", "ha", "高可用"},
	{"/help", "help", "help", "帮助"},
}

var beWebTabDefs = []feWebTabDef{
	{"/mem_tracker", "mem_tracker", "mem_tracker", "MemTracker"},
	{"/memz", "memz", "memz", "Memz"},
	{"/varz", "varz", "varz", "Varz"},
}

var feWebTabAliases = map[string]string{
	"backend":  "backends",
	"log":      "logs",
	"query":    "queries",
	"session":  "sessions",
	"variable": "variables",
}

// 主路径 404 时的备用路径（对齐 Hub）
var feWebFallbackPaths = map[string][]string{
	"/backend":  {"/api/show_proc?path=/backends"},
	"/query":    {"/api/show_proc?path=/current_queries"},
	"/session":  {"/api/show_proc?path=/sessions"},
	"/variable": {"/api/show_proc?path=/variables"},
}

type StarRocksWebHandler struct {
	Store  *store.DB
	Secret string
}

type starrocksWebPageReq struct {
	FeAdminID string `json:"fe_admin_id"`
	Kind      string `json:"kind"` // fe | be
	BaseURL   string `json:"base_url"`
	BeBaseURL string `json:"be_base_url"` // universe-fe / Hub 兼容字段
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Tab       string `json:"tab"`
	Path      string `json:"path"`
}

type starrocksWebPanel struct {
	TabKey      string `json:"tab_key"`
	TabLabel    string `json:"tab_label"`
	Title       string `json:"title"`
	HTML        string `json:"html"`
	Body        string `json:"body"` // 同 html，兼容 universe-fe
	URL         string `json:"url"`
	OK          bool   `json:"ok"`
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (h *StarRocksWebHandler) FetchPage(c *gin.Context) {
	var req starrocksWebPageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "fe"
	}
	base, user, pass, err := h.resolveAuth(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defs := feWebTabDefs
	if kind == "be" {
		defs = beWebTabDefs
	}
	tab := strings.ToLower(strings.TrimSpace(req.Tab))
	if alias, ok := feWebTabAliases[tab]; ok {
		tab = alias
	}
	subPath := strings.TrimSpace(req.Path)
	var panels []starrocksWebPanel
	for _, d := range defs {
		if tab != "" && tab != d.TabKey && tab != d.TabLabel {
			continue
		}
		pageURL := buildFePageURL(base, d.Path, subPath, len(defs) == 1 || (tab != "" && (tab == d.TabKey || tab == d.TabLabel)))
		st, ct, body, fetchErr := fetchBasicAuthPageEx(c, pageURL, user, pass)
		if st == http.StatusNotFound && kind == "fe" && subPath == "" {
			if fbs, ok := feWebFallbackPaths[d.Path]; ok {
				for _, fb := range fbs {
					fbURL := strings.TrimRight(base, "/") + fb
					fst, fct, fbody, ferr := fetchBasicAuthPageEx(c, fbURL, user, pass)
					if ferr == nil && fst >= 200 && fst < 300 {
						pageURL, st, ct, body, fetchErr = fbURL, fst, fct, fbody, nil
						break
					}
				}
			}
		}
		ok := fetchErr == nil && st >= 200 && st < 300
		errMsg := ""
		if fetchErr != nil {
			errMsg = fetchErr.Error()
		} else if !ok {
			errMsg = fmt.Sprintf("HTTP %d（上游 %s，用户 %s）", st, base, strings.TrimSpace(user))
		}
		p := starrocksWebPanel{
			TabKey:      d.TabKey,
			TabLabel:    d.TabLabel,
			Title:       d.Title,
			URL:         pageURL,
			HTML:        body,
			Body:        body,
			OK:          ok,
			StatusCode:  st,
			ContentType: ct,
			Error:       errMsg,
		}
		panels = append(panels, p)
	}
	if len(panels) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "未匹配到可用 tab"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"base_url": base,
		"username": strings.TrimSpace(user),
		"panels":   panels,
	}})
}

// resolveAuth 对齐 Hub：fe_admin_id 取凭据；可选 base_url/be_base_url/host 覆盖实际请求地址。
func (h *StarRocksWebHandler) resolveAuth(req *starrocksWebPageReq) (base, user, pass string, err error) {
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	override := firstNonEmpty(
		strings.TrimSpace(req.BaseURL),
		strings.TrimSpace(req.BeBaseURL),
		strings.TrimSpace(req.Host),
	)

	if id := strings.TrimSpace(req.FeAdminID); id != "" {
		it, e := h.Store.GetFeAdmin(id)
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
		user = strings.TrimSpace(it.Username)
		pass = plain

		if kind == "be" {
			if override == "" {
				return "", "", "", fmt.Errorf("BE 查询请指定 host / base_url")
			}
			b, e := normalizeStarrocksWebBase(override, 8040)
			return b, user, pass, e
		}

		target := override
		if target == "" {
			target = firstFeNode(it.FeNode)
		}
		if target == "" {
			target = strings.TrimSpace(it.Domain)
		}
		port := it.Port
		if port <= 0 {
			port = feWebDefaultPort
		}
		b, e := normalizeStarrocksWebBase(target, port)
		return b, user, pass, e
	}

	if override == "" {
		return "", "", "", fmt.Errorf("请指定 fe_admin_id 或 base_url/host")
	}
	port := req.Port
	if port <= 0 {
		if kind == "be" {
			port = 8040
		} else {
			port = feWebDefaultPort
		}
	}
	b, e := normalizeStarrocksWebBase(override, port)
	if e != nil {
		return "", "", "", e
	}
	user = strings.TrimSpace(req.Username)
	pass = req.Password
	if kind != "be" && user == "" {
		return "", "", "", fmt.Errorf("缺少用户名")
	}
	return b, user, pass, nil
}

// buildFePageURL 对齐 Hub：单 tab 时可带 path；含完整 path+query 的独立路径；否则追加 ?path=。
func buildFePageURL(base, tabPath, subPath string, singleTab bool) string {
	base = strings.TrimRight(base, "/")
	subPath = strings.TrimSpace(subPath)
	if !singleTab || subPath == "" || subPath == "/" {
		return base + tabPath
	}
	// 已是 ?path=...（相对当前 tab）
	if strings.HasPrefix(subPath, "?path=") {
		return base + tabPath + subPath
	}
	if strings.HasPrefix(subPath, "path=") {
		return base + tabPath + "?" + subPath
	}
	// 独立路径且自带 query（如 /query_profile?query_id=xxx）
	if strings.Contains(subPath, "?") && strings.HasPrefix(subPath, "/") {
		return base + subPath
	}
	full := base + tabPath
	sep := "?"
	if strings.Contains(full, "?") {
		sep = "&"
	}
	return full + sep + "path=" + url.QueryEscape(subPath)
}

func firstFeNode(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n' || r == ';'
	}) {
		if s := strings.TrimSpace(part); s != "" {
			return s
		}
	}
	return raw
}

// normalizeStarrocksWebBase 对齐 Hub：规范为 scheme://host[:port]（无 scheme 补 http）。
func normalizeStarrocksWebBase(raw string, defaultPort int) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("empty base url")
	}
	hasScheme := strings.Contains(s, "://")
	if !hasScheme {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host")
	}
	if u.Port() == "" && defaultPort > 0 {
		host := u.Hostname()
		if host != "" {
			u.Host = fmt.Sprintf("%s:%d", host, defaultPort)
		}
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func fetchBasicAuthPageEx(c *gin.Context, pageURL, username, password string) (status int, contentType, body string, err error) {
	var req *http.Request
	var e error
	if c != nil && c.Request != nil {
		req, e = http.NewRequestWithContext(c.Request.Context(), http.MethodGet, pageURL, nil)
	} else {
		req, e = http.NewRequest(http.MethodGet, pageURL, nil)
	}
	if e != nil {
		return 0, "", "", e
	}
	if strings.TrimSpace(username) != "" {
		req.SetBasicAuth(username, password)
	}
	req.Header.Set("User-Agent", "auto-dba/starrocks-web")
	client := &http.Client{
		Timeout:   feWebTimeout,
		Transport: srProxyHTTP.Transport,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if auth := via[0].Header.Get("Authorization"); auth != "" {
				r.Header.Set("Authorization", auth)
			}
			return nil
		},
	}
	resp, e := client.Do(req)
	if e != nil {
		return 0, "", "", e
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, feWebMaxBodyBytes))
	if e != nil {
		return resp.StatusCode, "", "", e
	}
	ct := resp.Header.Get("Content-Type")
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	text := string(raw)
	if resp.StatusCode >= 400 {
		return resp.StatusCode, ct, text, fmt.Errorf("HTTP %d（%s）", resp.StatusCode, pageURL)
	}
	return resp.StatusCode, ct, text, nil
}
