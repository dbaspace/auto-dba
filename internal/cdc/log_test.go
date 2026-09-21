package cdc

import (
	"testing"

	"auto-dba/internal/store"
)

func TestStepLabel(t *testing.T) {
	if got := StepLabel("pg"); got != "PG Publication/Slot" {
		t.Fatalf("pg label = %q", got)
	}
	if got := StepLabel("debezium"); got != "Debezium Connector" {
		t.Fatalf("debezium label = %q", got)
	}
	if got := StepLabel("unknown"); got != "unknown" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestPipelineLogFromStore(t *testing.T) {
	it := store.CDCSyncLog{
		ID: 3, TaskID: "t1", Level: "INFO", Step: "pg",
		Message: "ok", CreatedAt: "2026-09-11T00:00:00Z",
	}
	got := PipelineLogFromStore(it)
	if got.ID != 3 || got.Level != "INFO" || got.LogLevel != "INFO" || got.At != it.CreatedAt || got.CreatedAt != it.CreatedAt {
		t.Fatalf("unexpected %#v", got)
	}
}
