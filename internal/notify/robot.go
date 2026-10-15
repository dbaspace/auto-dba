package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

func sanitizeRobotURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.Trim(u, "`\"'")
	u = strings.TrimSpace(u)
	return u
}

func robotURLFingerprint(u string) string {
	u = sanitizeRobotURL(u)
	if u == "" {
		return ""
	}
	// 仅展示域名 + hook 后缀，便于核对是否为当前配置
	const marker = "/hook/"
	if i := strings.Index(u, marker); i >= 0 {
		tok := u[i+len(marker):]
		if len(tok) > 8 {
			tok = "…" + tok[len(tok)-8:]
		}
		host := u
		if j := strings.Index(u, "://"); j >= 0 {
			host = u[j+3:]
			if k := strings.Index(host, "/"); k >= 0 {
				host = host[:k]
			}
		}
		return host + marker + tok
	}
	if len(u) > 48 {
		return u[:24] + "…" + u[len(u)-8:]
	}
	return u
}

func routeForIndex(routes []store.RobotRoute, index int) string {
	for _, r := range routes {
		if r.Index == index {
			return store.NormalizeRobotFor(r.For)
		}
	}
	return "alert"
}

func robotMatchesFor(routeFor, want string) bool {
	want = store.NormalizeRobotFor(want)
	rf := store.NormalizeRobotFor(routeFor)
	if rf == "both" {
		return true
	}
	return rf == want
}

func sendRobots(cfg config.NotifyModuleConfig, msg Message) error {
	type target struct {
		kind string
		url  string
	}
	var targets []target
	wantKind := strings.ToLower(strings.TrimSpace(msg.RobotKind))
	wantFor := store.NormalizeRobotFor(msg.RobotFor)

	for i, r := range cfg.Robots {
		url := sanitizeRobotURL(r.URL)
		if url == "" {
			continue
		}
		if msg.RobotIndex != nil && *msg.RobotIndex != i {
			continue
		}
		// 指定下标（页面「测告警/测工单」）时跳过方向过滤，直接打该 webhook
		if msg.RobotIndex == nil && !robotMatchesFor(routeForIndex(msg.RobotRoutes, i), wantFor) {
			continue
		}
		kind := normalizeRobotKind(r.Kind, url)
		if wantKind != "" && kind != wantKind {
			continue
		}
		targets = append(targets, target{kind: kind, url: url})
	}
	// 库内旧 webhook_url：仅告警方向、且未指定单一 index 时附加
	if wantFor == "alert" && msg.RobotIndex == nil {
		for _, u := range msg.RobotURLs {
			u = sanitizeRobotURL(u)
			if u == "" {
				continue
			}
			kind := normalizeRobotKind(wantKind, u)
			if wantKind != "" && kind != wantKind && wantKind != "webhook" {
				kind = wantKind
			}
			targets = append(targets, target{kind: kind, url: u})
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("没有匹配的群机器人（kind=%s for=%s）；请检查 Nacos notify.robots 与页面方向", wantKind, wantFor)
	}
	var errs []string
	for _, t := range targets {
		if err := sendOneRobot(t.kind, t.url, msg); err != nil {
			errs = append(errs, fmt.Sprintf("%s(%s): %v", t.kind, robotURLFingerprint(t.url), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func normalizeRobotKind(kind, url string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	k = strings.TrimPrefix(k, "robot/")
	switch k {
	case "lark", "feishu", "wechat", "wecom", "dingtalk", "dingding":
		if k == "feishu" {
			return "lark"
		}
		if k == "wecom" {
			return "wechat"
		}
		if k == "dingding" {
			return "dingtalk"
		}
		return k
	}
	u := strings.ToLower(url)
	switch {
	case strings.Contains(u, "qyapi.weixin.qq.com"):
		return "wechat"
	case strings.Contains(u, "open.feishu.cn") || strings.Contains(u, "open.larksuite.com"):
		return "lark"
	case strings.Contains(u, "oapi.dingtalk.com"):
		return "dingtalk"
	default:
		return "wechat"
	}
}

func sendOneRobot(kind, webhookURL string, msg Message) error {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = strings.TrimSpace(msg.Markdown)
	}
	md := strings.TrimSpace(msg.Markdown)
	if md == "" {
		md = text
	}
	title := strings.TrimSpace(msg.Title)
	if title == "" {
		title = "KeePlus"
	}
	switch kind {
	case "lark":
		payload := map[string]any{
			"msg_type": "text",
			"content": map[string]string{
				"text": fmt.Sprintf("【%s】\n%s", title, text),
			},
		}
		return postJSONWebhook(webhookURL, payload)
	case "dingtalk":
		payload := map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": title,
				"text":  md,
			},
		}
		return postJSONWebhook(webhookURL, payload)
	default: // wechat
		payload := map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": md,
			},
		}
		return postJSONWebhook(webhookURL, payload)
	}
}

func postJSONWebhook(webhookURL string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d body=%s", resp.StatusCode, string(body))
	}
	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
	}
	if json.Unmarshal(body, &out) == nil {
		if out.ErrCode != 0 {
			return fmt.Errorf("errcode=%d errmsg=%s", out.ErrCode, out.ErrMsg)
		}
		if out.Code != 0 {
			return fmt.Errorf("code=%d msg=%s", out.Code, out.Msg)
		}
	}
	return nil
}
