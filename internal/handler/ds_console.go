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
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

const (
	dsSessionTTL   = 20 * time.Minute
	dsProxyTimeout = 30 * time.Second
	dsProxyPrefix  = "/api/v1/ds-proxy/"
	dsBodyLimit    = 32 << 20
)

type dsCachedSession struct {
	SessionID string
	LoginURL  string
	At        time.Time
}

var (
	dsSessions sync.Map
	dsHTTP     = &http.Client{
		Timeout: dsProxyTimeout,
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

type dsLoginResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		SessionID  string `json:"sessionId"`
		SessionID2 string `json:"sessionid"`
	} `json:"data"`
}

func normalizeDsLoginURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	return raw
}

func dsProxyEnvPrefix(env string) string {
	return dsProxyPrefix + url.PathEscape(strings.TrimSpace(env))
}

func dsLogin(loginURL, username, password string) (string, error) {
	form := url.Values{}
	form.Set("userName", username)
	form.Set("userPassword", password)
	req, err := http.NewRequest(http.MethodPost, normalizeDsLoginURL(loginURL)+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := dsHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var parsed dsLoginResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("DS 登录返回非 JSON")
	}
	sid := parsed.Data.SessionID
	if sid == "" {
		sid = parsed.Data.SessionID2
	}
	if parsed.Code != 0 || sid == "" {
		msg := parsed.Msg
		if msg == "" {
			msg = "DS 登录失败"
		}
		return "", fmt.Errorf("%s", msg)
	}
	return sid, nil
}

func dsFetchUserInfo(loginURL, sessionID string) map[string]any {
	req, err := http.NewRequest(http.MethodGet, normalizeDsLoginURL(loginURL)+"/users/get-user-info", nil)
	if err != nil {
		return map[string]any{}
	}
	req.Header.Set("sessionId", sessionID)
	req.Header.Set("Accept", "application/json")
	resp, err := dsHTTP.Do(req)
	if err != nil {
		return map[string]any{}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &parsed) != nil || parsed.Code != 0 || parsed.Data == nil {
		return map[string]any{}
	}
	return parsed.Data
}

func (h *DsAdminHandler) loadPlainCredential(env string) (*store.DsAdmin, string, error) {
	it, err := h.Store.GetDsAdminByEnv(env)
	if err != nil {
		return nil, "", err
	}
	if it == nil {
		return nil, "", fmt.Errorf("该环境未配置海豚调度")
	}
	plain, err := secret.Decrypt(h.Secret, it.PasswordEnc)
	if err != nil {
		return nil, "", fmt.Errorf("解密失败")
	}
	return it, plain, nil
}

func (h *DsAdminHandler) ensureDSSession(env string) (*dsCachedSession, *store.DsAdmin, error) {
	it, plain, err := h.loadPlainCredential(env)
	if err != nil {
		return nil, nil, err
	}
	loginURL := normalizeDsLoginURL(it.LoginURL)
	envKey := strings.TrimSpace(it.Environment)
	if envKey == "" {
		envKey = strings.TrimSpace(env)
	}
	if cached, ok := dsSessions.Load(envKey); ok {
		s := cached.(*dsCachedSession)
		if s.SessionID != "" && s.LoginURL == loginURL && time.Since(s.At) < dsSessionTTL {
			return s, it, nil
		}
	}
	sid, err := dsLogin(loginURL, it.Username, plain)
	if err != nil {
		return nil, it, fmt.Errorf("DolphinScheduler 登录失败: %w", err)
	}
	next := &dsCachedSession{SessionID: sid, LoginURL: loginURL, At: time.Now()}
	dsSessions.Store(envKey, next)
	return next, it, nil
}

// Warm pre-establishes DS session so iframe bootstrap skips cold login.
func (h *DsAdminHandler) Warm(c *gin.Context) {
	env := strings.TrimSpace(c.Query("env"))
	if env == "" {
		env = strings.TrimSpace(c.Query("environment"))
	}
	if env == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请指定 env"})
		return
	}
	start := time.Now()
	_, _, err := h.ensureDSSession(env)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		log.Printf("ds_warm env=%s err=%v elapsed_ms=%d", env, err, elapsed)
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error(), "elapsed_ms": elapsed})
		return
	}
	log.Printf("ds_warm env=%s ok elapsed_ms=%d", env, elapsed)
	c.JSON(http.StatusOK, gin.H{"ok": true, "elapsed_ms": elapsed})
}

// ConsoleBootstrap returns HTML that seeds session into localStorage and redirects into same-origin proxy.
func (h *DsAdminHandler) ConsoleBootstrap(c *gin.Context) {
	start := time.Now()
	env := strings.TrimSpace(c.Param("env"))
	if env == "" {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:24px;color:#c00">缺少环境参数</body></html>`)
		return
	}
	sessStart := time.Now()
	sess, it, err := h.ensureDSSession(env)
	sessMs := time.Since(sessStart).Milliseconds()
	if err != nil {
		fallback := ""
		if it != nil {
			fallback = normalizeDsLoginURL(it.LoginURL)
		}
		log.Printf("ds_console env=%s login_err=%v sess_ms=%d total_ms=%d", env, err, sessMs, time.Since(start).Milliseconds())
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:24px">
<p style="color:#c00">无法代登 DolphinScheduler：%s</p>
%s
</body></html>`, html.EscapeString(err.Error()), dsFallbackLinks(fallback))
		return
	}
	infoStart := time.Now()
	userInfo := dsFetchUserInfo(sess.LoginURL, sess.SessionID)
	infoMs := time.Since(infoStart).Milliseconds()
	if len(userInfo) == 0 && it != nil {
		userInfo = map[string]any{"userName": it.Username}
	}
	state, _ := json.Marshal(map[string]any{
		"sessionId":          sess.SessionID,
		"securityConfigType": "",
		"baseResDir":         "",
		"baseUdfDir":         "",
		"userInfo":           userInfo,
	})
	proxyHome := dsProxyEnvPrefix(env) + "/ui/home"
	openURL := sess.LoginURL
	log.Printf("ds_console env=%s sess_ms=%d userinfo_ms=%d total_ms=%d", env, sessMs, infoMs, time.Since(start).Milliseconds())
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.SetCookie("sessionId", sess.SessionID, 0, dsProxyEnvPrefix(env), "", false, false)
	c.String(http.StatusOK, `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>DolphinScheduler</title></head>
<body style="font-family:sans-serif;padding:24px;color:#666">
正在打开 DolphinScheduler 控制台…
<script>
(function () {
  try { localStorage.setItem('user', JSON.stringify(%s)); } catch (e) {}
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
</body></html>`, string(state), proxyHome, openURL, dsFallbackLinks(openURL))
}

func dsFallbackLinks(loginURL string) string {
	if strings.TrimSpace(loginURL) == "" {
		return `<p>请先到配置中心「海豚配置」补全登录地址。</p>`
	}
	u := html.EscapeString(loginURL)
	return fmt.Sprintf(`<p><a href="%s" target="_blank" rel="noreferrer">新窗口打开登录页</a></p>`, u)
}

func shouldRewriteDSBody(ctype, pathname string) bool {
	if strings.Contains(ctype, "text/html") || strings.Contains(ctype, "text/css") ||
		strings.Contains(ctype, "javascript") || strings.Contains(ctype, "application/json") {
		return true
	}
	return strings.HasSuffix(pathname, ".js") || strings.HasSuffix(pathname, ".css") ||
		strings.HasSuffix(pathname, ".html") || strings.HasSuffix(pathname, ".json") ||
		strings.HasSuffix(pathname, ".map")
}

func dsUpstreamContextPath(loginURL string) string {
	u, err := url.Parse(normalizeDsLoginURL(loginURL))
	if err != nil {
		return "/dolphinscheduler"
	}
	p := strings.TrimSuffix(u.Path, "/")
	if p == "" || p == "/" {
		return ""
	}
	return p
}

const dsEmbedChrome = `<style id="auto-dba-ds-embed">
/* 嵌入运维页：铺满 iframe 视口 */
html, body {
  width: 100% !important;
  height: 100% !important;
  margin: 0 !important;
  padding: 0 !important;
  overflow: hidden !important;
  max-width: none !important;
}
#app, .n-config-provider,
.n-layout, .n-layout-scroll-container,
.n-layout-content {
  width: 100% !important;
  height: 100% !important;
  max-width: none !important;
  box-sizing: border-box !important;
}
.n-layout-content { overflow: auto !important; }
.n-layout-content .n-grid,
.n-layout-content .n-grid-item,
.n-layout-content .n-gi { max-width: none !important; }
/* 首页统计卡：去掉上游 min-height:820，收紧留白并左对齐 */
body .n-layout-content {
  padding: 8px !important;
  margin: 0 !important;
}
body .n-layout-content .n-card {
  width: 100% !important;
  box-sizing: border-box !important;
  min-height: 0 !important;
  height: auto !important;
  overflow: visible !important;
}
body .n-layout-content .n-card .n-card__content {
  padding: 4px 8px 12px !important;
  overflow: visible !important;
}
body .n-layout-content .n-card .n-card-header {
  padding-top: 8px !important;
  padding-bottom: 8px !important;
}

/* 侧栏 / 折叠箭头 / 汉堡占位 / 统一隐藏类 — 用 body 提高优先级 */
body .n-layout-sider,
body .n-layout-toggle-bar,
body .n-layout-toggle-bar *,
body .n-layout-toggle-button,
body .n-layout-toggle-bar__top,
body .n-layout-toggle-bar__bottom,
body .n-layout-sider-toggle-button,
body [class*="layout-toggle"],
body [class*="toggle-bar"],
body [class*="toggle-button"],
body .n-layout-scroll-container > .n-layout-sider,
body .n-layout-header [class*="fold"],
body .n-layout-header [class*="collapsed"],
body .n-layout-header [class*="menu-fold"],
body .n-layout-header [class*="MenuFold"],
body .n-layout-header [class*="hamburger"],
body .auto-dba-hide-ds-user {
  display: none !important;
  visibility: hidden !important;
  width: 0 !important;
  min-width: 0 !important;
  max-width: 0 !important;
  height: 0 !important;
  margin: 0 !important;
  padding: 0 !important;
  overflow: hidden !important;
  pointer-events: none !important;
  border: none !important;
  opacity: 0 !important;
  flex: 0 0 0 !important;
}

/* User(admin)：settings 末项；环境注入后占其位 */
.n-layout-header [class*="settings"] > *:last-child:not(#auto-dba-ds-env),
.n-layout-header .n-dropdown:has(.n-avatar) {
  display: none !important;
  visibility: hidden !important;
  width: 0 !important;
  min-width: 0 !important;
  max-width: 0 !important;
  margin: 0 !important;
  padding: 0 !important;
  overflow: hidden !important;
  pointer-events: none !important;
}

/* 强制顶栏 40px，贴左（去掉侧栏预留空） */
body .n-layout-header {
  width: 100% !important;
  height: 40px !important;
  max-height: 40px !important;
  min-height: 40px !important;
  line-height: 40px !important;
  padding: 0 8px !important;
  margin: 0 !important;
  overflow: hidden !important;
  display: flex !important;
  flex-direction: row !important;
  align-items: center !important;
  box-sizing: border-box !important;
}
body .n-layout-header > *,
body .n-layout-header [class*="container"] {
  height: 40px !important;
  max-height: 40px !important;
  min-height: 0 !important;
  margin-left: 0 !important;
  padding-left: 0 !important;
  display: flex !important;
  flex-direction: row !important;
  align-items: center !important;
  box-sizing: border-box !important;
}
body .n-layout-header [class*="logo"] {
  margin-left: 0 !important;
  padding-left: 0 !important;
  margin-right: 8px !important;
}
body .n-layout-header [class*="settings"] {
  align-items: center !important;
  height: 40px !important;
}
body .n-layout-header .n-menu,
body .n-layout-header .n-menu-item,
body .n-layout-header .n-button {
  height: 28px !important;
  line-height: 28px !important;
}
/* 仅钉 header 下方内容区；勿钉包住 header 的外层 absolute（否则整页下移出现顶白缝） */
body .n-layout-header + .n-layout,
body .n-layout-header ~ .n-layout--absolute-positioned,
body .n-layout-header ~ .n-layout[class*="absolute"] {
  top: var(--auto-dba-ds-header-h, 40px) !important;
  left: 0 !important;
  margin-left: 0 !important;
  padding-left: 0 !important;
}
body .n-layout-scroll-container {
  margin-left: 0 !important;
  padding-left: 0 !important;
}

/* 注入的环境：与时区同一 flex 行 */
#auto-dba-ds-env {
  display: inline-flex !important;
  align-items: center !important;
  gap: 6px !important;
  height: 32px !important;
  margin: 0 0 0 8px !important;
  padding: 0 !important;
  flex-shrink: 0 !important;
  vertical-align: middle !important;
  box-sizing: border-box !important;
}
#auto-dba-ds-env .auto-dba-ds-env-label {
  color: rgba(0, 0, 0, 0.55);
  font-size: 14px;
  white-space: nowrap;
  line-height: 32px;
}
#auto-dba-ds-env select {
  height: 28px;
  min-width: 96px;
  max-width: 140px;
  padding: 0 8px;
  border: 1px solid rgba(0, 0, 0, 0.12);
  border-radius: 3px;
  background: #fff;
  font-size: 13px;
  color: rgba(0, 0, 0, 0.85);
  outline: none;
  cursor: pointer;
  box-sizing: border-box;
}

/* 账号水印 */
[class*="watermark"],
[class*="WaterMark"],
[class*="water-mark"],
.n-watermark {
  display: none !important;
  visibility: hidden !important;
  opacity: 0 !important;
  pointer-events: none !important;
}
</style>
<script id="auto-dba-ds-embed-js">
(function () {
  var ENV_STATE_KEY = 'auto-dba-ds-env-state';
  var ENV_PICK_KEY = 'auto-dba-ds-env-pick';
  var mounting = false;
  var cachedState = null;

  function markHide(el) {
    if (!el || !el.classList) return;
    el.classList.add('auto-dba-hide-ds-user');
    el.style.setProperty('display', 'none', 'important');
  }
  function isSettingsChrome(text) {
    var t = String(text || '').replace(/\s+/g, ' ').trim();
    if (!t) return true;
    if (/Asia\/|UTC|GMT|Europe\/|America\//i.test(t)) return true;
    if (/中文|English|简体|繁體|繁体/i.test(t)) return true;
    if (/深色|浅色|Light|Dark|跟随系统/i.test(t)) return true;
    if (/界面设置|UI\s*Setting/i.test(t)) return true;
    return false;
  }
  function isToggleText(t) {
    return t === '>' || t === '<' || t === '›' || t === '‹' || t === '»' || t === '«';
  }
  function findSettings(header) {
    return header.querySelector('[class*="settings"]');
  }
  function findTimezoneEl(settings) {
    if (!settings) return null;
    var found = null;
    var kids = settings.children;
    for (var i = 0; i < kids.length; i++) {
      var el = kids[i];
      if (el.id === 'auto-dba-ds-env') continue;
      if (/Asia\/|UTC|GMT|Europe\/|America\//i.test(el.textContent || '')) found = el;
    }
    if (found) return found;
    settings.querySelectorAll('.n-dropdown').forEach(function (d) {
      if (/Asia\/|UTC|GMT|Europe\/|America\//i.test(d.textContent || '')) found = d;
    });
    return found;
  }
  function findUserEl(settings) {
    if (!settings) return null;
    var hits = [];
    var kids = settings.children;
    for (var i = 0; i < kids.length; i++) {
      var el = kids[i];
      if (el.id === 'auto-dba-ds-env') continue;
      var t = (el.textContent || '').replace(/\s+/g, ' ').trim();
      if (isSettingsChrome(t)) continue;
      hits.push(el);
    }
    return hits.length ? hits[hits.length - 1] : null;
  }
  function readEnvState() {
    if (cachedState && cachedState.options && cachedState.options.length) return cachedState;
    try {
      var raw = localStorage.getItem(ENV_STATE_KEY);
      if (!raw) return null;
      return JSON.parse(raw);
    } catch (e) {
      return null;
    }
  }
  function notifyEnvPick(env) {
    try { localStorage.setItem(ENV_PICK_KEY, env); } catch (e) {}
    try {
      if (window.parent && window.parent !== window) {
        window.parent.postMessage({ type: 'auto-dba-ds-env', env: env }, '*');
      }
    } catch (e) {}
  }
  function ensureBox() {
    var box = document.getElementById('auto-dba-ds-env');
    if (box) return box;
    box = document.createElement('div');
    box.id = 'auto-dba-ds-env';
    var label = document.createElement('span');
    label.className = 'auto-dba-ds-env-label';
    label.textContent = '环境';
    var sel = document.createElement('select');
    sel.setAttribute('aria-label', '环境');
    box.appendChild(label);
    box.appendChild(sel);
    sel.addEventListener('change', function () {
      notifyEnvPick(sel.value);
    });
    return box;
  }
  function fillSelect(box, state) {
    var sel = box.querySelector('select');
    if (!sel) return;
    var cur = String(state.env || '');
    var opts = state.options || [];
    var html = '';
    for (var i = 0; i < opts.length; i++) {
      var v = String(opts[i].value || opts[i].label || '');
      if (!v) continue;
      var lab = String(opts[i].label || v);
      html += '<option value="' + v.replace(/"/g, '&quot;') + '"' + (v === cur ? ' selected' : '') + '>' + lab.replace(/</g, '&lt;') + '</option>';
    }
    if (sel.innerHTML !== html) sel.innerHTML = html;
    else if (cur && sel.value !== cur) sel.value = cur;
  }
  function mountEnvControl() {
    if (mounting) return;
    var header = document.querySelector('.n-layout-header');
    if (!header) return;
    var state = readEnvState();
    if (!state || !state.options || !state.options.length) return;
    var settings = findSettings(header);
    if (!settings) return;

    mounting = true;
    try {
      var box = ensureBox();
      fillSelect(box, state);
      var tz = findTimezoneEl(settings);
      var user = findUserEl(settings);

      if (user && user.parentElement === settings) {
        if (box.parentElement !== settings || box.nextSibling !== user) {
          settings.insertBefore(box, user);
        }
      } else if (tz && tz.parentElement === settings) {
        if (box.parentElement !== settings || box.previousSibling !== tz) {
          if (tz.nextSibling) settings.insertBefore(box, tz.nextSibling);
          else settings.appendChild(box);
        }
      } else if (box.parentElement !== settings) {
        settings.appendChild(box);
      }

      if (user) markHide(user);
    } finally {
      mounting = false;
    }
  }
  function hideUser() {
    var header = document.querySelector('.n-layout-header');
    if (!header) return;
    var settings = findSettings(header);
    if (settings) {
      var user = findUserEl(settings);
      if (user) markHide(user);
      var last = settings.lastElementChild;
      if (last && last.id !== 'auto-dba-ds-env') {
        var t = (last.textContent || '').replace(/\s+/g, ' ').trim();
        if (!isSettingsChrome(t)) markHide(last);
      }
    }
    header.querySelectorAll('.n-dropdown').forEach(function (d) {
      var t = (d.textContent || '').replace(/\s+/g, ' ').trim();
      if (isSettingsChrome(t)) return;
      markHide(d);
    });
    header.querySelectorAll('button, .n-button, [role="button"]').forEach(function (btn) {
      var t = (btn.textContent || '').replace(/\s+/g, '');
      if (isToggleText(t)) markHide(btn);
    });
  }
  function removeEl(el) {
    if (!el || !el.parentNode) return;
    try { el.parentNode.removeChild(el); } catch (e) { markHide(el); }
  }
  function isProtectedChrome(el) {
    if (!el) return true;
    if (el.id === 'auto-dba-ds-env' || (el.closest && el.closest('#auto-dba-ds-env'))) return true;
    if (el.closest && el.closest('.n-layout-header [class*="settings"]')) return true;
    if (el.closest && el.closest('.n-layout-header .n-menu')) return true;
    if (el.closest && el.closest('.n-layout-header [class*="logo"]')) return true;
    return false;
  }
  function hideSider() {
    document.querySelectorAll(
      '.n-layout-sider, .n-layout-toggle-bar, .n-layout-toggle-button, .n-layout-toggle-bar__top, .n-layout-toggle-bar__bottom, .n-layout-sider-toggle-button, [class*="layout-toggle"], [class*="toggle-bar"], [class*="toggle-button"]'
    ).forEach(removeEl);

    var header = document.querySelector('.n-layout-header');
    var headerBottom = 48;
    if (header) {
      var hb = header.getBoundingClientRect();
      if (hb && hb.bottom) headerBottom = hb.bottom + 8;

      /* 顶栏左侧汉堡/折叠按钮（Logo 左侧或紧邻） */
      header.querySelectorAll('button, .n-button, [role="button"]').forEach(function (btn) {
        if (!btn || !btn.parentNode) return;
        if (btn.closest && (btn.closest('[class*="settings"]') || btn.closest('.n-menu') || btn.closest('#auto-dba-ds-env'))) return;
        var cls = String(btn.className || '');
        var t = (btn.textContent || '').replace(/\s+/g, '');
        var rect = btn.getBoundingClientRect ? btn.getBoundingClientRect() : null;
        if (/fold|collapsed|hamburger|menu-fold|MenuFold/i.test(cls)) {
          removeEl(btn);
          return;
        }
        if (!rect || rect.left > 80 || rect.top > headerBottom) return;
        if (rect.width > 48 || rect.height > 48) return;
        /* 纯图标按钮（无「界面设置」等文案） */
        if (t.length === 0 || t.length <= 1) removeEl(btn);
      });

      /* 清掉 header 子树过大的左缩进 */
      header.querySelectorAll('*').forEach(function (el) {
        if (!el || !el.style) return;
        if (el.closest && (el.closest('.n-menu') || el.closest('[class*="settings"]') || el.closest('#auto-dba-ds-env'))) return;
        var computed = window.getComputedStyle ? window.getComputedStyle(el) : null;
        if (!computed) return;
        var ml = parseFloat(computed.marginLeft);
        var pl = parseFloat(computed.paddingLeft);
        if (!isNaN(ml) && ml > 24) el.style.setProperty('margin-left', '0', 'important');
        if (!isNaN(pl) && pl > 24) el.style.setProperty('padding-left', '0', 'important');
      });
      header.style.setProperty('padding-left', '8px', 'important');
      header.style.setProperty('margin-left', '0', 'important');
    }

    /* 左上角窄条/箭头（含无文本 ToggleBar）：物理删掉 */
    document.querySelectorAll('div, button, span, a, i, svg').forEach(function (el) {
      if (!el || !el.parentNode) return;
      if (isProtectedChrome(el)) return;
      var rect = el.getBoundingClientRect ? el.getBoundingClientRect() : null;
      if (!rect || rect.width < 2 || rect.height < 2) return;
      if (rect.left >= 28 || rect.top > headerBottom) return;
      if (rect.width > 36 || rect.height > 96) return;
      var cls = String(el.className || '');
      var t = (el.textContent || '').replace(/\s+/g, '');
      if (/toggle|sider|fold|hamburger/i.test(cls) || isToggleText(t) || t.length <= 2) {
        removeEl(el);
      }
    });

    shrinkHeader();
  }
  function shrinkHeader() {
    var header = document.querySelector('.n-layout-header');
    var topPx = '40px';
    if (header) {
      header.style.setProperty('height', '40px', 'important');
      header.style.setProperty('max-height', '40px', 'important');
      header.style.setProperty('min-height', '40px', 'important');
      header.style.setProperty('padding-top', '0', 'important');
      header.style.setProperty('padding-bottom', '0', 'important');
      header.style.setProperty('padding-left', '8px', 'important');
      header.style.setProperty('margin-left', '0', 'important');
      header.style.setProperty('overflow', 'hidden', 'important');
      header.style.setProperty('display', 'flex', 'important');
      header.style.setProperty('align-items', 'center', 'important');
      header.style.setProperty('flex-direction', 'row', 'important');
      var kids = header.querySelectorAll(':scope > *, [class*="container"]');
      for (var i = 0; i < kids.length; i++) {
        kids[i].style.setProperty('height', '40px', 'important');
        kids[i].style.setProperty('max-height', '40px', 'important');
        kids[i].style.setProperty('align-items', 'center', 'important');
        kids[i].style.setProperty('margin-left', '0', 'important');
        kids[i].style.setProperty('padding-left', '0', 'important');
      }
      var logo = header.querySelector('[class*="logo"]');
      if (logo) {
        logo.style.setProperty('margin-left', '0', 'important');
        logo.style.setProperty('padding-left', '0', 'important');
      }
      var hb = header.getBoundingClientRect();
      /* 相对定位父级：用 header 高度，勿用 viewport bottom（外层 layout top:0 会被误钉） */
      if (hb && hb.height > 0) topPx = Math.round(hb.height) + 'px';
    }
    try {
      document.documentElement.style.setProperty('--auto-dba-ds-header-h', topPx);
    } catch (e) {}
    function pinContent(el) {
      if (!el) return;
      if (el === header || (header && el.contains && el.contains(header))) return;
      if (el.classList && el.classList.contains('n-layout-header')) return;
      el.style.setProperty('top', topPx, 'important');
      el.style.setProperty('left', '0px', 'important');
      el.style.setProperty('margin-left', '0', 'important');
      var computed = window.getComputedStyle ? window.getComputedStyle(el) : null;
      if (computed) {
        var pl = parseFloat(computed.paddingLeft);
        if (!isNaN(pl) && pl > 24) el.style.setProperty('padding-left', '0', 'important');
      }
    }
    /* 只钉 header 后的内容兄弟，不钉包住 header 的外层 absolute */
    if (header) {
      var sib = header.nextElementSibling;
      while (sib) {
        if (sib.classList && (sib.classList.contains('n-layout') || String(sib.className || '').indexOf('n-layout') >= 0)) {
          pinContent(sib);
        }
        sib = sib.nextElementSibling;
      }
    }
    document.querySelectorAll('.n-layout--absolute-positioned, .n-layout').forEach(function (el) {
      if (el === header || (header && el.contains && el.contains(header))) return;
      var computed = window.getComputedStyle ? window.getComputedStyle(el) : null;
      if (!computed || computed.position !== 'absolute') return;
      var topVal = parseFloat(computed.top);
      /* 仅纠正 Vue 写回的 48–80px 顶栏留位；top≈0 的外壳不动 */
      if (!isNaN(topVal) && topVal >= 48 && topVal <= 80) pinContent(el);
    });
    document.querySelectorAll('.n-layout-content').forEach(function (el) {
      el.style.setProperty('padding', '8px', 'important');
      el.style.setProperty('margin-left', '0', 'important');
    });
  }
  function hideWatermark() {
    document.querySelectorAll(
      '[class*="watermark"], [class*="WaterMark"], [class*="water-mark"], .n-watermark'
    ).forEach(function (el) {
      markHide(el);
      if (el.querySelectorAll) el.querySelectorAll('canvas').forEach(removeEl);
    });
    document.querySelectorAll('div, section, aside').forEach(function (el) {
      if (!el || isProtectedChrome(el)) return;
      var computed = window.getComputedStyle ? window.getComputedStyle(el) : null;
      if (!computed) return;
      var pos = computed.position;
      if (pos !== 'fixed' && pos !== 'absolute') return;
      if (computed.pointerEvents !== 'none') return;
      var op = parseFloat(computed.opacity);
      var tf = String(computed.transform || '');
      var rotated = /matrix|rotate/i.test(tf) && tf !== 'none';
      var lowOp = !isNaN(op) && op > 0 && op < 0.35;
      var covers = false;
      try {
        var r = el.getBoundingClientRect();
        covers = r.width > window.innerWidth * 0.4 && r.height > window.innerHeight * 0.4;
      } catch (e) {}
      if (covers && (rotated || lowOp || el.querySelector('canvas'))) {
        el.querySelectorAll('canvas').forEach(removeEl);
        removeEl(el);
        return;
      }
      var st = el.getAttribute('style') || '';
      if (/pointer-events\s*:\s*none/i.test(st) && /position\s*:\s*fixed/i.test(st) && /inset|top\s*:\s*0/i.test(st)) {
        el.querySelectorAll('canvas').forEach(removeEl);
        removeEl(el);
      }
    });
  }
  function stripStrayBodyText() {
    var body = document.body;
    if (!body) return;
    var n = body.firstChild;
    while (n) {
      var next = n.nextSibling;
      if (n.nodeType === 3) {
        var t = String(n.nodeValue || '');
        if (/^[>\s"']*$/.test(t)) {
          try { body.removeChild(n); } catch (e) {}
        }
      }
      n = next;
      /* 只清开头连续垃圾文本，遇到元素就停 */
      if (n && n.nodeType === 1) break;
    }
  }
  /* 仅柱状图：保证高度与 grid.bottom，避免 X 轴任务名被裁切；不改饼图 */
  function fixBarChartLabels() {
    if (!window.echarts || typeof window.echarts.getInstanceByDom !== 'function') return;
    document.querySelectorAll('[_echarts_instance_]').forEach(function (el) {
      try {
        var inst = window.echarts.getInstanceByDom(el);
        if (!inst || typeof inst.getOption !== 'function') return;
        var opt = inst.getOption() || {};
        var series = opt.series || [];
        var isBar = false;
        for (var i = 0; i < series.length; i++) {
          if (series[i] && series[i].type === 'bar') { isBar = true; break; }
        }
        if (!isBar) return;
        var h = el.clientHeight || 0;
        if (h < 400) el.style.setProperty('height', '400px', 'important');
        el.style.setProperty('overflow', 'visible', 'important');
        el.style.setProperty('min-height', '400px', 'important');
        var child = el.firstElementChild;
        if (child && child.style) {
          child.style.setProperty('overflow', 'visible', 'important');
          if (h < 400) child.style.setProperty('height', '400px', 'important');
        }
        var gridPatch = { bottom: 88 };
        var xAxisPatch = { axisLabel: { interval: 0, hideOverlap: false } };
        inst.setOption({ grid: gridPatch, xAxis: xAxisPatch }, false);
        if (typeof inst.resize === 'function') inst.resize();
      } catch (e) {}
    });
  }
  var fittingViewport = false;
  function fitViewport() {
    if (fittingViewport) return;
    fittingViewport = true;
    stripStrayBodyText();
    try {
      var h = window.innerHeight, w = window.innerWidth;
      document.documentElement.style.height = h + 'px';
      document.documentElement.style.width = w + 'px';
      document.documentElement.style.margin = '0';
      document.documentElement.style.padding = '0';
      if (document.body) {
        document.body.style.height = h + 'px';
        document.body.style.width = w + 'px';
        document.body.style.margin = '0';
        document.body.style.padding = '0';
      }
      var app = document.getElementById('app');
      if (app) {
        app.style.height = h + 'px';
        app.style.width = w + 'px';
        app.style.margin = '0';
        app.style.padding = '0';
      }
      fixBarChartLabels();
    } catch (e) {}
    fittingViewport = false;
  }
  function tick() {
    stripStrayBodyText();
    hideUser();
    hideSider();
    hideWatermark();
    mountEnvControl();
    fixBarChartLabels();
  }
  var timer = 0;
  function schedule() {
    if (timer) return;
    timer = setTimeout(function () { timer = 0; tick(); }, 40);
  }
  var stylePinTimer = 0;
  function schedulePin() {
    if (stylePinTimer) return;
    stylePinTimer = setTimeout(function () { stylePinTimer = 0; shrinkHeader(); hideSider(); }, 0);
  }
  tick();
  fitViewport();
  document.addEventListener('DOMContentLoaded', function () { tick(); fitViewport(); });
  window.addEventListener('resize', function () { fitViewport(); shrinkHeader(); });
  window.addEventListener('storage', function (e) {
    if (e.key === ENV_STATE_KEY) {
      cachedState = null;
      mountEnvControl();
    }
  });
  window.addEventListener('message', function (e) {
    var d = e && e.data;
    if (!d || d.type !== 'auto-dba-ds-env-state') return;
    if (!d.options || !d.options.length) return;
    cachedState = { env: d.env || '', options: d.options };
    try { localStorage.setItem(ENV_STATE_KEY, JSON.stringify(cachedState)); } catch (err) {}
    mountEnvControl();
  });
  try {
    if (window.parent && window.parent !== window) {
      window.parent.postMessage({ type: 'auto-dba-ds-env-ready' }, '*');
    }
  } catch (e) {}
  setTimeout(tick, 200);
  setTimeout(tick, 800);
  setTimeout(tick, 2000);
  setInterval(tick, 250);
  if (document.documentElement) {
    new MutationObserver(schedule).observe(document.documentElement, { childList: true, subtree: true });
    /* Vue 写回 style.top/left 时立刻再钉 */
    new MutationObserver(function (mutations) {
      for (var i = 0; i < mutations.length; i++) {
        var t = mutations[i].target;
        if (!t || !t.getAttribute) continue;
        var st = t.getAttribute('style') || '';
        if (/top\s*:\s*(4[8-9]|[5-7]\d)\s*px/i.test(st) || /left\s*:\s*[1-9]\d*\s*px/i.test(st)) {
          schedulePin();
          return;
        }
      }
    }).observe(document.documentElement, { attributes: true, attributeFilter: ['style'], subtree: true });
  }
})();
</script>`

func rewriteDSBody(raw []byte, ctype, loginURL, proxyBase string) ([]byte, bool) {
	ctxPath := dsUpstreamContextPath(loginURL)
	needCtx := ctxPath != "" && strings.Contains(string(raw), ctxPath)
	needDefault := ctxPath != "/dolphinscheduler" && strings.Contains(string(raw), "/dolphinscheduler")
	isHTML := strings.Contains(ctype, "text/html")
	needCSP := isHTML
	needEmbed := isHTML && !strings.Contains(string(raw), "auto-dba-ds-embed")
	if !needCtx && !needDefault && !needCSP && !needEmbed {
		return raw, false
	}
	text := string(raw)
	if needCtx {
		text = strings.ReplaceAll(text, ctxPath, proxyBase)
	}
	if needDefault {
		text = strings.ReplaceAll(text, "/dolphinscheduler", proxyBase)
	}
	if needCSP {
		// 去掉上游 CSP，避免 inline embed 脚本被拦截
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
		}
		// 在完整 <head...> 开标签之后插入 CSP meta（勿 Replace("<head", ...)，否则会留下多余的 >）
		text = insertAfterHeadOpen(text, `<meta http-equiv="Content-Security-Policy" content="frame-ancestors *">`)
	}
	if needEmbed {
		if strings.Contains(text, "</head>") {
			text = strings.Replace(text, "</head>", dsEmbedChrome+"</head>", 1)
		} else if strings.Contains(text, "<body") {
			text = strings.Replace(text, "<body", dsEmbedChrome+"<body", 1)
		} else {
			text = dsEmbedChrome + text
		}
	}
	return []byte(text), true
}

// insertAfterHeadOpen 在第一个 <head...> 开标签结束后插入 snippet。
func insertAfterHeadOpen(html, snippet string) string {
	low := strings.ToLower(html)
	i := strings.Index(low, "<head")
	if i < 0 {
		return html
	}
	rest := html[i:]
	end := strings.Index(rest, ">")
	if end < 0 {
		return html
	}
	at := i + end + 1
	return html[:at] + snippet + html[at:]
}

func copyDSResponseHeaders(dst http.Header, src http.Header) {
	for k, vs := range src {
		lk := strings.ToLower(k)
		if lk == "content-length" || lk == "transfer-encoding" || lk == "x-frame-options" || lk == "content-security-policy" {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// Proxy reverse-proxies DolphinScheduler UI/API under same origin with sessionId header.
func (h *DsAdminHandler) Proxy(c *gin.Context) {
	start := time.Now()
	env := strings.TrimSpace(c.Param("env"))
	sess, _, err := h.ensureDSSession(env)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusBadGateway, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:24px;color:#c00">%s</body></html>`, html.EscapeString(err.Error()))
		return
	}
	rest := c.Param("filepath")
	if rest == "" {
		rest = "/"
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	target := normalizeDsLoginURL(sess.LoginURL) + rest
	if q := c.Request.URL.RawQuery; q != "" {
		target += "?" + q
	}

	var bodyBytes []byte
	if c.Request.Body != nil && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		bodyBytes, _ = io.ReadAll(io.LimitReader(c.Request.Body, dsBodyLimit))
	}

	var rdr io.Reader
	if bodyBytes != nil {
		rdr = strings.NewReader(string(bodyBytes))
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, rdr)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "DS 代理失败: " + err.Error()})
		return
	}
	if ct := c.GetHeader("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if lang := c.GetHeader("language"); lang != "" {
		req.Header.Set("language", lang)
	} else {
		req.Header.Set("language", "zh_CN")
	}
	req.Header.Set("sessionId", sess.SessionID)
	if acc := c.GetHeader("Accept"); acc != "" {
		req.Header.Set("Accept", acc)
	}

	upStart := time.Now()
	resp, err := dsHTTP.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "DS 代理失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	ctype := resp.Header.Get("Content-Type")
	proxyBase := dsProxyEnvPrefix(env)
	rewrite := shouldRewriteDSBody(ctype, rest)
	upMs := time.Since(upStart).Milliseconds()

	if !rewrite {
		copyDSResponseHeaders(c.Writer.Header(), resp.Header)
		c.Status(resp.StatusCode)
		n, _ := io.Copy(c.Writer, resp.Body)
		log.Printf("ds_proxy env=%s path=%s status=%d stream_bytes=%d up_ms=%d total_ms=%d",
			env, rest, resp.StatusCode, n, upMs, time.Since(start).Milliseconds())
		return
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, dsBodyLimit))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "DS 代理读取失败: " + err.Error()})
		return
	}
	rewritten, did := rewriteDSBody(raw, ctype, sess.LoginURL, proxyBase)
	copyDSResponseHeaders(c.Writer.Header(), resp.Header)
	c.Data(resp.StatusCode, ctype, rewritten)
	log.Printf("ds_proxy env=%s path=%s status=%d bytes=%d rewrite=%v up_ms=%d total_ms=%d",
		env, rest, resp.StatusCode, len(rewritten), did, upMs, time.Since(start).Milliseconds())
}
