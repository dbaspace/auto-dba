package handler

import (
	"os"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestClassifyResourceAction(t *testing.T) {
	applicant := "tenant-biz"
	if got := classifyResourceAction(applicant, applicant); got != raActionACL {
		t.Fatalf("same tenant: got %s", got)
	}
	if got := classifyResourceAction(store.DefaultTenantID, applicant); got != raActionAssign {
		t.Fatalf("default tenant: got %s", got)
	}
	if got := classifyResourceAction("tenant-other", applicant); got != raActionGrant {
		t.Fatalf("other tenant: got %s", got)
	}
	if got := classifyResourceAction("", applicant); got != raActionAssign {
		t.Fatalf("empty owner treated as default: got %s", got)
	}
}

func TestGrantResourceType(t *testing.T) {
	if grantResourceType(raTypeHost) != "instance" {
		t.Fatal("host should map to instance")
	}
	if grantResourceType(raTypeDatabase) != raTypeDatabase {
		t.Fatal("database unchanged")
	}
}

func TestAppendUniqueUsername(t *testing.T) {
	list := appendUniqueUsername(nil, "alice")
	list = appendUniqueUsername(list, "Alice")
	list = appendUniqueUsername(list, "bob")
	if len(list) != 2 {
		t.Fatalf("want 2, got %v", list)
	}
}

func TestResourceApplyNeedsPlatformAdmin(t *testing.T) {
	items := []ResourceApplyItem{{Action: raActionGrant}, {Action: raActionACL}}
	if resourceApplyNeedsPlatformAdmin(items) {
		t.Fatal("no assign")
	}
	items = append(items, ResourceApplyItem{Action: raActionAssign})
	if !resourceApplyNeedsPlatformAdmin(items) {
		t.Fatal("expect assign")
	}
}

func TestNormalizeResourceApplyIntent(t *testing.T) {
	if normalizeResourceApplyIntent("approver") != raIntentApprover {
		t.Fatal("approver")
	}
	if normalizeResourceApplyIntent("") != raIntentVisibility {
		t.Fatal("default visibility")
	}
	if normalizeResourceApplyIntent("visibility") != raIntentVisibility {
		t.Fatal("visibility")
	}
}

func TestResourceApplyHasApproverAction(t *testing.T) {
	if resourceApplyHasApproverAction([]ResourceApplyItem{{Action: raActionACL}}) {
		t.Fatal("no approver")
	}
	if !resourceApplyHasApproverAction([]ResourceApplyItem{{Action: raActionApprover}}) {
		t.Fatal("expect approver")
	}
}

func TestMergeApproverIntentAssignees(t *testing.T) {
	got := mergeApproverIntentAssignees([]string{"admin", "Admin"}, "wl_a, admin\nwl_b")
	if len(got) != 3 {
		t.Fatalf("want 3 unique, got %v", got)
	}
	if got[0] != "admin" || got[1] != "wl_a" || got[2] != "wl_b" {
		t.Fatalf("order/content: %v", got)
	}
	if len(mergeApproverIntentAssignees(nil, "")) != 0 {
		t.Fatal("empty")
	}
}

func TestIsDBATenant(t *testing.T) {
	if isDBATenant(store.DefaultTenantID, store.DefaultTenantCode) {
		t.Fatal("default tenant must not count as dba source")
	}
	if !isDBATenant("", "dba") {
		t.Fatal("dba code")
	}
	if !isDBATenant("any-id", "DBA") {
		t.Fatal("DBA case")
	}
	if isDBATenant("tenant-supply", "supply") {
		t.Fatal("business tenant should not be dba")
	}
}

func TestMergeResourceApplyGrantAssignees(t *testing.T) {
	got := mergeResourceApplyGrantAssignees(
		[]string{"admin"},
		"wl_a, admin",
		[]string{"abc", "Admin", "dba_ops"},
	)
	// admin, wl_a, abc, dba_ops
	if len(got) != 4 {
		t.Fatalf("want 4, got %v", got)
	}
	if got[0] != "admin" || got[1] != "wl_a" || got[2] != "abc" || got[3] != "dba_ops" {
		t.Fatalf("order: %v", got)
	}
}

func TestPickAssigneeFromPool(t *testing.T) {
	pool := []string{"admin", "abc", "wl_a"}
	got, err := pickAssigneeFromPool(pool, "ABC", "empty", "miss")
	if err != nil || got != "abc" {
		t.Fatalf("prefer case-insensitive: got=%q err=%v", got, err)
	}
	got, err = pickAssigneeFromPool(pool, "", "empty", "miss")
	if err != nil || got != "admin" {
		t.Fatalf("default first: got=%q err=%v", got, err)
	}
	_, err = pickAssigneeFromPool(pool, "outsider", "empty", "miss")
	if err == nil || !strings.Contains(err.Error(), "miss") {
		t.Fatalf("want miss err, got %v", err)
	}
	_, err = pickAssigneeFromPool(nil, "", "empty", "miss")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("want empty err, got %v", err)
	}
}

func TestResolveAssignee_CrossTenantGrantPool(t *testing.T) {
	db := resourceApplyTestStore(t)
	h := &ResourceApplyHandler{Store: db}

	pool, err := h.listResourceApplyGrantAssignees()
	if err != nil {
		t.Fatal(err)
	}
	if len(pool) == 0 {
		t.Fatal("grant pool empty: need platform admin, whitelist, or dba tenant members")
	}

	// 申请人租户 ≠ 资源归属 → grant 分支
	applicantTID := "tenant-applicant-not-owner"
	ownerTID := "tenant-supply-chain-owner"
	items := []ResourceApplyItem{{
		ResourceType:  raTypeDatabase,
		ResourceID:    "db-dummy",
		OwnerTenantID: ownerTID,
		Action:        raActionGrant,
	}}

	okName := pool[0]
	got, err := h.resolveAssignee(applicantTID, items, okName)
	if err != nil {
		t.Fatalf("pool member should pass: %v", err)
	}
	if !strings.EqualFold(got, okName) {
		t.Fatalf("got %q want %q", got, okName)
	}

	_, err = h.resolveAssignee(applicantTID, items, "not_in_grant_pool_zzz")
	if err == nil || !strings.Contains(err.Error(), "平台管理员、超级审核白名单或 dba 租户里的用户") {
		t.Fatalf("outsider want new grant err, got %v", err)
	}
	if strings.Contains(err.Error(), "资源归属租户成员") || strings.Contains(err.Error(), "默认租户成员") {
		t.Fatalf("legacy wording must be gone: %v", err)
	}

	// 划拨与跨租共用统一审批人池
	assignItems := []ResourceApplyItem{{
		ResourceType:  raTypeDatabase,
		ResourceID:    "db-assign",
		OwnerTenantID: store.DefaultTenantID,
		Action:        raActionAssign,
	}}
	got, err = h.resolveAssignee(applicantTID, assignItems, okName)
	if err != nil {
		t.Fatalf("assign pool member should pass: %v", err)
	}
	if !strings.EqualFold(got, okName) {
		t.Fatalf("assign got %q want %q", got, okName)
	}
	_, err = h.resolveAssignee(applicantTID, assignItems, "not_in_grant_pool_zzz")
	if err == nil || !strings.Contains(err.Error(), "平台管理员、超级审核白名单或 dba 租户里的用户") {
		t.Fatalf("assign outsider: %v", err)
	}

	// 划拨+跨租混合
	mixed := []ResourceApplyItem{
		{
			ResourceType:  raTypeDatabase,
			ResourceID:    "db-assign",
			OwnerTenantID: store.DefaultTenantID,
			Action:        raActionAssign,
		},
		{
			ResourceType:  raTypeDatabase,
			ResourceID:    "db-grant",
			OwnerTenantID: ownerTID,
			Action:        raActionGrant,
		},
	}
	got, err = h.resolveAssignee(applicantTID, mixed, okName)
	if err != nil {
		t.Fatalf("mixed pool member should pass: %v", err)
	}
	if !strings.EqualFold(got, okName) {
		t.Fatalf("mixed got %q want %q", got, okName)
	}
}

func resourceApplyTestStore(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run resource_apply store tests")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestApplyApprover_SameTenantPrivateAndIdempotent(t *testing.T) {
	db := resourceApplyTestStore(t)
	h := &ResourceApplyHandler{Store: db}
	suf := time.Now().Format("150405.000")
	code := "raappr" + strings.ReplaceAll(suf, ".", "")
	uname := "ra_user_" + code

	dev, err := db.GetRoleByCode("developer")
	if err != nil || dev == nil {
		t.Fatal("developer role required")
	}
	ten := &store.Tenant{Code: code, Name: code, Status: store.TenantStatusActive}
	if err := db.CreateTenant(ten); err != nil {
		t.Fatal(err)
	}
	tid := ten.ID
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id=?`, tid) })

	u := &store.User{Username: uname, DisplayName: uname, Role: "developer", RoleID: dev.ID}
	if err := db.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_users WHERE id=?`, u.ID)
	})
	if err := db.UpsertTenantMember(&store.TenantMember{
		TenantID: tid, UserID: u.ID, RoleIDs: []string{dev.ID}, Status: store.MemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	inst := &store.Instance{
		TenantID: tid, Name: "ra-inst-" + code,
		DomainName: "ra-" + code + ".local", Port: 5432, DBType: "postgres", Host: "127.0.0.1", IDC: "default",
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID) })

	row := &store.Database{
		TenantID: tid, InstanceID: inst.ID, DBName: "ra_db_" + code,
		Visibility: "private",
	}
	if err := db.CreateDatabase(row); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_databases WHERE id=?`, row.ID) })

	item := ResourceApplyItem{
		ResourceType: raTypeDatabase, ResourceID: row.ID, Action: raActionApprover,
		TargetUsers: []string{uname},
	}
	msg, err := h.applyApprover(item, tid, uname)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "审核人") || !strings.Contains(msg, "可见") {
		t.Fatalf("want approver+acl msg, got %s", msg)
	}
	got, err := db.GetDatabase(row.ID)
	if err != nil || got == nil {
		t.Fatalf("reload: %v", err)
	}
	if !usernameInList(uname, got.Approvers) {
		t.Fatalf("approvers missing user: %v", got.Approvers)
	}
	if !usernameInList(uname, got.AccessUsers) {
		t.Fatalf("access_users missing user: %v", got.AccessUsers)
	}

	msg2, err := h.applyApprover(item, tid, uname)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg2, "已在名单中") {
		t.Fatalf("idempotent want already-in-list, got %s", msg2)
	}

	// 空目标用户拒绝
	_, err = h.applyApprover(ResourceApplyItem{
		ResourceType: raTypeDatabase, ResourceID: row.ID, Action: raActionApprover,
	}, tid, uname)
	if err == nil || !strings.Contains(err.Error(), "库审核人为空") {
		t.Fatalf("empty targets want reject, got %v", err)
	}

	// 跨租：申请租户与库归属不一致则拒绝
	_, err = h.applyApprover(item, "other-tenant-not-exist", uname)
	if err == nil {
		t.Fatal("cross-tenant want reject")
	}
}

func TestBuildItems_ApproverRejectsForeignDB(t *testing.T) {
	db := resourceApplyTestStore(t)
	h := &ResourceApplyHandler{Store: db}
	suf := time.Now().Format("150405.000")
	codeOther := "raoth" + strings.ReplaceAll(suf, ".", "")
	codeBiz := "rabiz" + strings.ReplaceAll(suf, ".", "")

	otherTen := &store.Tenant{Code: codeOther, Name: codeOther, Status: store.TenantStatusActive}
	if err := db.CreateTenant(otherTen); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id=?`, otherTen.ID) })
	bizTen := &store.Tenant{Code: codeBiz, Name: codeBiz, Status: store.TenantStatusActive}
	if err := db.CreateTenant(bizTen); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id=?`, bizTen.ID) })

	inst := &store.Instance{
		TenantID: otherTen.ID, Name: "ra-f-" + codeOther,
		DomainName: "raf-" + codeOther + ".local", Port: 5432, DBType: "postgres", Host: "127.0.0.1", IDC: "default",
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID) })
	row := &store.Database{
		TenantID: otherTen.ID, InstanceID: inst.ID, DBName: "ra_fdb_" + codeOther,
		Approvers: []string{"x"}, Visibility: "global",
	}
	if err := db.CreateDatabase(row); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_databases WHERE id=?`, row.ID) })

	_, err := h.buildItems(bizTen.ID, raIntentApprover, []resourceApplySubmitItem{
		{ResourceType: raTypeDatabase, ResourceID: row.ID},
	}, []string{"someone"})
	if err == nil || !strings.Contains(err.Error(), "仅可申请本租户") {
		t.Fatalf("want same-tenant reject, got %v", err)
	}
}

func TestBuildItems_ApproverRequiresTargetUsers(t *testing.T) {
	h := &ResourceApplyHandler{}
	_, err := h.buildItems("tid", raIntentApprover, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "至少选择一项") {
		t.Fatalf("empty items: %v", err)
	}
}

func TestUniqueTrimmedNamesForTargets(t *testing.T) {
	got := uniqueTrimmedNames([]string{" a ", "A", "b", ""})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}
