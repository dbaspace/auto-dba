package runbook

import (
	"testing"

	"auto-dba/internal/store"
)

func TestPreviewIncludesClaim(t *testing.T) {
	svc := New(nil)
	a := &store.Alert{
		ID: "a1", Severity: "P1", Title: "PostgreSQL 连接数使用率过高",
		Labels: map[string]string{"alertname": "PGConnHigh"},
	}
	p := svc.Preview(a)
	if len(p.Suggestions) < 3 {
		t.Fatalf("suggestions=%d", len(p.Suggestions))
	}
	foundDraft := false
	for _, s := range p.Suggestions {
		if s.Action == ActionKillSession && !s.Executable {
			foundDraft = true
		}
	}
	if !foundDraft {
		t.Fatal("expected kill_session draft suggestion")
	}
}
