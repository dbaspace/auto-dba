package cdc

import "testing"

func TestCheckMySQLServerIDConflictNilSafe(t *testing.T) {
	o := &Orchestrator{}
	if err := o.checkMySQLServerIDConflict(nil, 5400); err != nil {
		t.Fatal(err)
	}
}
