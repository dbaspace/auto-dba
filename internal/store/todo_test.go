package store

import (
	"testing"
	"time"
)

func TestNormalizeTodo(t *testing.T) {
	td := &Todo{Title: " 磁盘告警 ", Owner: "alice", PlanAt: "2026-10-01T18:00:00+08:00", Followers: []string{"bob", " bob", "", "carol"}}
	if err := NormalizeTodo(td); err != nil {
		t.Fatal(err)
	}
	if td.Title != "磁盘告警" || td.Source != TodoSourceManual || td.Status != TodoStatusPending {
		t.Fatalf("%+v", td)
	}
	if td.PlanAt != "2026-10-01T10:00:00Z" {
		t.Fatalf("plan_at 应转 UTC: %s", td.PlanAt)
	}
	if len(td.Followers) != 2 {
		t.Fatalf("followers=%v", td.Followers)
	}
	for _, bad := range []*Todo{
		{Owner: "a", PlanAt: "2026-10-01 10:00"},
		{Title: "x", PlanAt: "2026-10-01 10:00"},
		{Title: "x", Owner: "a"},
		{Title: "x", Owner: "a", PlanAt: "2026-10-01 10:00", Source: "email"},
		{Title: "x", Owner: "a", PlanAt: "明天"},
	} {
		if err := NormalizeTodo(bad); err == nil {
			t.Fatalf("应校验失败: %+v", bad)
		}
	}
}

func TestTodoTabsAndOverdue(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	list := []Todo{
		{ID: "1", Owner: "alice", CreatedBy: "bob", Status: TodoStatusPending, PlanAt: "2026-09-30T12:00:00Z", UpdatedAt: "b"},
		{ID: "2", Owner: "bob", Followers: []string{"alice"}, CreatedBy: "alice", Status: TodoStatusFollowing, PlanAt: "2026-10-02T12:00:00Z", UpdatedAt: "c"},
		{ID: "3", Owner: "alice", CreatedBy: "alice", Status: TodoStatusClosed, PlanAt: "2026-09-01T00:00:00Z", ClosedAt: "2026-09-02T00:00:00Z"},
		{ID: "4", Owner: "carol", CreatedBy: "carol", Status: TodoStatusHandover, PlanAt: "2026-10-10T00:00:00Z", UpdatedAt: "a"},
	}
	counts := TodoTabCounts(list, "alice", now)
	want := map[string]int{TodoTabMine: 1, TodoTabFollow: 1, TodoTabCreated: 2, TodoTabTeam: 3, TodoTabClosed: 1, TodoTabWarning: 2}
	for k, v := range want {
		if counts[k] != v {
			t.Fatalf("%s=%d want %d (%v)", k, counts[k], v, counts)
		}
	}

	warn := FilterTodos(list, TodoFilter{Tab: TodoTabWarning}, "alice", now)
	if len(warn) != 2 || warn[0].ID != "1" || !warn[0].Overdue || warn[0].DisplayStatus != TodoStatusOverdue || !warn[1].DueSoon {
		t.Fatalf("warning=%+v", warn)
	}
	if got := FilterTodos(list, TodoFilter{Tab: TodoTabClosed}, "alice", now); got[0].Overdue {
		t.Fatalf("已闭环不算超期")
	}
	team := FilterTodos(list, TodoFilter{Tab: TodoTabTeam, Status: TodoStatusOverdue}, "alice", now)
	if len(team) != 1 || team[0].ID != "1" {
		t.Fatalf("按已超期筛选: %+v", team)
	}
	team = FilterTodos(list, TodoFilter{Tab: TodoTabTeam}, "alice", now)
	if team[0].ID != "2" || team[2].ID != "4" {
		t.Fatalf("团队待办按更新时间倒序: %+v", team)
	}
}

func TestTodoTransitions(t *testing.T) {
	td := &Todo{Owner: "alice", Status: TodoStatusPending}
	MarkTodoFollowing(td)
	if td.Status != TodoStatusFollowing {
		t.Fatalf("待处理有子任务进展应转跟进中: %s", td.Status)
	}
	if err := HandoverTodo(td, "alice"); err == nil {
		t.Fatal("不能转交给自己")
	}
	if err := HandoverTodo(td, "bob"); err != nil || td.Owner != "bob" || td.Status != TodoStatusHandover || td.Followers[0] != "alice" {
		t.Fatalf("handover: %v %+v", err, td)
	}
	MarkTodoFollowing(td)
	if td.Status != TodoStatusFollowing {
		t.Fatalf("待交接有子任务进展应转跟进中: %s", td.Status)
	}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if err := CloseTodo(td, "已解决", 2, now); err == nil || td.Status == TodoStatusClosed {
		t.Fatalf("有未完成子任务不能闭环: %v", err)
	}
	if err := CloseTodo(td, "已解决", 0, now); err != nil || td.Status != TodoStatusClosed || td.ClosedAt != "2026-10-01T04:00:00Z" || td.LatestProgress != "已解决" {
		t.Fatalf("close: %v %+v", err, td)
	}
	if err := CloseTodo(td, "", 0, now); err == nil {
		t.Fatal("已闭环不可重复闭环")
	}
	if err := ReopenTodo(td); err != nil || td.Status != TodoStatusFollowing || td.ClosedAt != "" {
		t.Fatalf("reopen: %v %+v", err, td)
	}
}

func TestMaybeAutoCloseTodo(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	td := &Todo{Status: TodoStatusFollowing, LatestProgress: "【子任务】已完成", SubtaskTotal: 3, SubtaskDone: 3}
	closed, err := MaybeAutoCloseTodo(td, now)
	if err != nil || !closed || td.Status != TodoStatusClosed || td.ClosedAt != "2026-10-01T12:00:00Z" {
		t.Fatalf("全部完成应自动闭环: %v closed=%v %+v", err, closed, td)
	}
	if td.LatestProgress != "【子任务】已完成" {
		t.Fatalf("自动闭环不覆盖最新进展: %s", td.LatestProgress)
	}
	closed, err = MaybeAutoCloseTodo(td, now.Add(time.Hour))
	if err != nil || closed || td.ClosedAt != "2026-10-01T12:00:00Z" {
		t.Fatalf("已闭环应幂等: %v closed=%v %+v", err, closed, td)
	}

	open := &Todo{Status: TodoStatusFollowing, SubtaskTotal: 3, SubtaskDone: 2}
	closed, err = MaybeAutoCloseTodo(open, now)
	if err != nil || closed || open.Status != TodoStatusFollowing {
		t.Fatalf("尚有未完成不应闭环: %v closed=%v %+v", err, closed, open)
	}

	empty := &Todo{Status: TodoStatusFollowing, SubtaskTotal: 0, SubtaskDone: 0}
	closed, err = MaybeAutoCloseTodo(empty, now)
	if err != nil || closed || empty.Status != TodoStatusFollowing {
		t.Fatalf("无子任务不应自动闭环: %v closed=%v %+v", err, closed, empty)
	}
}

func TestNormalizeSubtask(t *testing.T) {
	s := &TodoSubtask{Title: " 注册集群 ", PlanAt: "2026-10-01T18:00:00+08:00"}
	if err := NormalizeSubtask(s, "alice"); err != nil {
		t.Fatal(err)
	}
	if s.Title != "注册集群" || s.Owner != "alice" || s.Status != SubtaskPending || s.PlanAt != "2026-10-01T10:00:00Z" {
		t.Fatalf("%+v", s)
	}
	noPlan := &TodoSubtask{Title: "x", Owner: "bob"}
	if err := NormalizeSubtask(noPlan, "alice"); err != nil || noPlan.Owner != "bob" || noPlan.PlanAt != "" {
		t.Fatalf("计划时间可选、负责人不被覆盖: %v %+v", err, noPlan)
	}
	for _, bad := range []*TodoSubtask{{}, {Title: "x", Status: "closed"}, {Title: "x", PlanAt: "明天"}} {
		if err := NormalizeSubtask(bad, "alice"); err == nil {
			t.Fatalf("应校验失败: %+v", bad)
		}
	}
}

func TestApplySubtaskProgress(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	s := &TodoSubtask{Title: "x", Status: SubtaskPending}
	if err := ApplySubtaskProgress(s, "", " ", now); err == nil {
		t.Fatal("进展说明必填")
	}
	if err := ApplySubtaskProgress(s, "", "开始处理", now); err != nil || s.Status != SubtaskDoing || s.Progress != "开始处理" {
		t.Fatalf("待处理未指定状态应转进行中: %v %+v", err, s)
	}
	if err := ApplySubtaskProgress(s, "closed", "x", now); err == nil {
		t.Fatal("非法状态")
	}
	if err := ApplySubtaskProgress(s, SubtaskDone, "已完成", now); err != nil || s.DoneAt != "2026-10-01T12:00:00Z" {
		t.Fatalf("完成应记录时间: %v %+v", err, s)
	}
	if err := ApplySubtaskProgress(s, SubtaskDone, "补充说明", now.Add(time.Hour)); err != nil || s.DoneAt != "2026-10-01T12:00:00Z" {
		t.Fatalf("已完成再次更新不改完成时间: %+v", s)
	}
	if err := ApplySubtaskProgress(s, SubtaskDoing, "返工", now); err != nil || s.DoneAt != "" || s.Status != SubtaskDoing {
		t.Fatalf("退回应清空完成时间: %+v", s)
	}
}

func TestSummarizeSubtasks(t *testing.T) {
	td := &Todo{LatestProgress: "旧进展", NextAction: "旧动作"}
	SummarizeSubtasks(td, []TodoSubtask{
		{Seq: 2, Title: "注册集群", Status: SubtaskPending},
		{Seq: 1, Title: "版本历史", Status: SubtaskPending},
	})
	if td.LatestProgress != "旧进展" || td.NextAction != "1、版本历史\n2、注册集群" || td.SubtaskTotal != 2 || td.SubtaskDone != 0 {
		t.Fatalf("无进展时保留最新进展、下一步按顺序编号: %+v", td)
	}
	SummarizeSubtasks(td, []TodoSubtask{
		{Seq: 1, Title: "版本历史", Status: SubtaskDone, Progress: "已上线", UpdatedAt: "2026-10-01T01:00:00Z"},
		{Seq: 2, Title: "注册集群", Status: SubtaskDoing, Progress: "开发中", UpdatedAt: "2026-10-01T02:00:00Z"},
		{Seq: 3, Title: "超链接", Status: SubtaskPending},
	})
	if td.LatestProgress != "【注册集群】开发中" || td.NextAction != "1、注册集群\n2、超链接" || td.SubtaskDone != 1 || td.SubtaskTotal != 3 {
		t.Fatalf("summary: %+v", td)
	}
	SummarizeSubtasks(td, nil)
	if td.NextAction != "" || td.SubtaskTotal != 0 {
		t.Fatalf("无子任务时下一步为空: %+v", td)
	}
}

func TestMenuTreeKeepsEmptyGroupDropsActions(t *testing.T) {
	list := []Menu{
		{ID: "g-empty", Title: "待办跟进", Enabled: true},
		{ID: "g", Title: "目录", Enabled: true},
		{ID: "p", ParentID: "g", Title: "页面", Path: "/x", Enabled: true},
		{ID: "a", ParentID: "p", Title: "新建", URN: "um.action.x.create", Enabled: true},
		{ID: "off", Title: "停用目录", Enabled: false},
	}
	tree := buildMenuTree(list, "", true, true)
	if len(tree) != 2 || tree[0].ID != "g-empty" || len(tree[0].Children) != 0 {
		t.Fatalf("空目录应保留、停用目录应去掉: %+v", tree)
	}
	if page := tree[1].Children[0]; page.ID != "p" || len(page.Children) != 0 {
		t.Fatalf("操作节点不进侧栏菜单: %+v", page)
	}
	page, actions := pageActionMenus(list, "/x")
	if page == nil || page.ID != "p" || len(actions) != 1 || actions[0].ID != "a" {
		t.Fatalf("page=%v actions=%v", page, actions)
	}
	if page, _ := pageActionMenus(list, "/none"); page != nil {
		t.Fatal("未登记路径不应命中")
	}
}
