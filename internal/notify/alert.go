package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// AlertPayload webhook / IM 网关 JSON（Alertmanager 风格简化字段）。
type AlertPayload struct {
	Event       string            `json:"event"` // trigger | repeat | resolve
	AlertID     string            `json:"alert_id"`
	Fingerprint string            `json:"fingerprint"`
	RuleID      string            `json:"rule_id"`
	RuleName    string            `json:"rule_name"`
	Severity    string            `json:"severity"`
	Title       string            `json:"title"`
	Status      string            `json:"status"`
	Handle      string            `json:"handle"`
	Service     string            `json:"service"`
	Cluster     string            `json:"cluster"`
	CurValue    string            `json:"cur_value"`
	Threshold   string            `json:"threshold"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartedAt   string            `json:"started_at"`
	NotifyCount int               `json:"notify_count"`
	Channels    []string          `json:"channels,omitempty"`
	GroupKey    string            `json:"group_key,omitempty"`
	GroupSize   int               `json:"group_size,omitempty"`
	At          string            `json:"at"`
}

// AlertDispatchOpts 外发选项（路由 / 值班覆盖默认渠道）。
type AlertDispatchOpts struct {
	Channels []string // webhook|wecom|phone|email；空则用全局开关
	ToUsers  []string // 邮件 / 企微接收人（用户名）
	GroupKey string
	GroupSize int
}

// AlertFired 告警触发/续报外发（异步调用方应 go AlertFired）。
func AlertFired(db *store.DB, encKey string, a *store.Alert, rule *store.AlertRule, reason string, opts *AlertDispatchOpts) {
	if db == nil || a == nil {
		return
	}
	event := strings.TrimSpace(reason)
	if event == "" {
		event = "trigger"
	}
	name := ""
	if rule != nil {
		name = rule.Name
	}
	p := AlertPayload{
		Event: event, AlertID: a.ID, Fingerprint: a.Fingerprint, RuleID: a.RuleID, RuleName: name,
		Severity: a.Severity, Title: a.Title, Status: a.Status, Handle: a.Handle,
		Service: a.Service, Cluster: a.Cluster, CurValue: a.CurValue, Threshold: a.Threshold,
		Labels: a.Labels, Annotations: a.Annotations, StartedAt: a.StartedAt,
		NotifyCount: a.NotifyCount, At: time.Now().UTC().Format(time.RFC3339),
	}
	if opts != nil {
		p.Channels = opts.Channels
		p.GroupKey = opts.GroupKey
		p.GroupSize = opts.GroupSize
	}
	dispatchAlert(db, encKey, p, opts)
}

// AlertResolved 告警恢复外发。
func AlertResolved(db *store.DB, encKey string, a *store.Alert, ruleName string, opts *AlertDispatchOpts) {
	if db == nil || a == nil {
		return
	}
	p := AlertPayload{
		Event: "resolve", AlertID: a.ID, Fingerprint: a.Fingerprint, RuleID: a.RuleID, RuleName: ruleName,
		Severity: a.Severity, Title: a.Title, Status: a.Status, Handle: a.Handle,
		Service: a.Service, Cluster: a.Cluster, CurValue: a.CurValue, Threshold: a.Threshold,
		Labels: a.Labels, Annotations: a.Annotations, StartedAt: a.StartedAt,
		NotifyCount: a.NotifyCount, At: time.Now().UTC().Format(time.RFC3339),
	}
	if opts != nil {
		p.Channels = opts.Channels
		p.GroupKey = opts.GroupKey
		p.GroupSize = opts.GroupSize
	}
	dispatchAlert(db, encKey, p, opts)
}

func dispatchAlert(db *store.DB, encKey string, p AlertPayload, opts *AlertDispatchOpts) {
	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil {
		return
	}
	channels := resolveChannels(ncfg, opts)
	if len(channels) == 0 {
		return
	}
	p.Channels = channels
	text := FormatAlertText(p)
	md := FormatAlertMarkdown(p)

	for _, ch := range channels {
		switch ch {
		case "webhook":
			postAlertWebhook(ncfg, p)
		case "wecom":
			postAlertWecom(ncfg, md, opts)
		case "phone":
			postAlertPhone(ncfg, p)
		case "email":
			postAlertEmail(db, encKey, ncfg, p, opts)
		}
	}
	log.Printf("alert-notify: dispatched event=%s alert=%s channels=%v text=%s", p.Event, p.AlertID, channels, text)
}

func resolveChannels(ncfg *store.NotifyConfig, opts *AlertDispatchOpts) []string {
	if opts != nil && len(opts.Channels) > 0 {
		out := make([]string, 0, len(opts.Channels))
		for _, c := range opts.Channels {
			c = strings.ToLower(strings.TrimSpace(c))
			if c != "" {
				out = append(out, c)
			}
		}
		return out
	}
	var out []string
	if ncfg.WebhookEnabled && ncfg.WebhookOnAlert && strings.TrimSpace(ncfg.WebhookURL) != "" {
		out = append(out, "webhook")
	}
	if ncfg.WecomEnabled && ncfg.WecomOnAlert {
		out = append(out, "wecom")
	}
	if ncfg.PhoneEnabled && ncfg.PhoneOnAlert && strings.TrimSpace(ncfg.PhoneAPIURL) != "" {
		out = append(out, "phone")
	}
	if ncfg.EmailOnAlert {
		out = append(out, "email")
	}
	return out
}

func postAlertWebhook(ncfg *store.NotifyConfig, payload AlertPayload) {
	url := strings.TrimSpace(ncfg.WebhookURL)
	if url == "" {
		return
	}
	// 企微群机器人 URL：用 markdown 格式，避免通用 JSON 被拒
	if isWecomRobotURL(url) {
		if err := sendWecomRobotWebhook(url, FormatAlertMarkdown(payload)); err != nil {
			log.Printf("alert-notify: wecom-robot %s: %v", payload.AlertID, err)
		}
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("alert-notify: marshal: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("alert-notify: request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("alert-notify: webhook %s: %v", payload.AlertID, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("alert-notify: webhook %s status=%d", payload.AlertID, resp.StatusCode)
		return
	}
	log.Printf("alert-notify: webhook ok event=%s alert=%s", payload.Event, payload.AlertID)
}

func postAlertWecom(ncfg *store.NotifyConfig, md string, opts *AlertDispatchOpts) {
	to := strings.TrimSpace(ncfg.WecomToUser)
	if opts != nil && len(opts.ToUsers) > 0 {
		to = strings.Join(opts.ToUsers, "|")
	}
	if err := sendWecomMarkdown(ncfg.WecomCorpID, ncfg.WecomSecret, ncfg.WecomAgentID, to, md); err != nil {
		log.Printf("alert-notify: wecom: %v", err)
	}
}

func postAlertPhone(ncfg *store.NotifyConfig, p AlertPayload) {
	url := strings.TrimSpace(ncfg.PhoneAPIURL)
	if url == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"event": p.Event, "severity": p.Severity, "title": p.Title,
		"alert_id": p.AlertID, "service": p.Service, "cluster": p.Cluster,
		"cur_value": p.CurValue, "threshold": p.Threshold, "text": FormatAlertText(p),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("alert-notify: phone request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("alert-notify: phone %s: %v", p.AlertID, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("alert-notify: phone %s status=%d", p.AlertID, resp.StatusCode)
	}
}

func postAlertEmail(db *store.DB, encKey string, ncfg *store.NotifyConfig, p AlertPayload, opts *AlertDispatchOpts) {
	if !ncfg.EmailOnAlert {
		return
	}
	smtpCfg, mailCfg, ok := loadSMTP(db, encKey, p.AlertID, "alert")
	if !ok {
		return
	}
	users := collectAlertRecipients(db, p, opts)
	if len(users) == 0 {
		log.Printf("alert-notify: email skip no recipients alert=%s", p.AlertID)
		return
	}
	base := "http://127.0.0.1:5174"
	if smtpCfg != nil {
		if b := strings.TrimRight(strings.TrimSpace(smtpCfg.FrontendBaseURL), "/"); b != "" {
			base = b
		}
	}
	link := fmt.Sprintf("%s/#/ops/alerts", base)
	subject := fmt.Sprintf("[auto-dba][%s] %s %s", p.Severity, p.Event, p.Title)
	body := fmt.Sprintf("%s\n\n打开：%s\n", FormatAlertText(p), link)
	sendToUsernames(db, mailCfg, users, subject, body, p.AlertID, "alert")
}

func collectAlertRecipients(db *store.DB, p AlertPayload, opts *AlertDispatchOpts) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			return
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	if opts != nil {
		for _, u := range opts.ToUsers {
			add(u)
		}
	}
	if p.Labels != nil {
		add(p.Labels["owner"])
		add(p.Labels["notify"])
	}
	if oncall, err := db.CurrentAlertOncallUsernames(time.Now()); err == nil {
		for _, u := range oncall {
			add(u)
		}
	}
	return out
}

// FormatAlertText 纯文本摘要。
func FormatAlertText(p AlertPayload) string {
	extra := ""
	if p.GroupSize > 1 {
		extra = fmt.Sprintf(" group=%d", p.GroupSize)
	}
	return fmt.Sprintf("[%s] %s %s value=%s thr=%s handle=%s%s",
		p.Severity, p.Event, p.Title, p.CurValue, p.Threshold, p.Handle, extra)
}

// FormatAlertMarkdown 企微 markdown。
func FormatAlertMarkdown(p AlertPayload) string {
	color := "info"
	switch p.Severity {
	case "P0":
		color = "warning"
	case "P1":
		color = "warning"
	}
	group := ""
	if p.GroupSize > 1 {
		group = fmt.Sprintf("\n> 聚合: <font color=\"comment\">%d</font>", p.GroupSize)
	}
	return fmt.Sprintf("## [%s] %s\n> 事件: <font color=\"%s\">%s</font>\n> 服务: %s · 集群: %s\n> 当前值: %s / 阈值: %s\n> 处理: %s%s",
		p.Severity, p.Title, color, p.Event, nz(p.Service, "-"), nz(p.Cluster, "-"),
		nz(p.CurValue, "-"), nz(p.Threshold, "-"), nz(p.Handle, "-"), group)
}

func nz(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
