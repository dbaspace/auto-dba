package handler

import (
	"testing"

	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

func TestAsyncExecMaxWait(t *testing.T) {
	cic := "CREATE INDEX CONCURRENTLY idx_a ON t(a);"
	ddl := &store.Issue{
		IssueType: store.IssueTypeDDL, InstanceID: "i1", SQLContent: cic,
		NeedRollback: `{"data_source_ids":["i1","i2"]}`,
	}
	if got, want := asyncExecMaxWait(nil, ddl), ticketexec.AsyncAutoExecMaxWait+2*ticketexec.DefaultLongStmtTimeout; got != want {
		t.Fatalf("两个数据源应追加两倍长语句超时: got %v want %v", got, want)
	}
	dml := &store.Issue{IssueType: store.IssueTypeDML, InstanceID: "i1", SQLContent: "UPDATE t SET a=1 WHERE id=1"}
	if got := asyncExecMaxWait(nil, dml); got != ticketexec.AsyncAutoExecMaxWait {
		t.Fatalf("不含长语句保持原上限: %v", got)
	}
}
