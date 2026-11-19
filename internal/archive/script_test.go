package archive

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestPrepareBuiltinScripts(t *testing.T) {
	py := "def archive_delete():\n    return 0\n"
	if err := PrepareScript("python", py); err != nil {
		t.Fatal(err)
	}
	if err := PrepareScript("python", "print(1)\n"); err == nil {
		t.Fatal("python without archive_delete")
	}
	goSrc := "package main\n\nfunc archiveDelete() (int64, error) {\n\treturn 0, nil\n}\n"
	if err := PrepareScript("go", goSrc); err != nil {
		t.Fatal(err)
	}
	if err := PrepareScript("shell", "echo hi"); err == nil {
		t.Fatal("shell should be rejected")
	}
	if err := PrepareScript("sql", "DELETE FROM t WHERE id>0"); err != nil {
		t.Fatal(err)
	}
}

func TestRenderGoScript(t *testing.T) {
	src, err := RenderGoScript(ScriptParams{
		Host: "pg.example", Port: 5432, User: "u", Password: "p",
		DBName: "db_payment", Tables: []string{"tbl_pay"}, RetainDays: 92, BatchLimit: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "func archiveDelete()") || !strings.Contains(src, "pg.example") || !strings.Contains(src, "tbl_pay") {
		t.Fatalf("rendered script missing fields:\n%s", src)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "main.go", src, 0); err != nil {
		t.Fatalf("generated go does not parse: %v\n%s", err, src)
	}
	if _, err := RenderGoScript(ScriptParams{Host: "h"}); err == nil {
		t.Fatal("incomplete params")
	}
}

func TestEvalFailCount(t *testing.T) {
	hits := evalAlertRule(
		store.ArchiveAlertRule{RuleType: alertFailCount, Threshold: 1, Enabled: true},
		nil,
		nil,
		2,
		time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		StorageSettings{},
	)
	if len(hits) != 1 || !strings.Contains(hits[0].Message, "失败次数") {
		t.Fatalf("%+v", hits)
	}
	quiet := evalAlertRule(
		store.ArchiveAlertRule{RuleType: alertDBDisconnect, Threshold: 1, Enabled: true},
		nil,
		[]store.ArchiveRun{{ID: "r1", Message: "syntax error"}},
		1,
		time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		StorageSettings{},
	)
	if len(quiet) != 0 {
		t.Fatalf("disconnect false positive: %+v", quiet)
	}
}
