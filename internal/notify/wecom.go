package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type wecomTokenCache struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
	corpID    string
	secret    string
}

var wecomTok wecomTokenCache

func getWecomAccessToken(corpID, secret string) (string, error) {
	corpID = strings.TrimSpace(corpID)
	secret = strings.TrimSpace(secret)
	if corpID == "" || secret == "" {
		return "", fmt.Errorf("企微 CorpID/Secret 未配置")
	}
	wecomTok.mu.Lock()
	defer wecomTok.mu.Unlock()
	if wecomTok.token != "" && wecomTok.corpID == corpID && wecomTok.secret == secret && time.Now().Before(wecomTok.expiresAt) {
		return wecomTok.token, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s", corpID, secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.ErrCode != 0 || out.AccessToken == "" {
		return "", fmt.Errorf("企微 gettoken: %d %s", out.ErrCode, out.ErrMsg)
	}
	ttl := out.ExpiresIn - 120
	if ttl < 60 {
		ttl = 60
	}
	wecomTok.token = out.AccessToken
	wecomTok.corpID = corpID
	wecomTok.secret = secret
	wecomTok.expiresAt = time.Now().Add(time.Duration(ttl) * time.Second)
	return out.AccessToken, nil
}

func sendWecomMarkdown(corpID, secret, agentID, toUser, content string) error {
	token, err := getWecomAccessToken(corpID, secret)
	if err != nil {
		return err
	}
	aid, _ := strconv.Atoi(strings.TrimSpace(agentID))
	if aid <= 0 {
		return fmt.Errorf("企微 AgentID 无效")
	}
	toUser = strings.TrimSpace(toUser)
	if toUser == "" {
		toUser = "@all"
	}
	payload := map[string]any{
		"touser":  toUser,
		"msgtype": "markdown",
		"agentid": aid,
		"markdown": map[string]string{
			"content": content,
		},
	}
	raw, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	url := "https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=" + token
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
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
	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	_ = json.Unmarshal(body, &out)
	if out.ErrCode != 0 {
		return fmt.Errorf("企微 send: %d %s", out.ErrCode, out.ErrMsg)
	}
	return nil
}

func sendWecomRobotWebhook(webhookURL, content string) error {
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	}
	raw, _ := json.Marshal(payload)
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
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func isWecomRobotURL(u string) bool {
	u = strings.ToLower(u)
	return strings.Contains(u, "qyapi.weixin.qq.com") && strings.Contains(u, "webhook")
}
