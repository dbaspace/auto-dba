package handler

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

func openWorkbenchTestStore(t *testing.T) *store.DB {
	t.Helper()
	dsn := strings.TrimSpace(testingDSN(t))
	if dsn == "" {
		t.Skip("no test dsn")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		if strings.Contains(err.Error(), "must be owner") {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testingDSN(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(getenv("AUTO_DBA_TEST_DSN")); v != "" {
		return v
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	cfgPath := filepath.Join(filepath.Dir(file), "..", "..", "config", "config.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.DB.DSN)
}

func getenv(k string) string {
	return strings.TrimSpace(os.Getenv(k))
}
