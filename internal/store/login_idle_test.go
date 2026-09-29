package store

import (
	"os"
	"testing"
)

func TestLoginIdleTimeoutMin(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	_ = d.LoginIdleTimeoutMin(30)
}
