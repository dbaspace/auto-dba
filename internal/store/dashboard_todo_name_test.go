package store

import (
	"strings"
	"testing"
)

func TestTodoListNameKeepsTicketAndDB(t *testing.T) {
	got := todoListName("T202611170001", "db_ordering_config", "")
	want := "T202611170001 db_ordering_config"
	if got != want {
		t.Fatalf("empty description: got %q want %q", got, want)
	}
}

func TestTodoListNameTruncatesReason(t *testing.T) {
	desc := "TEST 老产品 AI 装修共同临时方案，需求 ai-decoration-workbench-research\n来源 V20261117_001，ALTER TABLE 很长的检查说明"
	got := todoListName("T1", "db_ordering_config", desc)
	excerpt := todoReasonExcerpt(desc)
	if len([]rune(excerpt)) != 40 {
		t.Fatalf("excerpt rune len = %d, want 40: %q", len([]rune(excerpt)), excerpt)
	}
	if got != "T1 db_ordering_config "+excerpt {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "来源") || strings.Contains(got, "ALTER") {
		t.Fatalf("list name still contains the rest of the reason: %q", got)
	}
}

func TestTodoReasonExcerptUsesFirstLine(t *testing.T) {
	got := todoReasonExcerpt("\r\n  只保留这一行  \n后面整段原因不进列表")
	if got != "只保留这一行" {
		t.Fatalf("got %q", got)
	}
}
