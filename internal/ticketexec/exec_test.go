package ticketexec

import (
	"context"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestShouldAutoRun(t *testing.T) {
	if !ShouldAutoRun(store.IssueTypeDDL) || !ShouldAutoRun("dml") || !ShouldAutoRun(store.IssueTypeDCL) || !ShouldAutoRun(store.IssueTypeCreateDB) || !ShouldAutoRun(store.IssueTypeHA) ||
		!ShouldAutoRun(store.IssueTypeSrCluster) || !ShouldAutoRun(store.IssueTypeDbDeploy) || !ShouldAutoRun(store.IssueTypeResourceApply) ||
		!ShouldAutoRun(store.IssueTypeRedis) || !ShouldAutoRun(store.IssueTypeBackupRestore) || !ShouldAutoRun(store.IssueTypeCloudPg) {
		t.Fatal("ddl/dml/dcl/createdb/ha/sr_cluster/db_deploy/resource_apply/redis/backup_restore should auto run")
	}
	for _, typ := range []string{store.IssueTypeDQL, store.IssueTypeKafka, store.IssueTypeArchive,
		store.IssueTypeFlashback, store.IssueTypeCDC, store.IssueTypeMigration} {
		if ShouldAutoRun(typ) {
			t.Fatalf("%s should not auto run", typ)
		}
	}
}

func TestAdminConnectDB(t *testing.T) {
	if AdminConnectDB("postgres") != "postgres" || AdminConnectDB("pg") != "postgres" {
		t.Fatal("pg should connect postgres")
	}
	if AdminConnectDB("mysql") != "" || AdminConnectDB("starrocks") != "" {
		t.Fatal("mysql/sr admin db should be empty")
	}
}

func TestRunCreateDBAllowsEmptyLogicalDB(t *testing.T) {
	res := Run(context.Background(), store.IssueTypeCreateDB, "CREATE DATABASE db_app;", Target{
		Host: "127.0.0.1", Port: 1, Username: "u", DBType: "postgres",
	})
	if res == nil || res.Success {
		t.Fatalf("unreachable host should fail: %+v", res)
	}
	if res.Message == "工单未指定数据库" {
		t.Fatal("createdb should connect admin db, not require ticket DBName")
	}
}

func TestExecutable(t *testing.T) {
	if !Executable("postgres") || !Executable("starrocks") || !Executable("pg") {
		t.Fatal("pg/sr should be executable")
	}
	if !Executable("mysql") || !Executable("tidb") || !Executable("oceanbase") {
		t.Fatal("mysql/tidb/oceanbase should auto-exec SQL")
	}
	if Executable("oracle") || Executable("mongodb") {
		t.Fatal("unsupported engines should not auto-exec")
	}
}

func TestRunMySQLAttemptsConnect(t *testing.T) {
	res := Run(context.Background(), "ddl", "ALTER TABLE t ADD COLUMN c int", Target{
		Host: "127.0.0.1", Port: 1, Username: "u", DBName: "db", DBType: "mysql",
	})
	if res == nil || res.Skipped || res.Success {
		t.Fatalf("mysql ddl should attempt connect and fail: %+v", res)
	}
}

func TestRunEmptySQL(t *testing.T) {
	res := Run(context.Background(), "ddl", "   ", Target{
		Host: "127.0.0.1", Port: 5432, Username: "u", DBName: "db", DBType: "postgres",
	})
	if res == nil || res.Success {
		t.Fatalf("empty sql should fail: %+v", res)
	}
}

func TestCanRetry(t *testing.T) {
	if !CanRetry(store.StatusApproved) || !CanRetry(store.StatusFailed) || !CanRetry(store.StatusStopped) {
		t.Fatal("approved/failed should retry")
	}
	if CanRetry(store.StatusExecuted) || CanRetry(store.StatusPending) {
		t.Fatal("executed/pending should not retry")
	}
	if CanRetry(store.StatusRunning) || IsExecuting(store.StatusApproved) {
		t.Fatal("running should not retry; approved is not executing")
	}
	if !IsExecuting(store.StatusRunning) {
		t.Fatal("running should be executing")
	}
}

func TestAsyncAutoExecMaxWait(t *testing.T) {
	if AsyncAutoExecMaxWait < time.Hour {
		t.Fatalf("async wall clock too short: %v", AsyncAutoExecMaxWait)
	}
	if ddlTimeout < time.Hour {
		t.Fatalf("per-target ddl timeout too short for large SQL: %v", ddlTimeout)
	}
}

func TestDisplaySQLTruncate(t *testing.T) {
	short := "SELECT 1"
	if displaySQL(short) != short {
		t.Fatal("short sql should keep intact")
	}
	long := strings.Repeat("a", maxSQLLen+50)
	got := displaySQL(long)
	if len(got) <= maxSQLLen || !strings.HasSuffix(got, "...") {
		t.Fatalf("want truncated with ..., got len=%d", len(got))
	}
	if got[:maxSQLLen] != long[:maxSQLLen] {
		t.Fatal("prefix mismatch")
	}
}
