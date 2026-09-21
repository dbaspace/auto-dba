package store

import "testing"

func TestBuildCDCLogWhere(t *testing.T) {
	where, args := buildCDCLogWhere(CDCLogFilter{TaskID: "t1", IncludeChildTaskLogs: true, LogLevel: "ERROR", Q: "pub"})
	if where == "" {
		t.Fatal("expected where clause")
	}
	if len(args) != 6 {
		t.Fatalf("args=%d want 6: %#v", len(args), args)
	}
	if args[0] != "t1" || args[1] != "t1" || args[2] != "ERROR" {
		t.Fatalf("unexpected args %#v", args)
	}
}
