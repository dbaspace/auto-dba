package handler

import (
	"strings"
	"testing"

	"auto-dba/internal/dbprobe"
)

func TestEncodeExportFormats(t *testing.T) {
	res := &dbprobe.QueryResult{
		Columns: []string{"id", "name"},
		Rows:    [][]any{{1, "a,b"}, {nil, "x"}},
	}
	csvBody, ext, _ := encodeExport(res, "csv", "t", "postgres")
	if ext != "csv" || !strings.Contains(string(csvBody), "id,name") || !strings.Contains(string(csvBody), `"a,b"`) {
		t.Fatalf("csv unexpected: %s", csvBody)
	}
	js, ext, _ := encodeExport(res, "json", "t", "postgres")
	if ext != "json" || !strings.Contains(string(js), `"name": "a,b"`) {
		t.Fatalf("json unexpected: %s", js)
	}
	sqlBody, ext, _ := encodeExport(res, "sql", "public.t", "postgres")
	if ext != "sql" || !strings.Contains(string(sqlBody), `INSERT INTO "public"."t"`) || !strings.Contains(string(sqlBody), "NULL") {
		t.Fatalf("sql unexpected: %s", sqlBody)
	}
}

func TestExportTableName(t *testing.T) {
	if got := exportTableName("SELECT * FROM account LIMIT 5"); got != "account" {
		t.Fatalf("got %s", got)
	}
	if got := exportTableName(`SELECT * FROM "public"."account"`); got != "public.account" {
		t.Fatalf("got %s", got)
	}
}
