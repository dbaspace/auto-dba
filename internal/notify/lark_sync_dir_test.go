package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestLarkSyncDirectoryMatchesEnterpriseEmailAndStops(t *testing.T) {
	larkSyncGap, larkSyncBackoff = 0, time.Millisecond
	defer func() { larkSyncGap, larkSyncBackoff = 150*time.Millisecond, 2*time.Second }()

	userPages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal"):
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t","expire":7200}`))
		case r.URL.Path == "/open-apis/contact/v3/users/batch_get_id":
			_, _ = w.Write([]byte(`{"code":0,"data":{"user_list":[{"email":"a@corp.com"}]}}`))
		case strings.Contains(r.URL.Path, "/departments/"):
			_, _ = w.Write([]byte(`{"code":0,"data":{"has_more":false,"items":[{"open_department_id":"od_1"}]}}`))
		case r.URL.Path == "/open-apis/contact/v3/users/find_by_department":
			userPages++
			dept := r.URL.Query().Get("department_id")
			if dept == "0" {
				_, _ = w.Write([]byte(`{"code":0,"data":{"has_more":false,"items":[{"user_id":"u_other","open_id":"ou_other","enterprise_email":"other@corp.com"}]}}`))
				return
			}
			if userPages > 2 {
				t.Errorf("should stop after match, page %d", userPages)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"has_more":true,"page_token":"p2","items":[{"user_id":"u_a","open_id":"ou_a","email":"a@qq.com","enterprise_email":"a@corp.com"}]}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	s := &larkSender{appID: "a", appSecret: "b", baseURL: srv.URL}
	saved := map[string][3]string{}
	res, err := s.syncUsers(func(id, uid, oid, email string, _ time.Time) error {
		saved[id] = [3]string{uid, oid, email}
		return nil
	}, []store.User{{ID: "1", Username: "a", Email: "a@corp.com", HasLoggedIn: true}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Synced != 1 || len(res.Unmatched) != 0 || userPages != 2 {
		t.Fatalf("synced=%d unmatched=%v pages=%d", res.Synced, res.Unmatched, userPages)
	}
	if saved["1"] != [3]string{"u_a", "ou_a", "a@qq.com"} {
		t.Fatalf("saved %v", saved["1"])
	}
	if res.ScannedPages != 3 {
		t.Fatalf("scanned_pages=%d want 3", res.ScannedPages)
	}
}

func TestLarkSyncDirectoryRateLimitStops(t *testing.T) {
	larkSyncGap, larkSyncBackoff = 0, time.Millisecond
	defer func() { larkSyncGap, larkSyncBackoff = 150*time.Millisecond, 2*time.Second }()
	dirCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal") {
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t","expire":7200}`))
			return
		}
		if r.URL.Path == "/open-apis/contact/v3/users/batch_get_id" {
			_, _ = w.Write([]byte(`{"code":0,"data":{"user_list":[]}}`))
			return
		}
		dirCalls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":99991400,"msg":"too many"}`))
	}))
	defer srv.Close()
	s := &larkSender{appID: "a", appSecret: "b", baseURL: srv.URL}
	users := []store.User{{ID: "1", Username: "a", Email: "a@corp.com", HasLoggedIn: true}}
	res, err := s.syncUsers(func(string, string, string, string, time.Time) error { return nil }, users)
	if err != nil {
		t.Fatal(err)
	}
	if dirCalls != 2 || !strings.Contains(res.Message, "限流") || len(res.Unmatched) != 1 {
		t.Fatalf("dirCalls=%d msg=%q unmatched=%v", dirCalls, res.Message, res.Unmatched)
	}
}

func TestLarkSyncDirectoryPageCap(t *testing.T) {
	larkSyncGap = 0
	old := larkSyncMaxPages
	larkSyncMaxPages = 2
	defer func() { larkSyncGap = 150 * time.Millisecond; larkSyncMaxPages = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal") {
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t","expire":7200}`))
			return
		}
		if r.URL.Path == "/open-apis/contact/v3/users/batch_get_id" {
			_, _ = w.Write([]byte(`{"code":0,"data":{"user_list":[]}}`))
			return
		}
		if strings.Contains(r.URL.Path, "/departments/") {
			_, _ = w.Write([]byte(`{"code":0,"data":{"has_more":true,"page_token":"next","items":[{"open_department_id":"od_1"}]}}`))
			return
		}
		t.Errorf("user page should not be reached: %s", r.URL.Path)
	}))
	defer srv.Close()
	s := &larkSender{appID: "a", appSecret: "b", baseURL: srv.URL}
	res, err := s.syncUsers(func(string, string, string, string, time.Time) error { return nil }, []store.User{
		{ID: "1", Username: "a", Email: "a@corp.com", HasLoggedIn: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ScannedPages != 2 || !strings.Contains(res.Message, "超过 2 页") || len(res.Unmatched) != 1 {
		t.Fatalf("pages=%d msg=%q unmatched=%v", res.ScannedPages, res.Message, res.Unmatched)
	}
}
