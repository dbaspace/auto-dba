package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiffCmdbPayload(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"tags": []string{"a"}, "visibility": "global", "approvers": []string{"u1"}})
	c, _ := json.Marshal(map[string]any{"tags": []string{}, "visibility": "global", "approvers": []string{}})
	diffs := DiffCmdbPayload(b, c)
	fields := map[string]bool{}
	for _, d := range diffs {
		fields[d.Field] = true
	}
	if !fields["tags"] || !fields["approvers"] {
		t.Fatalf("expected tags/approvers drift, got %+v", diffs)
	}
	if fields["visibility"] {
		t.Fatalf("visibility should not drift")
	}
}

func TestSnapshotInstanceMetaOmitsPassword(t *testing.T) {
	it := &Instance{ID: "x", Name: "n", Password: "secret", Host: "h", Port: 1, Tags: []string{"t"}}
	m := SnapshotInstanceMeta(it)
	raw, _ := json.Marshal(m)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "password") {
		t.Fatalf("password leaked: %s", raw)
	}
}
