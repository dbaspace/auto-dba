package notify

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"auto-dba/internal/mail"
	"auto-dba/internal/store"
)

func TestEmailProblemsDoNotBlockLark(t *testing.T) {
	user := &store.User{
		ID: "u-iso", Username: "chenjiachi", DisplayName: "陈加持",
		Email: "chenjiachi@example.com", LarkOpenID: "ou_iso", LarkID: "uid_iso",
	}
	var mu sync.Mutex
	var rows []store.QueryAudit
	oldFind, oldSave, oldLoad, oldSend, oldSMTP := findNotifyUser, saveNotifyAudit, loadTicketNotify, sendMail, loadTicketSMTP
	t.Cleanup(func() {
		findNotifyUser, saveNotifyAudit, loadTicketNotify, sendMail, loadTicketSMTP = oldFind, oldSave, oldLoad, oldSend, oldSMTP
	})
	findNotifyUser = func(*store.DB, string) (*store.User, error) { return user, nil }
	saveNotifyAudit = func(_ *store.DB, row *store.QueryAudit) error {
		if row == nil {
			return nil
		}
		mu.Lock()
		rows = append(rows, *row)
		mu.Unlock()
		return nil
	}
	loadTicketNotify = func(*store.DB) (*store.NotifyConfig, bool, error) {
		return &store.NotifyConfig{EmailOnTicketCreate: true, LarkOnTicketCreate: true}, false, nil
	}

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
	t.Cleanup(srv.Close)
	globalReg.mu.Lock()
	oldLark := globalReg.lark
	globalReg.lark = &larkSender{appID: "a", appSecret: "b", baseURL: srv.URL}
	globalReg.mu.Unlock()
	t.Cleanup(func() {
		globalReg.mu.Lock()
		globalReg.lark = oldLark
		globalReg.mu.Unlock()
	})

	readySMTP := func(*store.DB, string, string, string) (*store.SMTPConfig, mail.Config, bool) {
		return &store.SMTPConfig{Enabled: true, Host: "127.0.0.1", From: "from@example.com"},
			mail.Config{Host: "127.0.0.1", From: "from@example.com", Rate: 1000}, true
	}

	t.Run("hang", func(t *testing.T) {
		release := make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }) })
		sendMail = func(mail.Config, string, string, string, string) error {
			<-release
			return fmt.Errorf("SMTP 超时")
		}
		loadTicketSMTP = readySMTP
		issue := &store.Issue{TicketNo: "Thang", IssueType: store.IssueTypeDQL, AssignedTo: user.Username, Submitter: "admin"}
		start := time.Now()
		IssueCreated(nil, "", issue)
		waitNotifyRows(t, &mu, &rows, issue.TicketNo, start, func(got []store.QueryAudit) bool {
			return hasLarkSuccess(got, user.Username)
		})
		once.Do(func() { close(release) })
		waitNotifyRows(t, &mu, &rows, issue.TicketNo, start, func(got []store.QueryAudit) bool {
			return hasEmailFailure(got, "SMTP 超时")
		})
	})

	t.Run("smtp-not-ready", func(t *testing.T) {
		sendMail = func(mail.Config, string, string, string, string) error {
			t.Fatal("SMTP 未就绪时不应发送邮件")
			return nil
		}
		loadTicketSMTP = func(*store.DB, string, string, string) (*store.SMTPConfig, mail.Config, bool) {
			return nil, mail.Config{}, false
		}
		issue := &store.Issue{TicketNo: "Tnotready", IssueType: store.IssueTypeDQL, AssignedTo: user.Username, Submitter: "admin"}
		start := time.Now()
		IssueCreated(nil, "", issue)
		waitNotifyRows(t, &mu, &rows, issue.TicketNo, start, func(got []store.QueryAudit) bool {
			return hasLarkSuccess(got, user.Username) && hasEmailFailure(got, "SMTP 未就绪")
		})
	})

	t.Run("panic", func(t *testing.T) {
		sendMail = func(mail.Config, string, string, string, string) error {
			panic("smtp boom")
		}
		loadTicketSMTP = readySMTP
		issue := &store.Issue{TicketNo: "Tpanic", IssueType: store.IssueTypeDQL, AssignedTo: user.Username, Submitter: "admin"}
		start := time.Now()
		IssueCreated(nil, "", issue)
		waitNotifyRows(t, &mu, &rows, issue.TicketNo, start, func(got []store.QueryAudit) bool {
			return hasLarkSuccess(got, user.Username) && hasEmailFailure(got, "邮件发送异常")
		})
	})
}

func waitNotifyRows(t *testing.T, mu *sync.Mutex, rows *[]store.QueryAudit, ticket string, start time.Time, ok func([]store.QueryAudit) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last []store.QueryAudit
	for time.Now().Before(deadline) {
		mu.Lock()
		last = nil
		for _, row := range *rows {
			if row.InstanceName == ticket {
				last = append(last, row)
			}
		}
		mu.Unlock()
		if ok(last) {
			if time.Since(start) > 3*time.Second {
				t.Fatalf("channel waited too long: %s", time.Since(start))
			}
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("notify rows timeout ticket=%s rows=%+v", ticket, last)
}

func hasLarkSuccess(rows []store.QueryAudit, username string) bool {
	for _, row := range rows {
		if row.Success && strings.HasPrefix(row.Target, "lark:") && strings.EqualFold(row.Username, username) {
			return true
		}
	}
	return false
}

func hasEmailFailure(rows []store.QueryAudit, msg string) bool {
	for _, row := range rows {
		if !row.Success && strings.HasPrefix(row.Target, "email:") && strings.Contains(row.Message, msg) {
			return true
		}
	}
	return false
}
