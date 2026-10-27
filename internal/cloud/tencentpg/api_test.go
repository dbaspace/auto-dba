package tencentpg

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/cloud"
)

func TestTC3Headers(t *testing.T) {
	ts := time.Date(2026, 10, 26, 1, 2, 3, 0, time.UTC)
	h := TC3Headers("sid", "skey", "postgres", "postgres.tencentcloudapi.com", "DescribeDBInstances", "2017-03-12", "ap-guangzhou", []byte(`{}`), ts)
	if !strings.HasPrefix(h["Authorization"], "TC3-HMAC-SHA256 Credential=sid/") {
		t.Fatal(h["Authorization"])
	}
	if h["X-TC-Action"] != "DescribeDBInstances" || h["X-TC-Version"] != "2017-03-12" {
		t.Fatalf("%v", h)
	}
}

func TestRateLimitMapped(t *testing.T) {
	c := Reader{&Client{Host: "example.test", Do: func(r *http.Request) (*http.Response, error) {
		body := `{"Response":{"Error":{"Code":"RequestLimitExceeded","Message":"slow"}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}}}
	_, err := c.ListInstances(context.Background(), cloud.Cred{AccountID: "a", Region: "ap-guangzhou", SecretID: "i", SecretKey: "k"})
	if !cloud.IsRateLimit(err) {
		t.Fatal(err)
	}
}

func TestReaderHasNoMutations(t *testing.T) {
	var r cloud.Reader = Reader{&Client{}}
	if r.Caps().ScaleDiskDown {
		t.Fatal("tencent disk cannot shrink")
	}
	if !r.Caps().Switchover {
		t.Fatal("SwitchDBInstancePrimary is available")
	}
}

func TestSlowLogParse(t *testing.T) {
	c := Reader{&Client{Host: "example.test", Do: func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-TC-Action") != "DescribeSlowQueryList" {
			t.Fatalf("action %s", r.Header.Get("X-TC-Action"))
		}
		body := `{"Response":{"TotalCount":1,"SlowQueryList":[{"DatabaseName":"app","UserName":"u","ClientAddr":"10.0.0.1","Duration":1.5,"NormalQuery":"select 1","LastTime":"2026-10-26 12:00:00"}]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}}}
	rows, total, err := c.ListSlowLogs(context.Background(), cloud.Cred{SecretID: "i", SecretKey: "k", Region: "ap-guangzhou"}, "postgres-1", time.Now().Add(-time.Minute), time.Now(), 0, 10)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].DBName != "app" || rows[0].DurationMS != 1500 {
		t.Fatalf("rows=%+v total=%d err=%v", rows, total, err)
	}
}
