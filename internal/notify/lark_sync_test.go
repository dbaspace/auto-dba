package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestLarkSyncBatchesAndRateLimit(t *testing.T) {
	larkSyncGap, larkSyncBackoff = 0, time.Millisecond
	defer func() { larkSyncGap, larkSyncBackoff = 150*time.Millisecond, 2*time.Second }()

	var mu sync.Mutex
	calls, limited := 0, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t","expire":7200}`))
		case "/open-apis/contact/v3/users/batch_get_id":
			mu.Lock()
			calls++
			first := !limited
			limited = true
			mu.Unlock()
			if first {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"code":99991400,"msg":"too many"}`))
				return
			}
			var req struct {
				Emails  []string `json:"emails"`
				Mobiles []string `json:"mobiles"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if len(req.Emails) > 50 || len(req.Mobiles) > 50 {
				t.Errorf("batch too large: emails=%d mobiles=%d", len(req.Emails), len(req.Mobiles))
			}
			typ := r.URL.Query().Get("user_id_type")
			type item struct {
				UserID string `json:"user_id,omitempty"`
				Email  string `json:"email,omitempty"`
				Mobile string `json:"mobile,omitempty"`
			}
			var list []item
			for _, e := range req.Emails {
				var n int
				if _, err := fmt.Sscanf(e, "u%d@corp.com", &n); (err == nil && n%2 == 0 && n != 0) || e == "x0@qq.com" {
					list = append(list, item{UserID: typ + "-" + e, Email: e})
				} else {
					list = append(list, item{Email: e})
				}
			}
			for _, m := range req.Mobiles {
				list = append(list, item{UserID: typ + "-" + m, Mobile: m})
			}
			b, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"user_list": list}})
			_, _ = w.Write(b)
		default:
			_, _ = w.Write([]byte(`{"code":0,"data":{"items":[]}}`))
		}
	}))
	defer srv.Close()

	users := []store.User{
		{ID: "admin", Username: "admin", Email: "admin@corp.com", HasLoggedIn: true},
		{ID: "never", Username: "never", Email: "never@corp.com"},
		{ID: "nokey", Username: "nokey", HasLoggedIn: true},
	}
	for i := 0; i < 30; i++ {
		u := store.User{ID: fmt.Sprintf("id%d", i), Username: fmt.Sprintf("u%d", i), Email: fmt.Sprintf("u%d@corp.com", i), HasLoggedIn: true}
		if i == 0 {
			u.LarkEmail = "x0@qq.com"
		}
		if i == 1 {
			u.Phone = "+86 13800000001"
		}
		users = append(users, u)
	}
	saved := map[string][3]string{}
	save := func(id, uid, oid, email string, _ time.Time) error {
		saved[id] = [3]string{uid, oid, email}
		return nil
	}
	s := &larkSender{appID: "a", appSecret: "b", baseURL: srv.URL}
	res, err := s.syncUsers(save, users)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 30 || res.NotLoggedIn != 1 || strings.Join(res.NoKey, ",") != "nokey" {
		t.Fatalf("unexpected counts: %+v", res)
	}
	if calls != 5 || res.Requests != 7 || res.ScannedPages != 2 {
		t.Fatalf("calls=%d requests=%d pages=%d want 5/7/2", calls, res.Requests, res.ScannedPages)
	}
	// 偶数 2..28 共 14 人 + u0（飞书邮箱）+ u1（手机号）
	if res.Synced != 16 || len(res.Unmatched) != 14 {
		t.Fatalf("synced=%d unmatched=%d", res.Synced, len(res.Unmatched))
	}
	if got := saved["id0"]; got != [3]string{"user_id-x0@qq.com", "open_id-x0@qq.com", "x0@qq.com"} {
		t.Fatalf("u0 saved %v", got)
	}
	if got := saved["id1"]; got[0] != "user_id-13800000001" || got[2] != "" {
		t.Fatalf("u1 saved %v", got)
	}
	if _, ok := saved["admin"]; ok {
		t.Fatal("admin must be skipped")
	}
}

func TestLarkSyncStopsAfterRepeatedRateLimit(t *testing.T) {
	larkSyncGap, larkSyncBackoff = 0, time.Millisecond
	defer func() { larkSyncGap, larkSyncBackoff = 150*time.Millisecond, 2*time.Second }()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal") {
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t","expire":7200}`))
			return
		}
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":99991400,"msg":"too many"}`))
	}))
	defer srv.Close()
	var users []store.User
	for i := 0; i < 60; i++ {
		users = append(users, store.User{ID: fmt.Sprint(i), Username: fmt.Sprint("u", i), Email: fmt.Sprintf("u%d@corp.com", i), HasLoggedIn: true})
	}
	s := &larkSender{appID: "a", appSecret: "b", baseURL: srv.URL}
	res, err := s.syncUsers(func(string, string, string, string, time.Time) error { return nil }, users)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(res.Skipped) != 60 || !strings.Contains(res.Message, "限流") {
		t.Fatalf("calls=%d skipped=%d msg=%q", calls, len(res.Skipped), res.Message)
	}
}
