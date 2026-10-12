package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestListKnowledgePostgres(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &AlertHandler{}
	r := gin.New()
	r.GET("/aiops/knowledge", h.ListKnowledge)
	req := httptest.NewRequest(http.MethodGet, "/aiops/knowledge?engine=postgres", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Total int `json:"total"`
			Items []struct {
				Fingerprint string `json:"fingerprint"`
				Engine      string `json:"engine"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Total < 10 {
		t.Fatalf("total=%d", resp.Data.Total)
	}
	for _, it := range resp.Data.Items {
		if it.Engine != "postgres" {
			t.Fatalf("engine=%s", it.Engine)
		}
	}
}

func TestMatchKnowledgePreviewPGConnHigh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &AlertHandler{}
	r := gin.New()
	r.GET("/aiops/knowledge/match", h.MatchKnowledgePreview)
	req := httptest.NewRequest(http.MethodGet, "/aiops/knowledge/match?alertname=PGConnHigh", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Total int `json:"total"`
			Cases []struct {
				MetricKey string `json:"metric_key"`
				Title     string `json:"title"`
			} `json:"cases"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Total == 0 {
		t.Fatal("expected matches")
	}
	found := false
	for _, c := range resp.Data.Cases {
		if c.MetricKey == "conn_usage_ratio" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("cases=%+v", resp.Data.Cases)
	}
}
