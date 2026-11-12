package sqlexec

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeRejectsMissingOrOldProto(t *testing.T) {
	cases := []string{`{"ok":true}`, `{"ok":true,"proto":0}`, `not-json`}
	for _, body := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		err := Probe(context.Background(), srv.URL, "", "")
		srv.Close()
		if !errors.Is(err, ErrProtoTooOld) {
			t.Fatalf("body %s err=%v", body, err)
		}
	}
}

func TestProbeAcceptsCurrentProto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"proto":%d}`, Proto)
	}))
	defer srv.Close()
	if err := Probe(context.Background(), srv.URL, "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestGetJobNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"job_not_found","id":"gone"}`))
	}))
	defer srv.Close()

	_, err := Get(context.Background(), srv.URL, "", "", "gone", 0)
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("err=%v", err)
	}
}
