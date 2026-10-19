package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"auto-dba/internal/store"
)

// 国际版通讯录的企业邮箱只在 open.larksuite.com 返回；发消息仍走 open.feishu.cn。
const (
	larkDirectoryBase  = "https://open.larksuite.com"
	larkSyncPageSize   = 50
	larkSyncBatchUsers = 25
)

var larkSyncMaxPages = 200

var (
	larkSyncRunning atomic.Bool
	larkSyncGap     = 150 * time.Millisecond
	larkSyncBackoff = 2 * time.Second

	ErrLarkSyncRunning  = errors.New("飞书同步进行中，请稍后再试")
	ErrLarkSyncNotReady = errors.New("飞书应用未就绪：请在 Nacos notify.provider.lark 配置 enabled、app_id、app_secret")
)

// LarkSyncResult 一次同步的统计；用户名列表便于页面提示。
type LarkSyncResult struct {
	Total        int      `json:"total"`
	Synced       int      `json:"synced"`
	Unmatched    []string `json:"unmatched"`
	NoKey        []string `json:"no_key"`
	NotLoggedIn  int      `json:"not_logged_in"`
	Skipped      []string `json:"skipped"`
	Requests     int      `json:"requests"`
	ScannedPages int      `json:"scanned_pages"`
	Message      string   `json:"message,omitempty"`
}

type larkRateLimitError struct{ detail string }

func (e *larkRateLimitError) Error() string { return "飞书接口限流：" + e.detail }

type larkSyncUser struct {
	user store.User
	keys []string // e:<email> / m:<mobile>，按优先级
}

// SyncLarkUsers 只对登录过本平台的用户按飞书邮箱、企业邮箱、手机号批量点查飞书身份。
func SyncLarkUsers(db *store.DB) (*LarkSyncResult, error) {
	globalReg.mu.RLock()
	s, _ := globalReg.lark.(*larkSender)
	globalReg.mu.RUnlock()
	if s == nil {
		return nil, ErrLarkSyncNotReady
	}
	if !larkSyncRunning.CompareAndSwap(false, true) {
		return nil, ErrLarkSyncRunning
	}
	defer larkSyncRunning.Store(false)
	users, err := db.ListUsers()
	if err != nil {
		return nil, err
	}
	return s.syncUsers(db.SetUserLarkIdentity, users)
}

type larkIdentitySaver func(id, larkID, openID, matchedEmail string, at time.Time) error

func (s *larkSender) syncUsers(save larkIdentitySaver, users []store.User) (*LarkSyncResult, error) {
	res := &LarkSyncResult{Unmatched: []string{}, NoKey: []string{}, Skipped: []string{}}
	var todo []larkSyncUser
	for _, u := range users {
		if store.IsBuiltinAdminUser(&u) {
			continue
		}
		if !u.HasLoggedIn {
			res.NotLoggedIn++
			continue
		}
		keys := larkSyncKeys(u)
		if len(keys) == 0 {
			res.NoKey = append(res.NoKey, u.Username)
			continue
		}
		todo = append(todo, larkSyncUser{user: u, keys: keys})
	}
	res.Total = len(todo)
	if len(todo) == 0 {
		return res, nil
	}
	token, err := s.accessToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var pending []larkSyncUser
	for start := 0; start < len(todo); start += larkSyncBatchUsers {
		end := start + larkSyncBatchUsers
		if end > len(todo) {
			end = len(todo)
		}
		batch := todo[start:end]
		userIDs, openIDs, err := s.lookupBatch(token, batch, res)
		if err != nil {
			for _, it := range todo[start:] {
				res.Skipped = append(res.Skipped, it.user.Username)
			}
			res.Message = err.Error()
			return res, nil
		}
		for _, it := range batch {
			matched := false
			for _, k := range it.keys {
				uid, oid := userIDs[k], openIDs[k]
				if uid == "" && oid == "" {
					continue
				}
				email := ""
				if strings.HasPrefix(k, "e:") {
					email = larkKeyEmail(it.user, k)
				}
				if err := save(it.user.ID, uid, oid, email, now); err != nil {
					res.Skipped = append(res.Skipped, it.user.Username)
				} else {
					res.Synced++
				}
				matched = true
				break
			}
			if !matched {
				pending = append(pending, it)
			}
		}
	}
	if len(pending) > 0 {
		s.fillByDirectory(pending, res, save, now)
	}
	return res, nil
}

func larkSyncKeys(u store.User) []string {
	var keys []string
	seen := map[string]bool{}
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}
	for _, e := range []string{u.LarkEmail, u.Email} {
		if e = strings.ToLower(strings.TrimSpace(e)); strings.Contains(e, "@") {
			add("e:" + e)
		}
	}
	if p := normalizeLarkMobile(u.Phone); p != "" {
		add("m:" + p)
	}
	return keys
}

// larkKeyEmail 还原命中邮箱的原始大小写。
func larkKeyEmail(u store.User, key string) string {
	want := strings.TrimPrefix(key, "e:")
	for _, e := range []string{u.LarkEmail, u.Email} {
		if strings.EqualFold(strings.TrimSpace(e), want) {
			return strings.TrimSpace(e)
		}
	}
	return want
}

func (s *larkSender) lookupBatch(token string, batch []larkSyncUser, res *LarkSyncResult) (map[string]string, map[string]string, error) {
	var emails, mobiles []string
	for _, it := range batch {
		for _, k := range it.keys {
			if strings.HasPrefix(k, "e:") {
				emails = append(emails, strings.TrimPrefix(k, "e:"))
			} else {
				mobiles = append(mobiles, strings.TrimPrefix(k, "m:"))
			}
		}
	}
	userIDs, err := s.batchGetIDRetry(token, larkIDUserID, emails, mobiles, res)
	if err != nil {
		return nil, nil, err
	}
	openIDs, err := s.batchGetIDRetry(token, larkIDOpenID, emails, mobiles, res)
	if err != nil {
		return nil, nil, err
	}
	return userIDs, openIDs, nil
}

func (s *larkSender) batchGetIDRetry(token, idType string, emails, mobiles []string, res *LarkSyncResult) (map[string]string, error) {
	if res.Requests > 0 {
		time.Sleep(larkSyncGap)
	}
	res.Requests++
	out, err := s.batchGetID(token, idType, emails, mobiles)
	var rl *larkRateLimitError
	if errors.As(err, &rl) {
		time.Sleep(larkSyncBackoff)
		res.Requests++
		out, err = s.batchGetID(token, idType, emails, mobiles)
	}
	return out, err
}

// batchGetID 返回 e:<email> / m:<mobile> → id；未命中的不出现在结果里。
func (s *larkSender) batchGetID(token, idType string, emails, mobiles []string) (map[string]string, error) {
	payload := map[string]any{}
	if len(emails) > 0 {
		payload["emails"] = emails
	}
	if len(mobiles) > 0 {
		payload["mobiles"] = mobiles
	}
	raw, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u := s.baseURL + "/open-apis/contact/v3/users/batch_get_id?user_id_type=" + idType
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UserList []struct {
				UserID string `json:"user_id"`
				Email  string `json:"email"`
				Mobile string `json:"mobile"`
			} `json:"user_list"`
		} `json:"data"`
	}
	_ = json.Unmarshal(b, &out)
	if resp.StatusCode == http.StatusTooManyRequests || out.Code == 99991400 {
		return nil, &larkRateLimitError{detail: fmt.Sprintf("http=%d code=%d msg=%s", resp.StatusCode, out.Code, out.Msg)}
	}
	if resp.StatusCode != http.StatusOK || out.Code != 0 {
		return nil, fmt.Errorf("飞书通讯录查询失败：http=%d code=%d msg=%s（需应用权限 contact:user.id:readonly）", resp.StatusCode, out.Code, out.Msg)
	}
	ids := map[string]string{}
	for _, it := range out.Data.UserList {
		id := strings.TrimSpace(it.UserID)
		if id == "" {
			continue
		}
		if e := strings.ToLower(strings.TrimSpace(it.Email)); e != "" {
			ids["e:"+e] = id
		}
		if m := normalizeLarkMobile(it.Mobile); m != "" {
			ids["m:"+m] = id
		}
	}
	return ids, nil
}
