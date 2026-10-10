package store

import "testing"

func TestBuildMenuTreeSortsSiblingsAfterParentAppend(t *testing.T) {
	// 模拟角色只勾了叶子，父目录被 append 到切片末尾
	list := []Menu{
		{ID: "m-db-config", Title: "资源配置", SortOrder: 3, Enabled: true},
		{ID: "m-cfg-inst", ParentID: "m-db-config", Title: "实例管理", Path: "/config/instances", SortOrder: 1, Enabled: true},
		{ID: "m-data-query", Title: "数据查询", SortOrder: 5, Enabled: true},
		{ID: "m-query", ParentID: "m-data-query", Title: "数据库", Path: "/ops/query", SortOrder: 1, Enabled: true},
		// 父目录配置中心后补，故意放在最后
		{ID: "m-cfg", Title: "配置中心", SortOrder: 2, Enabled: true},
		{ID: "m-cfg-args", ParentID: "m-cfg", Title: "全局参数", Path: "/config/args", SortOrder: 1, Enabled: true},
		{ID: "m-dashboard", Title: "仪表盘", Path: "/ops/dashboard", SortOrder: 1, Enabled: true},
	}
	tree := buildMenuTree(list, "", true, true)
	want := []string{"m-dashboard", "m-cfg", "m-db-config", "m-data-query"}
	if len(tree) != len(want) {
		t.Fatalf("len=%d tree=%+v", len(tree), tree)
	}
	for i, id := range want {
		if tree[i].ID != id {
			t.Fatalf("pos %d got %s want %s (order=%v)", i, tree[i].ID, id, titles(tree))
		}
	}
}

func titles(nodes []MenuNode) []string {
	out := make([]string, len(nodes))
	for i := range nodes {
		out[i] = nodes[i].Title
	}
	return out
}
