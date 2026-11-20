package mail

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	smtpDialTimeoutDefault = 10 * time.Second
	smtpSendTimeoutDefault = 30 * time.Second
)

// 测试可缩短。生产使用默认值。
var (
	smtpDialTimeout = smtpDialTimeoutDefault
	smtpSendTimeout = smtpSendTimeoutDefault
)

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool
	AuthWay  string
	Rate     int
}

func (c Config) addr() string {
	port := c.Port
	if port <= 0 {
		if c.UseTLS {
			port = 465
		} else {
			port = 25
		}
	}
	return net.JoinHostPort(strings.TrimSpace(c.Host), fmt.Sprintf("%d", port))
}

func (c Config) fromAddr() string {
	from := strings.TrimSpace(c.From)
	if from == "" {
		from = strings.TrimSpace(c.Username)
	}
	return from
}

// Send 发送纯文本或简易 HTML 邮件（Content-Type 由 contentType 指定）。
func Send(cfg Config, to, subject, body, contentType string) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return fmt.Errorf("SMTP host 未配置")
	}
	to = strings.TrimSpace(to)
	if to == "" || !strings.Contains(to, "@") {
		return fmt.Errorf("收件人邮箱无效")
	}
	from := cfg.fromAddr()
	if from == "" || !strings.Contains(from, "@") {
		return fmt.Errorf("发件人邮箱无效")
	}
	if contentType == "" {
		contentType = "text/plain; charset=UTF-8"
	}
	msg := strings.Builder{}
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("Subject: " + encodeSubject(subject) + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: " + contentType + "\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	return deliverRaw(cfg, host, from, to, []byte(msg.String()))
}

// SendHTMLAttachment 正文只放说明，周报 HTML 作为附件发送。
func SendHTMLAttachment(cfg Config, to, subject, note, filename, htmlBody string) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return fmt.Errorf("SMTP host 未配置")
	}
	to = strings.TrimSpace(to)
	if to == "" || !strings.Contains(to, "@") {
		return fmt.Errorf("收件人邮箱无效")
	}
	from := cfg.fromAddr()
	if from == "" || !strings.Contains(from, "@") {
		return fmt.Errorf("发件人邮箱无效")
	}
	if strings.TrimSpace(filename) == "" {
		filename = "slow-sql-report.html"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".html") {
		filename += ".html"
	}
	if strings.TrimSpace(note) == "" {
		note = "慢 SQL 周报已作为 HTML 附件发送，请下载后用浏览器打开。"
	}
	boundary := fmt.Sprintf("auto-dba-%d", time.Now().UnixNano())
	var msg strings.Builder
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("Subject: " + encodeSubject(subject) + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n")
	msg.WriteString("\r\n")
	msg.WriteString("--" + boundary + "\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: base64\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(base64Lines(note))
	msg.WriteString("\r\n")
	msg.WriteString("--" + boundary + "\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: base64\r\n")
	msg.WriteString("Content-Disposition: attachment; filename=\"slow-sql-report.html\"; filename*=UTF-8''" + url.PathEscape(filename) + "\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(base64Lines(htmlBody))
	msg.WriteString("\r\n")
	msg.WriteString("--" + boundary + "--\r\n")
	return deliverRaw(cfg, host, from, to, []byte(msg.String()))
}

func deliverRaw(cfg Config, host, from, to string, raw []byte) error {
	waitRate(cfg.Rate)
	addr := cfg.addr()
	auth := smtpAuth(cfg, host)
	var err error
	if cfg.UseTLS && (cfg.Port == 465 || cfg.Port == 0) {
		err = sendSMTPS(addr, host, auth, from, []string{to}, raw)
	} else {
		err = sendSMTP(addr, host, auth, from, []string{to}, raw)
	}
	return wrapSMTPTimeout(err)
}

func base64Lines(s string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	return b.String()
}

func wrapSMTPTimeout(err error) error {
	if err == nil {
		return nil
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return fmt.Errorf("SMTP 超时: %w", err)
	}
	return err
}

func sendSMTP(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("连接 SMTP 失败: %w", err)
	}
	if err := conn.SetDeadline(time.Now().Add(smtpSendTimeout)); err != nil {
		_ = conn.Close()
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("SMTP STARTTLS 失败: %w", err)
		}
	}
	return deliverSMTP(client, auth, from, to, msg)
}

func sendSMTPS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("连接 SMTP(TLS) 失败: %w", err)
	}
	if err := conn.SetDeadline(time.Now().Add(smtpSendTimeout)); err != nil {
		_ = conn.Close()
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	return deliverSMTP(client, auth, from, to, msg)
}

func deliverSMTP(client *smtp.Client, auth smtp.Auth, from string, to []string, msg []byte) error {
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func smtpAuth(cfg Config, host string) smtp.Auth {
	user := strings.TrimSpace(cfg.Username)
	if user == "" {
		return nil
	}
	switch strings.ToUpper(strings.TrimSpace(cfg.AuthWay)) {
	case "LOGIN":
		return &loginAuth{username: user, password: cfg.Password, host: host}
	case "CRAM-MD5", "CRAMMD5":
		return smtp.CRAMMD5Auth(user, cfg.Password)
	default:
		return smtp.PlainAuth("", user, cfg.Password, host)
	}
}

type loginAuth struct {
	username, password, host string
	sentUser                 bool
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if server == nil || (!server.TLS && !isSMTPLocalhost(server.Name)) {
		return "", nil, errors.New("unencrypted connection")
	}
	if server.Name != a.host {
		return "", nil, errors.New("wrong host name")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	prompt := strings.ToLower(strings.TrimSpace(string(fromServer)))
	if strings.Contains(prompt, "password") || a.sentUser {
		a.sentUser = true
		return []byte(a.password), nil
	}
	a.sentUser = true
	return []byte(a.username), nil
}

func isSMTPLocalhost(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}

var (
	rateMu   sync.Mutex
	nextSend time.Time
)

func waitRate(rate int) {
	if rate <= 0 {
		rate = 2
	}
	gap := time.Second / time.Duration(rate)
	if gap <= 0 {
		gap = 500 * time.Millisecond
	}
	rateMu.Lock()
	defer rateMu.Unlock()
	now := time.Now()
	if now.Before(nextSend) {
		time.Sleep(nextSend.Sub(now))
		now = time.Now()
	}
	nextSend = now.Add(gap)
}

func encodeSubject(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "auto-dba"
	}
	for _, r := range s {
		if r > 127 {
			return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
		}
	}
	return s
}
