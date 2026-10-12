package ticketexec

import (
	"strings"
	"testing"
)

func TestFilterDMLTxnControlStatements(t *testing.T) {
	in := []string{
		"BEGIN;",
		"-- comment\nBEGIN TRANSACTION",
		"UPDATE tbl_menu_item SET a=1 WHERE id=1;",
		"COMMIT;",
		"START TRANSACTION",
		"UPDATE t SET b=2;",
		"ROLLBACK;",
		"ABORT",
	}
	got := filterDMLTxnControlStatements(in)
	if len(got) != 2 {
		t.Fatalf("len=%d got=%v", len(got), got)
	}
	if !strings.Contains(got[0], "tbl_menu_item") || !strings.Contains(got[1], "UPDATE t") {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestFilterDMLTxnControlOnlyMarkers(t *testing.T) {
	got := filterDMLTxnControlStatements([]string{"BEGIN;", "COMMIT;"})
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestFilterDMLTxnControlUnchanged(t *testing.T) {
	in := []string{
		"UPDATE t SET a=1 WHERE id=1;",
		"DELETE FROM t WHERE id=2;",
	}
	got := filterDMLTxnControlStatements(in)
	if len(got) != 2 || got[0] != in[0] || got[1] != in[1] {
		t.Fatalf("got %v", got)
	}
}

func TestIsDMLTxnControlStatement(t *testing.T) {
	yes := []string{
		"BEGIN", "begin;", "BEGIN TRANSACTION;", "START TRANSACTION",
		"COMMIT", "commit transaction;", "ROLLBACK", "ROLLBACK TRANSACTION", "ABORT;",
		"-- leading\nBEGIN;",
	}
	for _, s := range yes {
		if !isDMLTxnControlStatement(s) {
			t.Fatalf("want txn control: %q", s)
		}
	}
	no := []string{
		"UPDATE t SET a=1;",
		"SELECT 1",
		"BEGINNING_COL = 1", // not a txn marker
		"",
	}
	for _, s := range no {
		if isDMLTxnControlStatement(s) {
			t.Fatalf("want not txn control: %q", s)
		}
	}
}
