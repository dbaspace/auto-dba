package dbmig

import (
	"strings"
	"testing"
)

func TestSchemaCompareExportCSV(t *testing.T) {
	items := []TableSchemaDiff{
		{Table: "t1", Status: "match", Message: "列一致"},
		{
			Table:   "t2",
			Status:  "differ",
			Message: "1 处差异",
			Diffs: []SchemaDiffItem{
				{Kind: "column", Name: "c1", Left: "int", Right: "bigint", Message: "列类型不一致"},
			},
		},
	}
	raw := string(SchemaCompareExportCSV(items))
	if !strings.HasPrefix(raw, "\uFEFF") {
		t.Fatal("expected UTF-8 BOM")
	}
	if !strings.Contains(raw, "table,status,message,diff_kind") {
		t.Fatal("missing header")
	}
	if !strings.Contains(raw, "t1,match,") {
		t.Fatalf("missing match row: %s", raw)
	}
	if !strings.Contains(raw, "t2,differ,") || !strings.Contains(raw, "column,c1,int,bigint") {
		t.Fatalf("missing differ row: %s", raw)
	}
}
