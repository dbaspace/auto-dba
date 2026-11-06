package dbmig

import (
	"context"
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestScheduleDueAction(t *testing.T) {
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	raw := at.Format(time.RFC3339)
	if got := ScheduleDueAction(raw, at.Add(5*time.Minute), 10); got != "start" {
		t.Fatalf("within window: %s", got)
	}
	if got := ScheduleDueAction(raw, at.Add(11*time.Minute), 10); got != "missed" {
		t.Fatalf("past window: %s", got)
	}
	if got := ScheduleDueAction(raw, at.Add(-time.Minute), 10); got != "wait" {
		t.Fatalf("future: %s", got)
	}
	if got := ScheduleDueAction("bad", at, 10); got != "skip" {
		t.Fatalf("bad time: %s", got)
	}
}

func TestPrecheckOSSRejectsNonPostgres(t *testing.T) {
	items := Precheck(context.Background(), nil, "", &store.MigrationTask{DBType: "mysql", TransferMode: "oss"})
	if PrecheckPassed(items) {
		t.Fatal("mysql oss should fail precheck")
	}
}
