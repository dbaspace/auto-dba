package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/store"
)

func TestFillInstanceAudit(t *testing.T) {
	it := &store.QueryAudit{}
	fillInstanceAudit(it, &store.Instance{ID: "i1", Host: "db.local", DBType: "mysql"})
	if it.InstanceID != "i1" || it.InstanceName != "db.local" || it.DBType != "mysql" {
		t.Fatalf("%+v", it)
	}
	it2 := &store.QueryAudit{}
	fillInstanceAudit(it2, &store.Instance{ID: "i2", Name: "prod", DomainName: "x", Host: "h", DBType: "redis"})
	if it2.InstanceName != "prod" {
		t.Fatalf("prefer name, got %q", it2.InstanceName)
	}
}

func TestRecordQueryAuditNilStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/query", nil)
	recordQueryAudit(nil, c, &store.QueryAudit{Kind: "db", Statement: "select 1"})
}

func TestRequireAdminViewerWithoutStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/query-audits", nil)
	if requireAdminViewer(nil, c, "仅管理员可查看查询审计") {
		t.Fatal("want deny")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestListQueryAuditsRequiresAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/query-audits", nil)
	h := &AuthHandler{}
	h.ListQueryAudits(c)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}
