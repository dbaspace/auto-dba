package mail

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool
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

	addr := cfg.addr()
	authUser := strings.TrimSpace(cfg.Username)
	var auth smtp.Auth
	if authUser != "" {
		auth = smtp.PlainAuth("", authUser, cfg.Password, host)
	}

	if cfg.UseTLS && (cfg.Port == 465 || cfg.Port == 0) {
		return sendSMTPS(addr, host, auth, from, []string{to}, []byte(msg.String()))
	}
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg.String()))
}

func sendSMTPS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("连接 SMTP(TLS) 失败: %w", err)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
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
