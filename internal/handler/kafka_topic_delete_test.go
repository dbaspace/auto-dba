package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDeleteKafkaTopicRequiresTicket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &InstanceHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/instances/i1/kafka/topics/orders", nil)
	c.Params = gin.Params{{Key: "id", Value: "i1"}, {Key: "topic", Value: "orders"}}
	h.DeleteKafkaTopic(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "工单") {
		t.Fatalf("body %s", w.Body.String())
	}
}
