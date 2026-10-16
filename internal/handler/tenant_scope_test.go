package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestListScopeTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		query string
		want  bool
	}{
		{"", false},
		{"scope=all", false},
		{"scope=tenant", true},
		{"scope=Tenant", true},
		{"scope=tenant&q=x", true},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/databases?"+tc.query, nil)
		if got := listScopeTenant(c); got != tc.want {
			t.Fatalf("query=%q got %v want %v", tc.query, got, tc.want)
		}
	}
	if listScopeTenant(nil) {
		t.Fatal("nil context should be false")
	}
}
