package store

import "testing"

func TestExecProgressFraction(t *testing.T) {
	if _, ok := (ExecProgress{Phase: "preparing"}).Fraction(); ok {
		t.Fatal("preparing is indeterminate")
	}
	got, ok := (ExecProgress{Phase: "running", Total: 1, StmtIndex: 12, StmtTotal: 156}).Fraction()
	if !ok || got < 12.0/156-0.001 || got > 12.0/156+0.001 {
		t.Fatalf("single db fraction=%v ok=%v", got, ok)
	}
	got, ok = (ExecProgress{Phase: "running", Total: 24, Done: 6, StmtIndex: 3, StmtTotal: 8}).Fraction()
	want := (6 + 3.0/8) / 24
	if !ok || got < want-0.001 || got > want+0.001 {
		t.Fatalf("multi db fraction=%v want=%v", got, want)
	}
}
