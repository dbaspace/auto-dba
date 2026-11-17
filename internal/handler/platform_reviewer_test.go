package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func TestPlatformChangeReviewerNilStore(t *testing.T) {
	if platformChangeReviewer(nil, "admin", "1") {
		t.Fatal("nil store")
	}
	names, err := listPlatformChangeReviewers(nil)
	if err != nil || names != nil {
		t.Fatalf("nil list: %v %v", names, err)
	}
}

func TestRedisAssigneeCanApproveWithoutPool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUsername, "wangxiaotai")
	c.Set(middleware.CtxRole, "user")
	h := &TicketHandler{Store: &store.DB{}}
	it := &store.Issue{IssueType: store.IssueTypeRedis, AssignedTo: "wangxiaotai"}
	if !h.actorCanApproveIssue(c, it) {
		t.Fatal("实例登记的审核人应能审批 Redis 工单")
	}
}

func TestRedisApproveUsesPlatformPool(t *testing.T) {
	dsn := testingDSN(t)
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN or config dsn")
	}
	db, err := store.OpenWithoutMigrate("postgres", dsn)
	if err != nil {
		t.Skip(err.Error())
	}
	t.Cleanup(func() { db.Close() })

	names, err := listPlatformChangeReviewers(db)
	if err != nil {
		t.Fatal(err)
	}
	if !usernameInList("admin", names) {
		t.Fatalf("platform pool missing admin: %v", names)
	}

	gin.SetMode(gin.TestMode)
	h := &TicketHandler{Store: db}
	it := &store.Issue{IssueType: store.IssueTypeRedis, AssignedTo: "not-the-actor"}

	deny := httptest.NewRecorder()
	denyCtx, _ := gin.CreateTestContext(deny)
	denyCtx.Set(middleware.CtxUsername, "no-such-redis-reviewer")
	denyCtx.Set(middleware.CtxRole, "user")
	if h.actorCanApproveIssue(denyCtx, it) {
		t.Fatal("名单外用户不应审批 Redis 工单")
	}

	allowName := ""
	for _, name := range names {
		if strings.EqualFold(name, "admin") {
			continue
		}
		if db.IsGlobalApprover(name) {
			continue
		}
		allowName = name
		break
	}
	if allowName == "" {
		allowName = "admin"
	}
	allow := httptest.NewRecorder()
	allowCtx, _ := gin.CreateTestContext(allow)
	allowCtx.Set(middleware.CtxUsername, allowName)
	allowCtx.Set(middleware.CtxRole, "user")
	if !h.actorCanApproveIssue(allowCtx, it) {
		t.Fatalf("%s 在平台审核池内，应能审批 Redis 工单", allowName)
	}
}

func TestResolveRedisAssigneeWhitelistSkipsTenant(t *testing.T) {
	dsn := testingDSN(t)
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN or config dsn")
	}
	db, err := store.OpenWithoutMigrate("postgres", dsn)
	if err != nil {
		t.Skip(err.Error())
	}
	t.Cleanup(func() { db.Close() })

	const outsider = "redis-wl-outsider"
	prev, err := db.GetGlobalArg(store.ArgIssueGlobalApproverWhitelist)
	if err != nil {
		t.Fatal(err)
	}
	if !usernameInList(outsider, store.ParseApproverWhitelist(prev)) {
		next := outsider
		if strings.TrimSpace(prev) != "" {
			next = prev + "," + outsider
		}
		if err := db.SetGlobalArg(store.ArgIssueGlobalApproverWhitelist, next, "test"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = db.SetGlobalArg(store.ArgIssueGlobalApproverWhitelist, prev, "test")
		})
	}

	got, err := resolveRedisAssignee(db, "tenant-not-a-member", outsider)
	if err != nil {
		t.Fatal(err)
	}
	if got != outsider {
		t.Fatalf("whitelist assignee: got %q", got)
	}

	_, err = resolveRedisAssignee(db, "tenant-not-a-member", "no-such-redis-reviewer")
	if err == nil || !strings.Contains(err.Error(), "不是该租户的活跃成员") {
		t.Fatalf("outsider should be rejected, got %v", err)
	}
}
