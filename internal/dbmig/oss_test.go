package dbmig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestOssRegion(t *testing.T) {
	cases := map[string]string{
		"oss-cn-hangzhou.aliyuncs.com": "cn-hangzhou",
		"cos.ap-beijing.myqcloud.com":  "ap-beijing",
		"s3.us-west-2.amazonaws.com":   "us-west-2",
		"example.com":                  "us-east-1",
	}
	for host, want := range cases {
		if got := ossRegion(host); got != want {
			t.Fatalf("ossRegion(%s)=%s want %s", host, got, want)
		}
	}
}

func TestTestOSSValidation(t *testing.T) {
	ok, msg := TestOSS(context.Background(), store.MigrationOSSSettings{})
	if ok || !strings.Contains(msg, "访问地址") {
		t.Fatalf("empty cfg: ok=%v msg=%s", ok, msg)
	}
}

func TestTestOSSHeadOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("method %s", r.Method)
		}
		if r.Header.Get("Authorization") == "" {
			t.Fatal("missing Authorization")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ok, msg := TestOSS(context.Background(), store.MigrationOSSSettings{
		Endpoint:  srv.URL,
		Bucket:    "demo",
		AccessKey: "ak",
		SecretKey: "sk",
	})
	if !ok {
		t.Fatalf("want ok, got %s", msg)
	}
}
