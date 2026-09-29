package sqlinsight_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"auto-dba/internal/middleware"
)

// 对临时自测端口上的 SQL 洞察相关路由做鉴权后冒烟（需 AUTO_DBA_SMOKE_BASE）。
func TestSQLInsightAPISmoke(t *testing.T) {
	base := os.Getenv("AUTO_DBA_SMOKE_BASE")
	if base == "" {
		t.Skip("set AUTO_DBA_SMOKE_BASE=http://127.0.0.1:18880/api/v1")
	}
	secret := os.Getenv("AUTO_DBA_SMOKE_JWT_SECRET")
	if secret == "" {
		secret = "change-me-in-production"
	}
	tok, err := middleware.SignToken(secret, "selftest", "admin", "admin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	paths := []string{
		"/pg-activity-snapshots/summary",
		"/pg-space/instances",
		"/pg-space/advice/summary",
		"/awr-reports?current=1&page_size=5",
		"/awr-reports/summary",
		"/sql-insight/analysis/catalog",
		"/sql-insight/analysis/summary",
		"/sql-insight/analysis/runs?current=1&page_size=5",
		"/sql-insight/analysis/metrics?current=1&page_size=5",
		"/sql-insight/analysis/rules?current=1&page_size=5",
	}
	for _, p := range paths {
		req, err := http.NewRequest(http.MethodGet, base+p, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("%s: read body: %v", p, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s => %d %s", p, resp.StatusCode, string(body[:min(len(body), 512)]))
		}
		var wrap struct {
			Data    json.RawMessage `json:"data"`
			Message string          `json:"message"`
		}
		if err := json.Unmarshal(body, &wrap); err != nil {
			t.Fatalf("%s: bad json: %v head=%s", p, err, string(body[:min(len(body), 200)]))
		}
		if len(wrap.Data) == 0 || string(wrap.Data) == "null" {
			t.Fatalf("%s: empty data", p)
		}
		fmt.Printf("OK %s message=%q data_bytes=%d\n", p, wrap.Message, len(wrap.Data))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
