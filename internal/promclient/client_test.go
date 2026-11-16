package promclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGatewayTimeoutDropsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte(`<html><head><title>504 Gateway Time-out</title></head><body><center>openresty</center></body></html>`))
	}))
	defer srv.Close()
	cli := New(Config{BaseURL: srv.URL, Timeout: time.Second})
	_, err := cli.Query(context.Background(), "up", time.Now())
	if err == nil || err.Error() != "Prometheus 网关超时（504）" {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), "openresty") || strings.Contains(err.Error(), "<html") {
		t.Fatalf("html leaked: %v", err)
	}
}

func TestHTTPErrorKeepsShortText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "parse error", http.StatusBadRequest)
	}))
	defer srv.Close()
	cli := New(Config{BaseURL: srv.URL, Timeout: time.Second})
	_, err := cli.Query(context.Background(), "up", time.Now())
	if err == nil || !strings.Contains(err.Error(), "prometheus http 400") || !strings.Contains(err.Error(), "parse error") {
		t.Fatalf("err %v", err)
	}
}
