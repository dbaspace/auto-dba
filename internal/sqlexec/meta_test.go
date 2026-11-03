package sqlexec

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunMetaValidatesInput(t *testing.T) {
	ctx := context.Background()
	_, err := RunMeta(ctx, MetaRequest{})
	if err == nil {
		t.Fatal("empty target should fail")
	}
	_, err = RunMeta(ctx, MetaRequest{Target: Target{Host: "h"}})
	if err == nil {
		t.Fatal("empty databases should fail")
	}
	dbs := make([]MetaDB, metaMaxDatabases+1)
	for i := range dbs {
		dbs[i] = MetaDB{DBName: "d"}
	}
	_, err = RunMeta(ctx, MetaRequest{Target: Target{Host: "h"}, Databases: dbs})
	if err == nil {
		t.Fatal("too many databases should fail")
	}
}

func TestFetchMetaUnsupportedOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sql/meta" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 page not found"))
	}))
	defer srv.Close()

	_, err := FetchMeta(context.Background(), srv.URL, "", "", MetaRequest{
		Target:    Target{Host: "h"},
		Databases: []MetaDB{{DBName: "db"}},
	})
	if !errors.Is(err, ErrMetaUnsupported) {
		t.Fatalf("want ErrMetaUnsupported, got %v", err)
	}
	if errors.Is(err, ErrJobNotFound) {
		t.Fatal("must not be ErrJobNotFound")
	}
	msg := err.Error()
	if !strings.Contains(msg, "重新下发") {
		t.Fatalf("msg should mention 重新下发: %s", msg)
	}
	if strings.Contains(msg, "没有这个任务") {
		t.Fatalf("msg must not say 没有这个任务: %s", msg)
	}
}
