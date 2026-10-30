package dqlexport

import (
	"strings"
	"testing"

	"auto-dba/internal/dbprobe"
)

func TestEncodeCSV(t *testing.T) {
	res := &dbprobe.QueryResult{
		Columns: []string{"id", "name"},
		Rows:    [][]any{{1, "a"}, {2, "b"}},
	}
	body, ext, ct := Encode(res, "csv", "", "mysql")
	if ext != "csv" || !strings.Contains(ct, "csv") {
		t.Fatalf("ext=%s ct=%s", ext, ct)
	}
	if !strings.Contains(string(body), "id,name") || !strings.Contains(string(body), "1,a") {
		t.Fatalf("body=%s", body)
	}
}

func TestTableNameFromSQL(t *testing.T) {
	if got := TableNameFromSQL("select * from foo.bar where x=1"); got != "foo.bar" {
		t.Fatalf("got %q", got)
	}
}
