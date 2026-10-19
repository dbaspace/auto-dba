package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
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

// 飞书 receive_id_type 取值。
const (
	larkIDUserID  = "user_id"
	larkIDOpenID  = "open_id"
	larkIDUnionID = "union_id"
	larkIDEmail   = "email"
	larkIDChatID  = "chat_id"
	larkIDMobile  = "mobile" // 非飞书原生类型：先经通讯录换成 user_id
)

var cnMobileRe = regexp.MustCompile(`^1\d{10}$`)

// larkReceiveIDType 按格式识别接收人类型；支持显式前缀 email: / open_id: / union_id: / user_id: / chat_id: / mobile:。
func larkReceiveIDType(id string) (typ, normalized string) {
	id = strings.TrimSpace(id)
	if i := strings.Index(id, ":"); i > 0 {
		prefix := strings.ToLower(strings.TrimSpace(id[:i]))
		rest := strings.TrimSpace(id[i+1:])
		switch prefix {
		case larkIDEmail, larkIDOpenID, larkIDUnionID, larkIDUserID, larkIDChatID, larkIDMobile:
			if prefix == larkIDMobile {
				return larkIDMobile, normalizeLarkMobile(rest)
			}
			return prefix, rest
		}
	}
	switch {
	case strings.Contains(id, "@"):
		return larkIDEmail, id
	case strings.HasPrefix(id, "ou_"):
		return larkIDOpenID, id
	case strings.HasPrefix(id, "on_"):
		return larkIDUnionID, id
	case strings.HasPrefix(id, "oc_"):
		return larkIDChatID, id
	case strings.HasPrefix(id, "+"), cnMobileRe.MatchString(id):
		return larkIDMobile, normalizeLarkMobile(id)
	}
	return larkIDUserID, id
}

func normalizeLarkMobile(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	s = strings.ReplaceAll(s, "-", "")
	return strings.TrimPrefix(s, "+86")
}

// larkInvalidIDCodes 接收人 ID 无效/不存在/不在可用范围：可换下一个候选重试。
var larkInvalidIDCodes = map[int]bool{
	99992360: true, 99992361: true, 99992364: true, 230013: true, 230006: true,
}

type larkAPIError struct {
	Code int
	Msg  string
}

func (e *larkAPIError) Error() string { return fmt.Sprintf("code=%d msg=%s", e.Code, e.Msg) }

func larkRetryable(err error) bool {
	var ae *larkAPIError
	if errors.As(err, &ae) {
		return larkInvalidIDCodes[ae.Code]
	}
	var nf *larkNotFoundError
	return errors.As(err, &nf)
}

type larkNotFoundError struct{ what string }

func (e *larkNotFoundError) Error() string { return e.what }

func (s *larkSender) Send(msg Message) error {
	people := make([][]string, 0, len(msg.LarkUserIDs)+len(msg.LarkRecipients))
	for _, id := range msg.LarkUserIDs {
		if id = strings.TrimSpace(id); id != "" {
			people = append(people, []string{id})
		}
	}
	for _, cands := range msg.LarkRecipients {
		if len(cands) > 0 {
			people = append(people, cands)
		}
	}
	if len(people) == 0 {
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
	var fails []string
	for _, cands := range people {
		if err := s.sendToCandidates(token, cands, text); err != nil {
			log.Printf("notify lark: %v", err)
			fails = append(fails, err.Error())
		}
	}
	if len(fails) > 0 {
		return fmt.Errorf("飞书发送 %d 人中 %d 人失败：%s", len(people), len(fails), strings.Join(fails, "；"))
	}
	return nil
}

// sendToCandidates 依次尝试同一人的候选身份，成功即停；仅「ID 无效」类错误才换下一个。
func (s *larkSender) sendToCandidates(token string, cands []string, text string) error {
	var errs []string
	for _, c := range cands {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		typ, id := larkReceiveIDType(c)
		err := s.sendOne(token, typ, id, text)
		if err == nil {
			return nil
		}
		errs = append(errs, larkSendErrText(c, typ, err))
		if !larkRetryable(err) {
			break
		}
	}
	if len(errs) == 0 {
		return fmt.Errorf("lark send: empty recipient")
	}
	return errors.New(strings.Join(errs, " | "))
}

func larkSendErrText(raw, typ string, err error) string {
	msg := fmt.Sprintf("lark send to %s (%s): %v", raw, typ, err)
	var ae *larkAPIError
	if errors.As(err, &ae) && ae.Code == 99992360 {
		msg += "；提示：user_id 为飞书企业内用户 ID（非登录名），可改填飞书通讯录中的邮箱 / 手机号 / open_id"
	}
	return msg
}

func (s *larkSender) sendOne(token, typ, id, text string) error {
	if typ == larkIDMobile {
		uid, err := s.userIDByMobile(token, id)
		if err != nil {
			return err
		}
		typ, id = larkIDUserID, uid
	}
	return s.postText(token, typ, id, text)
}

func (s *larkSender) doJSON(token, method, u string, payload any, out any) error {
	raw, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(raw))
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
	return json.Unmarshal(b, out)
}

// userIDByMobile 经通讯录 batch_get_id 将手机号换成 user_id（需 contact:user.id:readonly）。
func (s *larkSender) userIDByMobile(token, mobile string) (string, error) {
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UserList []struct {
				UserID string `json:"user_id"`
				Mobile string `json:"mobile"`
			} `json:"user_list"`
		} `json:"data"`
	}
	u := s.baseURL + "/open-apis/contact/v3/users/batch_get_id?user_id_type=user_id"
	if err := s.doJSON(token, http.MethodPost, u, map[string]any{"mobiles": []string{mobile}}, &out); err != nil {
		return "", err
	}
	if out.Code != 0 {
		return "", &larkAPIError{Code: out.Code, Msg: out.Msg}
	}
	for _, it := range out.Data.UserList {
		if strings.TrimSpace(it.UserID) != "" {
			return it.UserID, nil
		}
	}
	return "", &larkNotFoundError{what: "飞书通讯录中未找到该手机号（或不在应用可用范围）"}
}

func (s *larkSender) postText(token, idType, receiveID, text string) error {
	payload := map[string]any{
		"receive_id": receiveID,
		"msg_type":   "text",
		"content":    mustJSON(map[string]string{"text": text}),
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	u := s.baseURL + "/open-apis/im/v1/messages?receive_id_type=" + url.QueryEscape(idType)
	if err := s.doJSON(token, http.MethodPost, u, payload, &out); err != nil {
		return err
	}
	if out.Code != 0 {
		return &larkAPIError{Code: out.Code, Msg: out.Msg}
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

// ResolveLarkRecipient 用户的飞书候选身份（按优先级）。
// 已同步：open_id > user_id > 飞书邮箱，不再用企业邮箱（飞书私聊不认）。
// 未同步：飞书邮箱 > im_account > 邮箱 > 手机号。
func ResolveLarkRecipient(u *store.User) []string {
	if u == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			return
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	larkEmail := strings.TrimSpace(u.LarkEmail)
	if !strings.Contains(larkEmail, "@") {
		larkEmail = ""
	}
	if oid, uid := strings.TrimSpace(u.LarkOpenID), strings.TrimSpace(u.LarkID); oid != "" || uid != "" {
		if oid != "" {
			add("open_id:" + oid)
		}
		if uid != "" {
			add("user_id:" + uid)
		}
		if larkEmail != "" {
			add("email:" + larkEmail)
		}
		return out
	}
	if larkEmail != "" {
		add("email:" + larkEmail)
	}
	add(ResolveLarkUserID(u.IMType, u.IMAccount))
	if e := strings.TrimSpace(u.Email); strings.Contains(e, "@") {
		add("email:" + e)
	}
	if p := strings.TrimSpace(u.Phone); p != "" {
		add("mobile:" + p)
	}
	return out
}

// CollectLarkRecipients 按用户名去重收集飞书候选身份；无任何身份的用户记日志跳过。
func CollectLarkRecipients(db *store.DB, usernames []string, tag string) [][]string {
	if db == nil {
		return nil
	}
	var out [][]string
	seen := map[string]bool{}
	for _, uname := range usernames {
		uname = strings.TrimSpace(uname)
		key := strings.ToLower(uname)
		if uname == "" || seen[key] {
			continue
		}
		seen[key] = true
		u, _ := db.GetUserByUsername(uname)
		if u == nil {
			log.Printf("notify lark %s: user %q not found", tag, uname)
			continue
		}
		cands := ResolveLarkRecipient(u)
		if len(cands) == 0 {
			log.Printf("notify lark %s: user %q has no im_account/email/phone", tag, uname)
			continue
		}
		out = append(out, cands)
	}
	return out
}
