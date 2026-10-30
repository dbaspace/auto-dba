package dqlexport

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"auto-dba/internal/dbprobe"
)

func TestGzipExtAndFileName(t *testing.T) {
	if got := GzipExt("json"); got != "json.gz" {
		t.Fatalf("GzipExt json: %q", got)
	}
	if got := GzipExt(".csv"); got != "csv.gz" {
		t.Fatalf("GzipExt csv: %q", got)
	}
	if got := GzipFileName("dql_T1_20260101.json"); got != "dql_T1_20260101.json.gz" {
		t.Fatalf("GzipFileName: %q", got)
	}
	if got := GzipFileName("already.json.gz"); got != "already.json.gz" {
		t.Fatalf("idempotent: %q", got)
	}
}

func TestWriteGzipFileRoundTrip(t *testing.T) {
	res := &dbprobe.QueryResult{
		Columns: []string{"id", "name"},
		Rows:    [][]any{{1, "alice"}, {2, "bob"}},
	}
	body, ext, _ := Encode(res, "json", "t", "postgres")
	if ext != "json" || len(body) == 0 {
		t.Fatalf("encode: ext=%q len=%d", ext, len(body))
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "out."+GzipExt(ext))
	n, err := WriteGzipFile(path, body)
	if err != nil {
		t.Fatal(err)
	}
	if n <= 0 || n >= int64(len(body)) {
		// small JSON still usually compresses; if equal, still ok as long as readable
		t.Logf("compressed=%d raw=%d", n, len(body))
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("round-trip mismatch: got %d bytes want %d", len(got), len(body))
	}
}
