package pgspace

import (
	"strings"
	"testing"
)

func TestTableSQLReadsHeapBlocksFromStatio(t *testing.T) {
	for _, q := range []string{pgSpaceTablesSQL, pgSpaceHotTablesSQL} {
		if strings.Contains(q, "st.heap_blks_read") || strings.Contains(q, "st.heap_blks_hit") {
			t.Fatalf("heap blocks must come from pg_statio_user_tables, sql:\n%s", q)
		}
		if !strings.Contains(q, "LEFT JOIN pg_statio_user_tables io ON io.relid = c.oid") {
			t.Fatalf("missing statio join:\n%s", q)
		}
		if !strings.Contains(q, "io.heap_blks_read") || !strings.Contains(q, "io.heap_blks_hit") {
			t.Fatalf("missing io heap columns:\n%s", q)
		}
	}
}
