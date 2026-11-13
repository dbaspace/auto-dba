package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func TestRegisterJoinsDefaultTenantWhenEnabled(t *testing.T) {
	db := systemNoticeTestStore(t)
	h := &AuthHandler{Store: db}
	gin.SetMode(gin.TestMode)

	_, _ = db.Exec(`DELETE FROM tbl_system_notices`)
	it := &store.SystemNotice{
		Title: "register-off", Body: "x", Enabled: false,
		ShowMode: store.SystemNoticeModeOnVersionChange,
	}
	if err := db.UpsertSystemNotice(it, false, "admin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_system_notices WHERE id=?`, it.ID)
	})

	username := "reg_" + uuid.NewString()[:8]
	call := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{
			"username": username, "password": "secret1", "password_confirm": "secret1",
			"display_name": "自助",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.Register(c)
		return w
	}

	off := call()
	if off.Code != http.StatusForbidden {
		t.Fatalf("closed code=%d body=%s", off.Code, off.Body.String())
	}

	it.UserRegisterEnabled = true
	if err := db.UpsertSystemNotice(it, false, "admin"); err != nil {
		t.Fatal(err)
	}
	on := call()
	if on.Code != http.StatusOK {
		t.Fatalf("open code=%d body=%s", on.Code, on.Body.String())
	}
	if u, err := db.GetUserByUsername(username); err != nil || u != nil {
		t.Fatalf("user created before approval: %+v err=%v", u, err)
	}
	pending, err := db.GetPendingUserRegistrationByUsername(username)
	if err != nil || pending == nil {
		t.Fatalf("pending missing: %v", err)
	}
	t.Cleanup(func() { _ = db.DeleteUserRegistration(pending.ID) })

	again := call()
	if again.Code != http.StatusBadRequest {
		t.Fatalf("duplicate pending code=%d body=%s", again.Code, again.Body.String())
	}

	viewerRole, err := db.GetRoleByCode("viewer")
	if err != nil || viewerRole == nil {
		t.Fatalf("viewer role: %v", err)
	}
	viewer := &store.User{
		Username: "regv_" + uuid.NewString()[:8], DisplayName: "只读",
		Role: viewerRole.Code, RoleID: viewerRole.ID, RoleIDs: []string{viewerRole.ID},
		Source: store.UserSourcePlatform,
	}
	if err := db.CreateUser(viewer, "hash"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DeleteUser(viewer.ID) })
	denied := review(t, h, "approve", pending.ID, viewer.Username)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-admin approve code=%d body=%s", denied.Code, denied.Body.String())
	}
	if u, _ := db.GetUserByUsername(username); u != nil {
		t.Fatal("non-admin approve created user")
	}

	admin, err := db.GetUserByUsername("admin")
	if err != nil || admin == nil {
		t.Fatal("need admin user")
	}
	approved := review(t, h, "approve", pending.ID, admin.Username)
	if approved.Code != http.StatusOK {
		t.Fatalf("approve code=%d body=%s", approved.Code, approved.Body.String())
	}
	u, err := db.GetUserByUsername(username)
	if err != nil || u == nil {
		t.Fatalf("user missing after approve: %v", err)
	}
	t.Cleanup(func() { _ = db.DeleteUser(u.ID) })
	if u.Role != "developer" || u.Source != store.UserSourcePlatform {
		t.Fatalf("role=%s source=%s", u.Role, u.Source)
	}
	member, err := db.IsActiveTenantMember(store.DefaultTenantID, u.ID)
	if err != nil || !member {
		t.Fatalf("default membership ok=%v err=%v", member, err)
	}
	if left, _ := db.GetUserRegistration(pending.ID); left != nil {
		t.Fatal("registration kept after approve")
	}

	rejectName := "rejr_" + uuid.NewString()[:8]
	body, _ := json.Marshal(map[string]string{
		"username": rejectName, "password": "secret1", "password_confirm": "secret1",
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.Register(c)
	if w.Code != http.StatusOK {
		t.Fatalf("reject-submit code=%d body=%s", w.Code, w.Body.String())
	}
	rej, err := db.GetPendingUserRegistrationByUsername(rejectName)
	if err != nil || rej == nil {
		t.Fatalf("reject pending: %v", err)
	}
	t.Cleanup(func() { _ = db.DeleteUserRegistration(rej.ID) })
	rejected := review(t, h, "reject", rej.ID, admin.Username)
	if rejected.Code != http.StatusOK {
		t.Fatalf("reject code=%d body=%s", rejected.Code, rejected.Body.String())
	}
	if u, _ := db.GetUserByUsername(rejectName); u != nil {
		t.Fatal("reject created user")
	}
	if left, _ := db.GetUserRegistration(rej.ID); left != nil {
		t.Fatal("registration kept after reject")
	}
}

func review(t *testing.T, h *AuthHandler, action, id, username string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/user-registrations/"+id+"/"+action, nil)
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Set(middleware.CtxUsername, username)
	if action == "reject" {
		h.RejectUserRegistration(c)
	} else {
		h.ApproveUserRegistration(c)
	}
	return w
}
