package handler

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestApplyIssueListPrefix(t *testing.T) {
	it := &store.Issue{}
	applyIssueListPrefix(it, `{"env":"beijing","db_type":"starrocks","sql":"`+strings.Repeat("x", 5000))
	if it.Env != "" || it.DBType != "" {
		t.Fatalf("截断的 JSON 不应写入展示字段: %+v", it)
	}
	applyIssueListPrefix(it, `{"idc_scope":"singapore","engine":"postgres","sql":"select 1"}`)
	if it.Env != "singapore" || it.DBType != "postgres" {
		t.Fatalf("前缀应补环境和库类型: %+v", it)
	}
	applyIssueListPrefix(it, `{"env":"beijing","db_type":"mysql"}`)
	if it.Env != "singapore" || it.DBType != "postgres" {
		t.Fatalf("已有值不应被前缀覆盖: %+v", it)
	}
	it.SQLContent = ""
	if it.SQLContent != "" {
		t.Fatal("前缀不能写回正文")
	}
}
