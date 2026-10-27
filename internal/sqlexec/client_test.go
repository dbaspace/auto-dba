package sqlexec

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
