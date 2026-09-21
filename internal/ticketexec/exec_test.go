package ticketexec

import (
	"context"
	"testing"

	"auto-dba/internal/store"
)

func TestShouldAutoRun(t *testing.T) {
	if !ShouldAutoRun(store.IssueTypeDDL) || !ShouldAutoRun("dml") || !ShouldAutoRun(store.IssueTypeCreateDB) {
		t.Fatal("ddl/dml/createdb should auto run")
	}
	if ShouldAutoRun(store.IssueTypeDQL) {
		t.Fatal("dql should not auto run")
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
	if Executable("mysql") || Executable("tidb") {
		t.Fatal("mysql/tidb should not auto-exec SQL")
	}
}

func TestRunSkipMySQL(t *testing.T) {
	res := Run(context.Background(), "ddl", "ALTER TABLE t ADD COLUMN c int", Target{
		Host: "127.0.0.1", Port: 3306, Username: "u", DBName: "db", DBType: "mysql",
	})
	if res == nil || !res.Success || !res.Skipped {
		t.Fatalf("mysql ddl should skip: %+v", res)
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
	if !CanRetry(store.StatusApproved) || !CanRetry(store.StatusFailed) {
		t.Fatal("approved/failed should retry")
	}
	if CanRetry(store.StatusExecuted) || CanRetry(store.StatusPending) {
		t.Fatal("executed/pending should not retry")
	}
}
