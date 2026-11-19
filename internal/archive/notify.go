package archive

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type MailSettings struct {
	Host     string
	Port     int
	Account  string
	Password string
	Sender   string
	Timeout  int
}

type WebhookSettings struct {
	URL    string
	Secret string
}

type StorageSettings struct {
	AvailableTb float64
	Set         bool
}

type AlertSettings struct {
	Mail    MailSettings
	Webhook WebhookSettings
	Storage StorageSettings
}

func ParseAlertSettings(raw string) AlertSettings {
	var m map[string]any
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &m)
	}
	return alertSettingsFromMap(m)
}

func alertSettingsFromMap(m map[string]any) AlertSettings {
	var out AlertSettings
	mail, _ := m["mail"].(map[string]any)
	hook, _ := m["webhook"].(map[string]any)
	storage, _ := m["storage"].(map[string]any)
	out.Mail.Host = strOf(mail, "host")
	out.Mail.Port = intOf(mail, "port", 25)
	out.Mail.Account = strOf(mail, "account")
	out.Mail.Password = strOf(mail, "password")
	out.Mail.Sender = strOf(mail, "sender")
	out.Mail.Timeout = intOf(mail, "timeout", 10)
	out.Webhook.URL = strOf(hook, "url")
	out.Webhook.Secret = strOf(hook, "secret")
	if storage != nil {
		if v, ok := storage["availableTb"]; ok && v != nil {
			switch n := v.(type) {
			case float64:
				out.Storage.AvailableTb = n
				out.Storage.Set = true
			case string:
				if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
					out.Storage.AvailableTb = f
					out.Storage.Set = true
				}
			}
		}
	}
	return out
}

func strOf(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return strings.TrimSpace(s)
}

func intOf(m map[string]any, k string, def int) int {
	if m == nil {
		return def
	}
	switch v := m[k].(type) {
	case float64:
		if v > 0 {
			return int(v)
		}
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil && n > 0 {
			return n
		}
	}
	return def
}

func MergeMail(base MailSettings, override map[string]any) MailSettings {
	if override == nil {
		return base
	}
	if s := strOf(override, "host"); s != "" {
		base.Host = s
	}
	if s := strOf(override, "account"); s != "" {
		base.Account = s
	}
	if s := strOf(override, "password"); s != "" {
		base.Password = s
	}
	if s := strOf(override, "sender"); s != "" {
		base.Sender = s
	}
	if n := intOf(override, "port", 0); n > 0 {
		base.Port = n
	}
	if n := intOf(override, "timeout", 0); n > 0 {
		base.Timeout = n
	}
	return base
}

func MergeWebhook(base WebhookSettings, override map[string]any) WebhookSettings {
	if override == nil {
		return base
	}
	if s := strOf(override, "url"); s != "" {
		base.URL = s
	}
	if s := strOf(override, "secret"); s != "" {
		base.Secret = s
	}
	return base
}

func SendMail(ctx context.Context, cfg MailSettings, to, subject, body string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		to = strings.TrimSpace(cfg.Sender)
	}
	if cfg.Host == "" || cfg.Sender == "" || to == "" {
		return fmt.Errorf("邮件未配置完整")
	}
	if cfg.Port <= 0 {
		cfg.Port = 25
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10
	}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("连接邮件服务器失败: %w", err)
	}
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		_ = client.StartTLS(&tls.Config{ServerName: cfg.Host})
	}
	if cfg.Account != "" {
		if err := client.Auth(unencryptedPlainAuth{cfg.Account, cfg.Password, cfg.Host}); err != nil {
			return fmt.Errorf("邮件认证失败: %w", err)
		}
	}
	if err := client.Mail(cfg.Sender); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	msg := "To: " + to + "\r\nFrom: " + cfg.Sender + "\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	if _, err := io.WriteString(w, msg); err != nil {
		return err
	}
	return w.Close()
}

type unencryptedPlainAuth struct {
	user, pass, host string
}

func (a unencryptedPlainAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}

func (a unencryptedPlainAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return nil, fmt.Errorf("邮件服务器要求额外认证步骤")
	}
	return nil, nil
}

func SendWebhook(ctx context.Context, cfg WebhookSettings, text string) error {
	if strings.TrimSpace(cfg.URL) == "" {
		return fmt.Errorf("Webhook 地址为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	payload := webhookPayload(cfg.URL, text)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Secret != "" {
		req.Header.Set("X-Webhook-Secret", cfg.Secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("Webhook 请求失败: %w", err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(buf))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("Webhook 返回 %s", msg)
	}
	return nil
}

func webhookPayload(url, text string) string {
	lower := strings.ToLower(url)
	if strings.Contains(lower, "feishu") || strings.Contains(lower, "lark") {
		b, _ := json.Marshal(map[string]any{
			"msg_type": "text",
			"content":  map[string]string{"text": text},
		})
		return string(b)
	}
	b, _ := json.Marshal(map[string]string{"text": text})
	return string(b)
}

func NotifyAlert(ctx context.Context, cfg AlertSettings, message string) error {
	var errs []string
	if cfg.Mail.Host != "" && cfg.Mail.Sender != "" {
		if err := SendMail(ctx, cfg.Mail, cfg.Mail.Sender, "归档告警", message); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if cfg.Webhook.URL != "" {
		if err := SendWebhook(ctx, cfg.Webhook, message); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "；"))
	}
	return nil
}
