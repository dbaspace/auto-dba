package handler

import (
	"testing"
	"time"

	"auto-dba/internal/sqlcheck"
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
