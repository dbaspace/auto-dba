package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type larkContactUser struct {
	UserID          string `json:"user_id"`
	OpenID          string `json:"open_id"`
	Email           string `json:"email"`
	EnterpriseEmail string `json:"enterprise_email"`
}

// directorySender 通讯录读国际版域名。测试里 baseURL 不是飞书国内域名时沿用原地址。
func (s *larkSender) directorySender() *larkSender {
	if s.baseURL == "" || s.baseURL == "https://open.feishu.cn" {
		return &larkSender{appID: s.appID, appSecret: s.appSecret, baseURL: larkDirectoryBase}
	}
	return s
}

// fillByDirectory 点查未命中的用户，分页读通讯录，按企业邮箱或名片邮箱比对。全部对上即停。
func (s *larkSender) fillByDirectory(pending []larkSyncUser, res *LarkSyncResult, save larkIdentitySaver, now time.Time) {
	left := map[int]struct{}{}
	byEmail := map[string][]int{}
	for i, it := range pending {
		left[i] = struct{}{}
		for _, k := range it.keys {
			if strings.HasPrefix(k, "e:") {
				byEmail[strings.TrimPrefix(k, "e:")] = append(byEmail[strings.TrimPrefix(k, "e:")], i)
			}
		}
	}
	if len(byEmail) == 0 {
		for _, it := range pending {
			res.Unmatched = append(res.Unmatched, it.user.Username)
		}
		return
	}
	dir := s.directorySender()
	token, err := dir.accessToken()
	if err != nil {
		res.Message = "飞书通讯录不可用：" + err.Error()
		for _, it := range pending {
			res.Unmatched = append(res.Unmatched, it.user.Username)
		}
		return
	}
	depts, err := dir.listDeptIDs(token, res)
	if err != nil {
		res.Message = err.Error()
		for i := range left {
			res.Unmatched = append(res.Unmatched, pending[i].user.Username)
		}
		return
	}
	for _, dept := range append([]string{"0"}, depts...) {
		if len(left) == 0 || res.Message != "" {
			break
		}
		if err := dir.scanDept(token, dept, byEmail, left, pending, res, save, now); err != nil {
			res.Message = err.Error()
			break
		}
	}
	for i := range left {
		res.Unmatched = append(res.Unmatched, pending[i].user.Username)
	}
}

func (s *larkSender) listDeptIDs(token string, res *LarkSyncResult) ([]string, error) {
	var ids []string
	page := ""
	for {
		if res.ScannedPages >= larkSyncMaxPages {
			return ids, fmt.Errorf("通讯录超过 %d 页，已停止", larkSyncMaxPages)
		}
		q := fmt.Sprintf("/open-apis/contact/v3/departments/0/children?fetch_child=true&page_size=%d&department_id_type=open_department_id", larkSyncPageSize)
		if page != "" {
			q += "&page_token=" + url.QueryEscape(page)
		}
		raw, err := s.getRetry(token, q, res)
		if err != nil {
			return nil, err
		}
		var out struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				HasMore   bool   `json:"has_more"`
				PageToken string `json:"page_token"`
				Items     []struct {
					OpenDepartmentID string `json:"open_department_id"`
				} `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		if out.Code != 0 {
			return nil, larkDirectoryDenied(out.Code, out.Msg)
		}
		for _, it := range out.Data.Items {
			if id := strings.TrimSpace(it.OpenDepartmentID); id != "" {
				ids = append(ids, id)
			}
		}
		if !out.Data.HasMore || out.Data.PageToken == "" {
			return ids, nil
		}
		page = out.Data.PageToken
	}
}

func (s *larkSender) scanDept(token, dept string, byEmail map[string][]int, left map[int]struct{}, pending []larkSyncUser, res *LarkSyncResult, save larkIdentitySaver, now time.Time) error {
	page := ""
	for {
		if len(left) == 0 {
			return nil
		}
		if res.ScannedPages >= larkSyncMaxPages {
			return fmt.Errorf("通讯录超过 %d 页，已停止", larkSyncMaxPages)
		}
		q := fmt.Sprintf("/open-apis/contact/v3/users/find_by_department?department_id=%s&user_id_type=user_id&page_size=%d", url.QueryEscape(dept), larkSyncPageSize)
		if dept == "0" {
			q += "&department_id_type=department_id"
		} else {
			q += "&department_id_type=open_department_id"
		}
		if page != "" {
			q += "&page_token=" + url.QueryEscape(page)
		}
		raw, err := s.getRetry(token, q, res)
		if err != nil {
			return err
		}
		var out struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				HasMore   bool              `json:"has_more"`
				PageToken string            `json:"page_token"`
				Items     []larkContactUser `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		if out.Code != 0 {
			return larkDirectoryDenied(out.Code, out.Msg)
		}
		for _, u := range out.Data.Items {
			if strings.TrimSpace(u.UserID) == "" && strings.TrimSpace(u.OpenID) == "" {
				continue
			}
			for _, addr := range []string{u.EnterpriseEmail, u.Email} {
				addr = strings.ToLower(strings.TrimSpace(addr))
				for _, idx := range byEmail[addr] {
					if _, ok := left[idx]; !ok {
						continue
					}
					email := strings.TrimSpace(u.Email)
					if email == "" {
						email = strings.TrimSpace(u.EnterpriseEmail)
					}
					if err := save(pending[idx].user.ID, u.UserID, u.OpenID, email, now); err != nil {
						res.Skipped = append(res.Skipped, pending[idx].user.Username)
					} else {
						res.Synced++
					}
					delete(left, idx)
				}
			}
		}
		if !out.Data.HasMore || out.Data.PageToken == "" {
			return nil
		}
		page = out.Data.PageToken
	}
}

func larkDirectoryDenied(code int, msg string) error {
	return fmt.Errorf("飞书通讯录读取失败：code=%d msg=%s（需通讯录权限：部门、用户基本信息、邮箱、企业邮箱、user_id，且可用范围包含要同步的用户）", code, msg)
}

func (s *larkSender) getRetry(token, path string, res *LarkSyncResult) ([]byte, error) {
	if res.Requests > 0 {
		time.Sleep(larkSyncGap)
	}
	res.Requests++
	res.ScannedPages++
	raw, status, err := s.getRaw(token, path)
	if err != nil {
		return nil, err
	}
	if larkHTTPLimited(status, raw) {
		time.Sleep(larkSyncBackoff)
		if res.ScannedPages >= larkSyncMaxPages {
			return nil, fmt.Errorf("通讯录超过 %d 页，已停止", larkSyncMaxPages)
		}
		res.Requests++
		res.ScannedPages++
		raw, status, err = s.getRaw(token, path)
		if err != nil {
			return nil, err
		}
		if larkHTTPLimited(status, raw) {
			return nil, &larkRateLimitError{detail: fmt.Sprintf("http=%d", status)}
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("飞书通讯录读取失败：http=%d", status)
	}
	return raw, nil
}

func (s *larkSender) getRaw(token, path string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return b, resp.StatusCode, nil
}

func larkHTTPLimited(status int, raw []byte) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	var out struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(raw, &out)
	return out.Code == 99991400
}
