package handler

import (
	"os"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/store"
)

// 平台管理员/白名单可跨租列出平台资源申请；普通用户不可见异租单。
func TestResourceApplyCrossTenantPlatformReviewer(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run resource_apply cross-tenant tests")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		if strings.Contains(err.Error(), "must be owner") {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	suf := time.Now().Format("150405.000")
	code := "rax" + strings.ReplaceAll(suf, ".", "")
	ten := &store.Tenant{Code: code, Name: code, Status: store.TenantStatusActive}
	if err := db.CreateTenant(ten); err != nil {
		t.Fatal(err)
	}
	tid := ten.ID
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_issues WHERE tenant_id=?`, tid)
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE tenant_id=?`, tid)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id=?`, tid)
	})

	issue := &store.Issue{
		IssueType:   store.IssueTypeResourceApply,
		Type:        store.IssueTypeResourceApply,
		SQLContent:  `{"kind":"resource_apply","intent":"visibility","items":[]}`,
		Description: "cross-tenant ra " + code,
		Status:      store.StatusPending,
		Submitter:   "dev_" + code,
		AssignedTo:  "admin",
		TenantID:    tid,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}

	all, err := db.ListResourceApplyAcrossTenants(store.StatusPending, code)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range all {
		if it.ID == issue.ID {
			found = true
			if it.TenantID != tid {
				t.Fatalf("tenant_id=%q want %q", it.TenantID, tid)
			}
			break
		}
	}
	if !found {
		t.Fatal("ListResourceApplyAcrossTenants should return issue from other tenant")
	}

	// 本租列表（default）不应仅靠租户过滤看到该单
	local, err := db.ListIssuesInTenant(store.DefaultTenantID, store.IssueTypeResourceApply, store.StatusPending, code, "nobody", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range local {
		if it.ID == issue.ID {
			t.Fatal("default-tenant list must not include other-tenant issue without platform merge")
		}
	}

	todos, total, err := db.DashboardTodos(store.DashboardFilter{TenantID: store.DefaultTenantID}, "admin", 1, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	if total < 1 {
		t.Fatal("todos pending_count should include cross-tenant resource_apply")
	}
	todoHit := false
	for _, td := range todos {
		if td.ID == issue.ID {
			todoHit = true
			break
		}
	}
	if !todoHit {
		// 可能分页外；再拉更大页或用 q
		todos, _, err = db.DashboardTodos(store.DashboardFilter{TenantID: store.DefaultTenantID, Q: code}, "admin", 1, 100, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, td := range todos {
			if td.ID == issue.ID {
				todoHit = true
				break
			}
		}
	}
	if !todoHit {
		t.Fatal("DashboardTodos with seeAllResourceApply should include issue")
	}

	todosNo, _, err := db.DashboardTodos(store.DashboardFilter{TenantID: store.DefaultTenantID, Q: code}, "stranger_"+code, 1, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, td := range todosNo {
		if td.ID == issue.ID {
			t.Fatal("non-reviewer must not see cross-tenant resource_apply in todos")
		}
	}

	got, err := db.GetIssue(issue.ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.TenantID != tid {
		t.Fatalf("GetIssue tenant_id=%q", got.TenantID)
	}
	if !db.CanApproveResourceApply(got, "admin", "", true) {
		t.Fatal("privileged admin should approve resource_apply")
	}
}

// dba 等跨租审批人：非平台审核人，仅因 assigned_to=自己，应在本租待办/列表看到 resource_apply；其它类型不受影响。
func TestResourceApplyAssignedToAcrossTenantsVisible(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run resource_apply cross-tenant tests")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		if strings.Contains(err.Error(), "must be owner") {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	suf := time.Now().Format("150405.000")
	code := "raasg" + strings.ReplaceAll(suf, ".", "")
	assignee := "dba_user_" + code
	viewerTenant := store.DefaultTenantID // 模拟审批人处在与申请人不同的租户

	ten := &store.Tenant{Code: code, Name: code, Status: store.TenantStatusActive}
	if err := db.CreateTenant(ten); err != nil {
		t.Fatal(err)
	}
	tid := ten.ID
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_issues WHERE tenant_id=? OR description LIKE ?`, tid, "%"+code+"%")
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE tenant_id=?`, tid)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id=?`, tid)
	})

	ra := &store.Issue{
		IssueType:   store.IssueTypeResourceApply,
		Type:        store.IssueTypeResourceApply,
		SQLContent:  `{"kind":"resource_apply","intent":"visibility","items":[]}`,
		Description: "assigned-across ra " + code,
		Status:      store.StatusPending,
		Submitter:   "dev_" + code,
		AssignedTo:  assignee,
		TenantID:    tid,
	}
	if err := db.CreateIssue(ra); err != nil {
		t.Fatal(err)
	}

	// 同租户下再放一张 DDL，指派给同一人：跨租 merge 不得把它从异租「变」可见；本测用申请人租户外的 viewerTenant 查 DDL 应看不到
	ddl := &store.Issue{
		IssueType:   store.IssueTypeDDL,
		Type:        store.IssueTypeDDL,
		SQLContent:  "SELECT 1",
		Description: "assigned-across ddl " + code,
		Status:      store.StatusPending,
		Submitter:   "dev_" + code,
		AssignedTo:  assignee,
		TenantID:    tid,
	}
	if err := db.CreateIssue(ddl); err != nil {
		t.Fatal(err)
	}

	hit, err := db.ListResourceApplyAssignedToAcrossTenants(assignee, store.StatusPending, code)
	if err != nil {
		t.Fatal(err)
	}
	foundRA, foundDDL := false, false
	for _, it := range hit {
		if it.ID == ra.ID {
			foundRA = true
		}
		if it.ID == ddl.ID {
			foundDDL = true
		}
	}
	if !foundRA {
		t.Fatal("ListResourceApplyAssignedToAcrossTenants should return assigned resource_apply")
	}
	if foundDDL {
		t.Fatal("ListResourceApplyAssignedToAcrossTenants must not return DDL")
	}

	// 审批人当前租户 ≠ 工单租户、非平台审核人
	todos, _, err := db.DashboardTodos(store.DashboardFilter{TenantID: viewerTenant, Q: code}, assignee, 1, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	todoRA, todoDDL := false, false
	for _, td := range todos {
		if td.ID == ra.ID {
			todoRA = true
		}
		if td.ID == ddl.ID {
			todoDDL = true
		}
	}
	if !todoRA {
		t.Fatal("DashboardTodos should include assigned cross-tenant resource_apply for assignee")
	}
	if todoDDL {
		t.Fatal("DashboardTodos must not surface cross-tenant DDL via resource_apply merge")
	}

	todosStranger, _, err := db.DashboardTodos(store.DashboardFilter{TenantID: viewerTenant, Q: code}, "stranger_"+code, 1, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, td := range todosStranger {
		if td.ID == ra.ID {
			t.Fatal("non-assignee must not see cross-tenant resource_apply")
		}
	}

	local, err := db.ListIssuesInTenant(viewerTenant, store.IssueTypeResourceApply, store.StatusPending, code, assignee, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range local {
		if it.ID == ra.ID {
			t.Fatal("same-tenant list alone must not include other-tenant issue")
		}
	}
}
