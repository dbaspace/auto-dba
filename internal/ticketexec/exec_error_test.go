package ticketexec

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgconn"
)

func TestFormatExecErrPgDetail(t *testing.T) {
	pe := &pgconn.PgError{
		Severity:       "ERROR",
		Code:           "23505",
		Message:        `duplicate key value violates unique constraint "t_pkey"`,
		Detail:         "Key (id)=(1) already exists.",
		SchemaName:     "public",
		TableName:      "t",
		ConstraintName: "t_pkey",
	}
	got := formatExecErr(fmt.Errorf("执行第 2 条 SQL 失败: %w", pe))
	for _, want := range []string{
		"执行第 2 条 SQL 失败",
		"SQLSTATE 23505",
		"详情: Key (id)=(1) already exists.",
		"对象: 表 public.t，约束 t_pkey",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}

	syntax := &pgconn.PgError{Severity: "ERROR", Code: "42601", Message: "syntax error", Position: 15, Hint: "check"}
	got = formatExecErr(syntax)
	if !strings.Contains(got, "位置: 第 15 个字符") || !strings.Contains(got, "提示: check") {
		t.Fatalf("got %q", got)
	}
}

func TestFormatExecErrPlain(t *testing.T) {
	if got := formatExecErr(errors.New("boom")); got != "boom" {
		t.Fatalf("got %q", got)
	}
	if got := formatExecErr(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestMarkRolledBack(t *testing.T) {
	tg := Target{Host: "h", DBName: "d"}
	r := &Result{}
	r.appendSuccess(tg, "update t set a=1", time.Now(), time.Now(), 3)
	r.appendSuccess(tg, "update t set a=2", time.Now(), time.Now(), 4)
	r.appendFailure(tg, "update t set a=x", time.Now(), errors.New("bad"))
	n := r.markRolledBack()
	if n != 2 || r.SuccessCount != 0 || !r.RolledBack || len(r.SuccessSQLs) != 0 {
		t.Fatalf("n=%d %+v", n, r)
	}
	var rb, failed int
	for _, rec := range r.Records {
		switch rec.Status {
		case "rolled_back":
			rb++
			if rec.AffectedRow == 0 {
				t.Fatal("rolled_back should keep affected rows for reference")
			}
		case "failed":
			failed++
		}
	}
	if rb != 2 || failed != 1 {
		t.Fatalf("rb=%d failed=%d", rb, failed)
	}
	if st := r.targetHit[targetKey("h", "d")]; st == nil || st.Success != 0 || st.Failed != 1 {
		t.Fatalf("target stat %+v", st)
	}
}

func TestRolledBackSummaryKeptWhenCompacted(t *testing.T) {
	r := &Result{Compacted: true, RolledBack: true, Summary: "第 3 条 SQL 失败，事务已回滚，此前已执行的 2 条均未生效", FailCount: 1}
	Compact(r)
	if !strings.Contains(r.Summary, "事务已回滚") {
		t.Fatalf("summary overwritten: %q", r.Summary)
	}
}

func TestRunPrecheckFailAppendsRecord(t *testing.T) {
	res := Run(t.Context(), "dml", "update t set a=1", Target{DBType: "postgres", Username: "u", DBName: "d"})
	if res.Success || res.Message != "实例未配置连接主机或端口" {
		t.Fatalf("%+v", res)
	}
	if len(res.Records) != 1 || res.Records[0].Status != "failed" ||
		res.Records[0].SQL != PrecheckSQL || res.Records[0].ErrorMessage != res.Message || res.Records[0].DB != "d" {
		t.Fatalf("records %+v", res.Records)
	}
}

func TestFailBeforeExec(t *testing.T) {
	res := FailBeforeExec("工单实例不存在", Record{InstanceID: "i1", DB: "d"})
	if res.Success || res.FailCount != 1 || len(res.Records) != 1 {
		t.Fatalf("%+v", res)
	}
	rec := res.Records[0]
	if rec.InstanceID != "i1" || rec.SQL != PrecheckSQL || rec.ErrorMessage != "工单实例不存在" || rec.Order != 1 {
		t.Fatalf("%+v", rec)
	}
}
