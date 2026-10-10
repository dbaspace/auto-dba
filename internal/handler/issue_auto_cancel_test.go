package handler

import (
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestIssuePendingAutoCancelDeadline(t *testing.T) {
	now := time.Date(2026, 9, 10, 17, 18, 0, 0, time.UTC)
	got := issuePendingAutoCancelDeadline(now)
	want := time.Date(2026, 9, 10, 9, 18, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("deadline=%v want=%v", got, want)
	}
}

func TestIssuePendingAutoCancelDeadlineUsesUTC(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 10, 17, 18, 0, 0, loc)
	got := issuePendingAutoCancelDeadline(now)
	if got.Location() != time.UTC {
		t.Fatalf("deadline loc=%v want UTC", got.Location())
	}
	if got.Hour() != 1 || got.Day() != 10 {
		t.Fatalf("deadline=%v want 2026-09-10T01:18:00Z", got)
	}
}

func TestCloseStalePendingIssuesNilHandler(t *testing.T) {
	var h *TicketHandler
	n, err := h.closeStalePendingIssues(t.Context(), time.Now())
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	h = &TicketHandler{}
	n, err = h.closeStalePendingIssues(t.Context(), time.Now())
	if err != nil || n != 0 {
		t.Fatalf("empty handler n=%d err=%v", n, err)
	}
}

func TestCloseStalePendingIssuesCancelsOnlyExpired(t *testing.T) {
	db := testStore(t)
	h := &TicketHandler{Store: db}
	now := time.Date(2026, 9, 10, 17, 18, 0, 0, time.UTC)

	stale := &store.Issue{
		TenantID: store.DefaultTenantID,IssueType: store.IssueTypeDDL, Description: "stale-auto-cancel-test", SQLContent: "SELECT 1", Submitter: "tester", AssignedTo: "admin", Status: store.StatusPending}
	edge := &store.Issue{
		TenantID: store.DefaultTenantID,IssueType: store.IssueTypeDDL, Description: "edge-auto-cancel-test", SQLContent: "SELECT 1", Submitter: "tester", AssignedTo: "admin", Status: store.StatusPending}
	fresh := &store.Issue{
		TenantID: store.DefaultTenantID,IssueType: store.IssueTypeDDL, Description: "fresh-auto-cancel-test", SQLContent: "SELECT 1", Submitter: "tester", AssignedTo: "admin", Status: store.StatusPending}
	for _, it := range []*store.Issue{stale, edge, fresh} {
		if err := db.CreateIssue(it); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_issues WHERE id IN (?,?,?)`, stale.ID, edge.ID, fresh.ID)
	})

	old := now.Add(-8*time.Hour - time.Minute).UTC().Format(time.RFC3339)
	exact := now.Add(-8 * time.Hour).UTC().Format(time.RFC3339)
	recent := now.Add(-time.Hour).UTC().Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE tbl_issues SET created_at=? WHERE id=?`, old, stale.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tbl_issues SET created_at=? WHERE id=?`, exact, edge.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tbl_issues SET created_at=? WHERE id=?`, recent, fresh.ID); err != nil {
		t.Fatal(err)
	}

	n, err := h.closeStalePendingIssues(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected at least 1 closed, got %d", n)
	}

	gotStale, err := db.GetIssue(stale.ID)
	if err != nil || gotStale == nil {
		t.Fatalf("stale: %v", err)
	}
	if gotStale.Status != store.StatusCanceled || gotStale.RejectReason != issuePendingAutoCancelReason {
		t.Fatalf("stale status=%s reason=%s", gotStale.Status, gotStale.RejectReason)
	}

	gotEdge, err := db.GetIssue(edge.ID)
	if err != nil || gotEdge == nil {
		t.Fatalf("edge: %v", err)
	}
	if gotEdge.Status != store.StatusPending {
		t.Fatalf("exactly 8h should stay pending, got %s", gotEdge.Status)
	}

	gotFresh, err := db.GetIssue(fresh.ID)
	if err != nil || gotFresh == nil {
		t.Fatalf("fresh: %v", err)
	}
	if gotFresh.Status != store.StatusPending {
		t.Fatalf("fresh should stay pending, got %s", gotFresh.Status)
	}
}
