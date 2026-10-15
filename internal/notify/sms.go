package notify

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"auto-dba/internal/config"
)

func newSMSSender(cfg config.NotifyModuleConfig) channelSender {
	backend := strings.ToLower(strings.TrimSpace(cfg.Provider.SMS))
	if backend == "" {
		backend = "cloopen"
	}
	switch backend {
	case "yjzy":
		y := cfg.TeleProvider.YjzySMS
		if y.BaseURL == "" || y.CID == "" || y.Pwd == "" {
			return nil
		}
		return &yjzySender{cfg: y, action: "sms"}
	default:
		c := cfg.TeleProvider.Cloopen
		if !c.Enabled || c.APIAccount == "" || c.APIToken == "" || c.AppID == "" {
			return nil
		}
		return &cloopenSMSSender{cfg: c}
	}
}

type cloopenSMSSender struct {
	cfg config.CloopenSMSConfig
}

func (s *cloopenSMSSender) Close() {}

func (s *cloopenSMSSender) Send(msg Message) error {
	phones := msg.Phones
	if len(phones) == 0 {
		return fmt.Errorf("no phones")
	}
	to := strings.Join(phones, ",")
	content := strings.TrimSpace(msg.Text)
	if content == "" {
		content = strings.TrimSpace(msg.Markdown)
	}
	runes := []rune(content)
	if len(runes) > 140 {
		content = string(runes[:140])
	}
	sign := strings.TrimSpace(s.cfg.Singer)
	if sign == "" {
		sign = "【KeePlus】"
	}
	datas := sign + content
	ts := time.Now().Format("20060102150405")
	sig := strings.ToUpper(md5Hex(s.cfg.APIAccount + s.cfg.APIToken + ts))
	auth := base64.StdEncoding.EncodeToString([]byte(s.cfg.APIAccount + ":" + ts))
	endpoint := "app.cloopen.com:8883"
	path := fmt.Sprintf("/2013-12-26/Accounts/%s/SMS/TemplateSMS?sig=%s", s.cfg.APIAccount, sig)
	body := map[string]any{
		"appId": s.cfg.AppID,
		"to":    to,
		"code":  s.cfg.TemplateID,
		"datas": datas,
	}
	raw, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", auth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		StatusCode string `json:"statusCode"`
		StatusMsg  string `json:"statusMsg"`
	}
	_ = json.Unmarshal(b, &out)
	if out.StatusCode != "000000" {
		return fmt.Errorf("cloopen sms: %s %s body=%s", out.StatusCode, out.StatusMsg, string(b))
	}
	return nil
}

type yjzySender struct {
	cfg    config.YjzyTeleConfig
	action string // sms | call
}

func (s *yjzySender) Close() {}

func (s *yjzySender) Send(msg Message) error {
	phones := msg.Phones
	if len(phones) == 0 {
		return fmt.Errorf("no phones")
	}
	content := strings.TrimSpace(msg.Text)
	if content == "" {
		content = strings.TrimSpace(msg.Markdown)
	}
	if content == "" || (s.action == "call" && len([]rune(content)) >= 200) {
		content = "你有新的消息,请登录 KeePlus 查看"
	}
	sign := strings.TrimSpace(s.cfg.Singer)
	if sign == "" {
		sign = "【KeePlus】"
	}
	full := sign + content
	b64 := func(v string) string {
		return base64.StdEncoding.EncodeToString([]byte(v))
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // 对齐 yjzy 参考实现
		},
	}
	base := strings.TrimRight(strings.TrimSpace(s.cfg.BaseURL), "/")
	var lastErr error
	for _, phone := range phones {
		phone = strings.TrimSpace(phone)
		if phone == "" {
			continue
		}
		ssid := fmt.Sprintf("%d", time.Now().UnixNano())
		if len(ssid) > 18 {
			ssid = ssid[:18]
		}
		q := url.Values{}
		q.Set("cid", b64(s.cfg.CID))
		q.Set("pwd", b64(s.cfg.Pwd))
		q.Set("mobile", b64(phone))
		q.Set("productid", b64(s.cfg.ProductID))
		q.Set("lcode", b64("001"))
		q.Set("format", b64("32"))
		q.Set("content", b64(url.QueryEscape(full)))
		q.Set("ssid", b64(ssid))
		u := base + "/communication/sendSms.ashx?" + q.Encode()
		resp, err := client.Post(u, "application/x-www-form-urlencoded", nil)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("yjzy status=%d body=%s", resp.StatusCode, string(body))
		}
	}
	return lastErr
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
