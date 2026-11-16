package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPgSpaceCollectArgsJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"instance_id":"inst-1","host":"10.0.0.8","port":5432,"db_scope":"__cluster__"}`)
	req := httptest.NewRequest(http.MethodPost, "/pg-space/collect", body)
	req.Header.Set("Content-Type", "application/json")
	c := ginContext(req)
	id, host, port, scope := pgSpaceCollectArgs(c)
	if id != "inst-1" || host != "10.0.0.8" || port != 5432 || scope != "__cluster__" {
		t.Fatalf("got %s %s %d %s", id, host, port, scope)
	}
}

func TestPgSpaceCollectArgsQueryWins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"instance_id":"from-body","host":"10.0.0.2","port":5433,"db_name":"app"}`)
	req := httptest.NewRequest(http.MethodPost, "/pg-space/collect?instance_id=from-query&host=10.0.0.1&port=5432&db_scope=__all__", body)
	req.Header.Set("Content-Type", "application/json")
	c := ginContext(req)
	id, host, port, scope := pgSpaceCollectArgs(c)
	if id != "from-query" || host != "10.0.0.1" || port != 5432 || scope != "__all__" {
		t.Fatalf("got %s %s %d %s", id, host, port, scope)
	}
}

func TestPgSpaceCollectArgsDBNameFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"instance_id":"inst-1","db_name":"orders"}`)
	req := httptest.NewRequest(http.MethodPost, "/pg-space/collect", body)
	req.Header.Set("Content-Type", "application/json")
	c := ginContext(req)
	_, _, _, scope := pgSpaceCollectArgs(c)
	if scope != "orders" {
		t.Fatalf("scope=%s", scope)
	}
}

func TestPgSpaceJobIDFromBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"job_id":"job-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/pg-space/collect/cancel", body)
	req.Header.Set("Content-Type", "application/json")
	if got := pgSpaceJobID(ginContext(req)); got != "job-1" {
		t.Fatalf("got %s", got)
	}
}

func TestPgSpaceJobIDQueryWins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"job_id":"from-body"}`)
	req := httptest.NewRequest(http.MethodPost, "/pg-space/collect/pause?job_id=from-query", body)
	req.Header.Set("Content-Type", "application/json")
	if got := pgSpaceJobID(ginContext(req)); got != "from-query" {
		t.Fatalf("got %s", got)
	}
}

func ginContext(req *http.Request) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	return c
}
