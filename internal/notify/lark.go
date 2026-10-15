package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/config"
)

type larkSender struct {
	appID     string
	appSecret string
	baseURL   string

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func newLarkSender(cfg config.LarkNotifyConfig) (*larkSender, error) {
	appID := strings.TrimSpace(cfg.AppID)
	secret := strings.TrimSpace(cfg.AppSecret)
	if appID == "" || secret == "" {
		return nil, fmt.Errorf("lark app_id/app_secret required")
	}
	return &larkSender{
		appID:     appID,
		appSecret: secret,
		baseURL:   "https://open.feishu.cn",
	}, nil
}

func (s *larkSender) Close() {}

func (s *larkSender) accessToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.expiresAt) {
		return s.token, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{
		"app_id":     s.appID,
		"app_secret": s.appSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Code != 0 || out.TenantAccessToken == "" {
		return "", fmt.Errorf("lark token: code=%d msg=%s", out.Code, out.Msg)
	}
	ttl := out.Expire - 120
	if ttl < 60 {
		ttl = 60
	}
	s.token = out.TenantAccessToken
	s.expiresAt = time.Now().Add(time.Duration(ttl) * time.Second)
	return s.token, nil
}

func (s *larkSender) Send(msg Message) error {
	ids := msg.LarkUserIDs
	if len(ids) == 0 {
		return fmt.Errorf("no lark user ids")
	}
	token, err := s.accessToken()
	if err != nil {
		return err
	}
	title := strings.TrimSpace(msg.Title)
	if title == "" {
		title = "KeePlus"
	}
	body := strings.TrimSpace(msg.Text)
	if body == "" {
		body = strings.TrimSpace(msg.Markdown)
	}
	text := fmt.Sprintf("【%s】\n%s", title, body)
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := s.postText(token, id, text); err != nil {
			return err
		}
	}
	return nil
}

func (s *larkSender) postText(token, userID, text string) error {
	payload := map[string]any{
		"receive_id": userID,
		"msg_type":   "text",
		"content":    mustJSON(map[string]string{"text": text}),
	}
	raw, _ := json.Marshal(payload)
	u := s.baseURL + "/open-apis/im/v1/messages?receive_id_type=" + url.QueryEscape("user_id")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(b, &out)
	if out.Code != 0 {
		return fmt.Errorf("lark send to %s: code=%d msg=%s", userID, out.Code, out.Msg)
	}
	return nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ResolveLarkUserID 从用户档案取飞书 user_id。
func ResolveLarkUserID(imType, imAccount string) string {
	imAccount = strings.TrimSpace(imAccount)
	if imAccount == "" {
		return ""
	}
	t := strings.ToLower(strings.TrimSpace(imType))
	if t == "" || t == "lark" || t == "feishu" {
		return imAccount
	}
	return ""
}
