package aiops

import (
	"testing"

	"auto-dba/internal/store"
)

func TestGroupKeyForAlert(t *testing.T) {
	a := &store.Alert{Labels: map[string]string{"instance_id": "i1"}, Service: "postgres"}
	if got := GroupKeyForAlert(nil, a); got != "inst:i1" {
		t.Fatalf("got %s", got)
	}
	a2 := &store.Alert{Labels: map[string]string{"alertname": "PGConnHigh"}, Service: "postgres"}
	if got := GroupKeyForAlert(nil, a2); got != "name:PGConnHigh" {
		t.Fatalf("got %s", got)
	}
}
