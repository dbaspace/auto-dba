package notify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"auto-dba/internal/config"
)

func newCallSender(cfg config.NotifyModuleConfig) channelSender {
	backend := strings.ToLower(strings.TrimSpace(cfg.Provider.Call))
	if backend == "" {
		backend = "cloopen"
	}
	switch backend {
	case "huawei":
		h := cfg.TeleProvider.Huawei
		if !h.Enabled || h.APIKey == "" || h.APISecret == "" {
			return nil
		}
		return &huaweiCallSender{cfg: h}
	case "yjzy":
		y := cfg.TeleProvider.YjzyCall
		if y.BaseURL == "" || y.CID == "" || y.Pwd == "" {
			return nil
		}
		return &yjzySender{cfg: y, action: "call"}
	default:
		c := cfg.TeleProvider.CloopenCall
		if !c.Enabled || c.APIAccount == "" || c.APIToken == "" || c.AppID == "" {
			return nil
		}
		return &cloopenCallSender{cfg: c}
	}
}

type cloopenCallSender struct {
	cfg config.CloopenCallConfig
}

func (s *cloopenCallSender) Close() {}

func (s *cloopenCallSender) Send(msg Message) error {
	phones := msg.Phones
	if len(phones) == 0 {
		return fmt.Errorf("no phones")
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = strings.TrimSpace(msg.Markdown)
	}
	if text == "" || len([]rune(text)) > 70 {
		text = "你有新的消息,请登录 KeePlus 查看"
	}
	ts := time.Now().Format("20060102150405")
	sig := strings.ToUpper(md5Hex(s.cfg.APIAccount + s.cfg.APIToken + ts))
	auth := base64.StdEncoding.EncodeToString([]byte(s.cfg.APIAccount + ":" + ts))
	endpoint := "app.cloopen.com:8883"
	var lastErr error
	for _, phone := range phones {
		phone = strings.TrimSpace(phone)
		if phone == "" {
			continue
		}
		path := fmt.Sprintf("/2013-12-26/Accounts/%s/Calls/LandingCalls?sig=%s", s.cfg.APIAccount, sig)
		body := map[string]any{
			"appId":      s.cfg.AppID,
			"to":         phone,
			"displayNum": s.cfg.DisplayNum,
		}
		if s.cfg.TTS {
			body["mediaTxt"] = text
		} else {
			body["mediaName"] = s.cfg.MediaName
		}
		raw, _ := json.Marshal(body)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+endpoint+path, bytes.NewReader(raw))
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json;charset=utf-8")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", auth)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		var out struct {
			StatusCode string `json:"statusCode"`
			StatusMsg  string `json:"statusMsg"`
		}
		_ = json.Unmarshal(b, &out)
		if out.StatusCode != "000000" {
			lastErr = fmt.Errorf("cloopen call: %s %s", out.StatusCode, out.StatusMsg)
		}
	}
	return lastErr
}

type huaweiCallSender struct {
	cfg config.HuaweiCallConfig
	idx int
}

func (s *huaweiCallSender) Close() {}

func (s *huaweiCallSender) Send(msg Message) error {
	phones := msg.Phones
	if len(phones) == 0 {
		return fmt.Errorf("no phones")
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = strings.TrimSpace(msg.Markdown)
	}
	paramSize := s.cfg.ParamSize
	if paramSize <= 0 {
		paramSize = 32
	}
	paramNum := s.cfg.ParamNum
	if paramNum <= 0 {
		paramNum = 6
	}
	paras := splitRunes(text, paramSize, paramNum)
	base := strings.TrimRight(strings.TrimSpace(s.cfg.BaseURL), "/")
	if base == "" {
		base = "https://rtccall.cn-north-1.myhuaweicloud.cn:443"
	}
	statusB64 := base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(s.cfg.StatusURL)))
	var lastErr error
	for _, phone := range phones {
		phone = strings.TrimSpace(phone)
		if phone == "" {
			continue
		}
		if !strings.HasPrefix(phone, "+") {
			phone = "+86" + phone
		}
		display := ""
		if len(s.cfg.DisplayNumbers) > 0 {
			display = s.cfg.DisplayNumbers[s.idx%len(s.cfg.DisplayNumbers)]
			s.idx++
		}
		body := map[string]any{
			"displayNbr": display,
			"calleeNbr":  phone,
			"statusUrl":  statusB64,
			"playInfoList": []map[string]any{{
				"templateId":    s.cfg.TemplateID,
				"templateParas": paras,
			}},
		}
		raw, _ := json.Marshal(body)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/rest/httpsessions/callnotify/v2.0", bytes.NewReader(raw))
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json;charset=UTF-8")
		req.Header.Set("Authorization", `WSSE realm="SDP",profile="UsernameToken",type="Appkey"`)
		req.Header.Set("X-WSSE", buildHuaweiWSSE(s.cfg.APIKey, s.cfg.APISecret))
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("huawei call status=%d body=%s", resp.StatusCode, string(b))
		}
	}
	return lastErr
}

func buildHuaweiWSSE(appKey, appSecret string) string {
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	created := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	raw := nonce + created + appSecret
	sum := sha256.Sum256([]byte(raw))
	digest := base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(sum[:])))
	return fmt.Sprintf(`UsernameToken Username="%s",PasswordDigest="%s",Nonce="%s",Created="%s"`,
		appKey, digest, nonce, created)
}

func splitRunes(s string, size, maxParts int) []string {
	r := []rune(s)
	var out []string
	for len(r) > 0 && len(out) < maxParts {
		n := size
		if n > len(r) {
			n = len(r)
		}
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	for len(out) < maxParts {
		out = append(out, "")
	}
	return out
}
