package notify

import (
	"os"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/store"
	"net/http"
	"net/http/httptest"
)

func TestNotifyApproverWritesQueryAudit(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to a postgres DSN to run notify audit tests")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	prev, err := db.GetNotifyConfig()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.SetNotifyConfig(prev) })
	if err := db.SetNotifyConfig(&store.NotifyConfig{LarkOnTicketCreate: true}); err != nil {
		t.Fatal(err)
	}

	suffix := strings.ReplaceAll(time.Now().UTC().Format("150405.000"), ".", "")
	okUser := &store.User{Username: "notifyok" + suffix, DisplayName: "通知成功"}
	badUser := &store.User{Username: "notifybad" + suffix, DisplayName: "通知失败"}
	if err := db.CreateUser(okUser, "x"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(badUser, "x"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserLarkIdentity(okUser.ID, "uid_ok", "ou_ok", "ok@qq.com", time.Now()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id IN (?,?)`, okUser.ID, badUser.ID)
		_, _ = db.Exec(`DELETE FROM tbl_users WHERE id IN (?,?)`, okUser.ID, badUser.ID)
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t","expire":7200}`))
		case "/open-apis/im/v1/messages":
			_, _ = w.Write([]byte(`{"code":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	globalReg.mu.Lock()
	old := globalReg.lark
	globalReg.lark = &larkSender{appID: "a", appSecret: "b", baseURL: srv.URL}
	globalReg.mu.Unlock()
	t.Cleanup(func() {
		globalReg.mu.Lock()
		globalReg.lark = old
		globalReg.mu.Unlock()
	})

	issue := &store.Issue{
		IssueType:   store.IssueTypeCreateDB,
		TenantID:    store.DefaultTenantID,
		Submitter:   "admin",
		AssignedTo:  strings.ToUpper(okUser.Username) + "," + badUser.Username,
		DBName:      "db_boh_operation",
		Description: "notify audit",
		Status:      store.StatusPending,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_query_audits WHERE instance_name=?`, issue.TicketNo)
		_, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, issue.ID)
	})

	IssueCreated(db, "", issue)

	rows, _, err := db.ListQueryAudits(store.QueryAuditFilter{Kind: "notify", Q: issue.TicketNo, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	var okRow, badRow *store.QueryAudit
	for i := range rows {
		row := &rows[i]
		if row.InstanceName != issue.TicketNo || row.Kind != "notify" || row.ClientType != "notify" {
			continue
		}
		switch strings.ToLower(row.Username) {
		case strings.ToLower(okUser.Username):
			okRow = row
		case strings.ToLower(badUser.Username):
			badRow = row
		}
	}
	if okRow == nil || !okRow.Success || okRow.Target != "lark:open_id:ou_ok" || okRow.DisplayName != okUser.DisplayName {
		t.Fatalf("success row=%+v", okRow)
	}
	if badRow == nil || badRow.Success || !strings.Contains(badRow.Message, "无飞书身份") {
		t.Fatalf("failure row=%+v", badRow)
	}
}
