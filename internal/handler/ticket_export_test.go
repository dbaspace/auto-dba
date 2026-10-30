package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbprobe"
	"auto-dba/internal/sqlexec"
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

func TestMapExportAgentPhase(t *testing.T) {
	cases := []struct {
		phase, status, want string
	}{
		{sqlexec.ExportQuerying, "running", dqlExportPhaseQuerying},
		{sqlexec.ExportEncoding, "running", dqlExportPhaseQuerying},
		{sqlexec.ExportQueued, "running", dqlExportPhaseQuerying},
		{"", sqlexec.ExportFailed, dqlExportPhaseFailed},
		{"", sqlexec.ExportDone, dqlExportPhaseQuerying},
	}
	for _, tc := range cases {
		if got := mapExportAgentPhase(tc.phase, tc.status); got != tc.want {
			t.Fatalf("phase=%q status=%q got %s want %s", tc.phase, tc.status, got, tc.want)
		}
	}
}

func TestDQLApprovalExtraExportRoundTrip(t *testing.T) {
	ex := &dqlApprovalExtra{
		NeedSensitive: true,
		ExportFormat:  "csv",
		ExportFile:    "dql_t1.csv",
		ExportBytes:   12,
		ExportStage:   dqlExportPhaseReady,
		ExportVia:     "local",
	}
	raw, err := ex.encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseDQLApprovalExtra(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExportFile != "dql_t1.csv" || got.ExportStage != dqlExportPhaseReady || got.ExportVia != "local" {
		t.Fatalf("roundtrip %+v", got)
	}
	// 审批字段不得被导出字段冲掉
	if !got.NeedSensitive {
		t.Fatal("need_sensitive lost")
	}
}

func TestExport_BadFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &TicketHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/tickets/x/export?file_format=xlsx", nil)
	c.Params = gin.Params{{Key: "id", Value: "x"}}
	withUser(c, "alice", "developer")
	h.Export(c)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "csv") {
		t.Fatalf("want 400 format, got %d %s", w.Code, w.Body.String())
	}
}

func TestReapDQLExportFiles(t *testing.T) {
	root := filepath.Join("data", dqlExportDirName)
	oldDir := filepath.Join(root, "old-issue")
	newDir := filepath.Join(root, "new-issue")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(oldDir)
		_ = os.RemoveAll(newDir)
	})
	oldTime := time.Now().Add(-dqlExportRetain - time.Hour)
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	h := &TicketHandler{}
	h.ReapDQLExportFiles()
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old export dir should be removed, err=%v", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("new export dir should remain: %v", err)
	}
}
