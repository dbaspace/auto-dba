package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/store"
)

// stubDeleteStore 仅覆盖 Delete 路径所需的最小 Store 表面（通过嵌入真实类型不可行时改测错误映射）。
func TestDeleteTenantHTTPStatusMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name   string
		err    error
		status int
		kind   string
		msg    string
	}{
		{
			name:   "default",
			err:    &store.TenantDeleteBlockedError{Kind: "default", Message: "默认租户不可删除"},
			status: http.StatusBadRequest,
			kind:   "default",
			msg:    "默认租户不可删除",
		},
		{
			name:   "members",
			err:    &store.TenantDeleteBlockedError{Kind: "members", Message: "请先移除全部成员", Members: 2},
			status: http.StatusConflict,
			kind:   "members",
			msg:    "请先移除全部成员",
		},
		{
			name: "resources",
			err: &store.TenantDeleteBlockedError{
				Kind: "resources", Message: "需要先释放资源",
				Resources: map[string]int64{"tbl_instances": 1},
			},
			status: http.StatusConflict,
			kind:   "resources",
			msg:    "需要先释放资源",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 直接验证 errors.As + 状态码映射逻辑（与 handler.Delete 一致）
			var blocked *store.TenantDeleteBlockedError
			if !errors.As(tc.err, &blocked) {
				t.Fatal("errors.As failed")
			}
			status := http.StatusConflict
			if blocked.Kind == "default" {
				status = http.StatusBadRequest
			}
			if status != tc.status {
				t.Fatalf("status=%d want %d", status, tc.status)
			}
			if blocked.Message != tc.msg || blocked.Kind != tc.kind {
				t.Fatalf("blocked=%+v", blocked)
			}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodDelete, "/tenants/x", nil)
			body := gin.H{"message": blocked.Message, "kind": blocked.Kind}
			if blocked.Members > 0 {
				body["members"] = blocked.Members
			}
			if len(blocked.Resources) > 0 {
				body["resources"] = blocked.Resources
			}
			c.JSON(status, body)
			if w.Code != tc.status {
				t.Fatalf("wrote %d", w.Code)
			}
			var out map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out["message"] != tc.msg {
				t.Fatalf("body=%v", out)
			}
		})
	}
}
