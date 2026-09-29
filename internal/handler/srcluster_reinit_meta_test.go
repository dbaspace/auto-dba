package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSrClusterMetaIncludesReinit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &SrClusterHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/sr-cluster/meta", nil)
	h.Meta(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			ChangeTypes []string `json:"change_types"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, typ := range out.Data.ChangeTypes {
		if typ == "cluster_reinit" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("change_types missing cluster_reinit: %v", out.Data.ChangeTypes)
	}
}
