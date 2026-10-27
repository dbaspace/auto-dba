package handler

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/sqlcheck"
	"auto-dba/internal/sqlexec"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

func TestScheduledInFuture(t *testing.T) {
	if scheduledInFuture("") || scheduledInFuture("   ") {
		t.Fatal("empty should not be future")
	}
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	if scheduledInFuture(past) {
		t.Fatal("past should not block auto exec")
	}
	future := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	if !scheduledInFuture(future) {
		t.Fatal("future scheduled_at should delay auto exec")
	}
}

func TestBeginAsyncAutoExecNilGuard(t *testing.T) {
	var h *TicketHandler
	if err := h.beginAsyncAutoExec(nil); err == nil {
		t.Fatal("nil handler should error")
	}
	h = &TicketHandler{}
	if err := h.beginAsyncAutoExec(nil); err == nil {
		t.Fatal("nil issue should error")
	}
}

func TestFailAsyncIssueNilSafe(t *testing.T) {
	var h *TicketHandler
	h.failAsyncIssue("x", "msg") // must not panic
	h = &TicketHandler{}
	h.failAsyncIssue("", "msg")
	h.failAsyncIssue("id", "msg")
}

func TestBeginAsyncAutoExecReturnsImmediately(t *testing.T) {
	db := testStore(t)
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		TenantID:    store.DefaultTenantID,
		IssueType:   store.IssueTypeDDL,
		Description: "async-approve-fast-return",
		SQLContent:  "ALTER TABLE t ADD COLUMN c int",
		Submitter:   "tester",
		AssignedTo:  "admin",
		Status:      store.StatusApproved,
		// 无实例：后台会快速 persist 失败，但 begin 必须先返回 running
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })

	start := time.Now()
	if err := h.beginAsyncAutoExec(it); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("beginAsyncAutoExec blocked too long: %v", time.Since(start))
	}
	if it.Status != store.StatusRunning {
		t.Fatalf("status=%s want running", it.Status)
	}
	cur, err := db.GetIssue(it.ID)
	if err != nil || cur == nil {
		t.Fatal(err)
	}
	if cur.Status != store.StatusRunning && cur.Status != store.StatusFailed {
		t.Fatalf("unexpected status right after begin: %s", cur.Status)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		cur, err = db.GetIssue(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cur.Status != store.StatusRunning {
			if cur.Status != store.StatusFailed {
				t.Fatalf("final status=%s want failed", cur.Status)
			}
			if strings.TrimSpace(cur.ExecuteResult) == "" {
				t.Fatal("failed async path should write execute_result")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("ticket stuck in running")
}

func TestFailAsyncIssueWritesRecord(t *testing.T) {
	db := testStore(t)
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		TenantID:    store.DefaultTenantID,
		IssueType:   store.IssueTypeDML,
		Description: "async-fail-record",
		SQLContent:  "UPDATE t SET a=1",
		DBName:      "d1",
		Submitter:   "tester",
		AssignedTo:  "admin",
		Status:      store.StatusRunning,
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })

	h.failAsyncIssue(it.ID, "context deadline exceeded")
	cur, err := db.GetIssue(it.ID)
	if err != nil || cur == nil {
		t.Fatal(err)
	}
	if cur.Status != store.StatusFailed {
		t.Fatalf("status=%s", cur.Status)
	}
	res := ticketexec.Parse(cur.ExecuteResult)
	if res == nil || res.Message != "context deadline exceeded" || res.Summary == res.Message {
		t.Fatalf("%+v", res)
	}
	if len(res.Records) != 1 || res.Records[0].Status != "failed" ||
		res.Records[0].ErrorMessage != res.Message || res.Records[0].DB != "d1" {
		t.Fatalf("records %+v", res.Records)
	}
}

func TestRemoteGetGiveUp(t *testing.T) {
	if remoteGetGiveUp(1, errors.New("地址不通: timeout")) {
		t.Fatal("one timeout should keep polling")
	}
	if !remoteGetGiveUp(remoteGetFailLimit, errors.New("地址不通: timeout")) {
		t.Fatal("limit should fail the ticket")
	}
	if !remoteGetGiveUp(1, fmt.Errorf("执行器返回: %w", sqlexec.ErrJobNotFound)) {
		t.Fatal("missing job should fail immediately")
	}
}

func TestReclaimOrphanRunning(t *testing.T) {
	db := testStore(t)
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		TenantID:    store.DefaultTenantID,
		IssueType:   store.IssueTypeDML,
		Description: "orphan-running",
		SQLContent:  "UPDATE t SET a=1",
		DBName:      "d1",
		Submitter:   "tester",
		AssignedTo:  "admin",
		Status:      store.StatusRunning,
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })
	past := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE tbl_issues SET updated_at=? WHERE id=?`, past, it.ID); err != nil {
		t.Fatal(err)
	}

	live := &store.Issue{
		TenantID:    store.DefaultTenantID,
		IssueType:   store.IssueTypeDML,
		Description: "live-running",
		SQLContent:  "UPDATE t SET a=2",
		DBName:      "d1",
		Submitter:   "tester",
		AssignedTo:  "admin",
		Status:      store.StatusRunning,
	}
	if err := db.CreateIssue(live); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		unbindExecCancel(live.ID)
		_, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, live.ID)
	})
	markExecLive(live.ID)

	if n := h.ReclaimOrphanRunning(false); n < 1 {
		t.Fatalf("reclaimed %d", n)
	}
	cur, err := db.GetIssue(it.ID)
	if err != nil || cur == nil || cur.Status != store.StatusFailed {
		t.Fatalf("orphan status=%v err=%v", cur, err)
	}
	res := ticketexec.Parse(cur.ExecuteResult)
	if res == nil || len(res.Records) != 1 || !strings.Contains(res.Records[0].ErrorMessage, "执行进程已不在") {
		t.Fatalf("result %+v", res)
	}
	kept, err := db.GetIssue(live.ID)
	if err != nil || kept == nil || kept.Status != store.StatusRunning {
		t.Fatalf("live ticket changed: %+v err=%v", kept, err)
	}
}

func TestReviewGateResult(t *testing.T) {
	res := reviewGateResult(sqlcheck.Result{
		OK: false,
		Statements: []sqlcheck.StatementRow{
			{ErrorLevel: "ERROR", Message: "禁止 TRUNCATE", SQL: "TRUNCATE t", RuleID: "DDLForbidTruncate"},
		},
	})
	if res.Success || len(res.Records) != 1 || res.Records[0].ErrorMessage != "禁止 TRUNCATE" {
		t.Fatalf("%+v", res)
	}
}
