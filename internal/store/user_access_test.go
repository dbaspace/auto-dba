package store

import "testing"

func TestEnabledMenuURNsAndPlatformFilter(t *testing.T) {
	all := []Menu{
		{ID: "1", Title: "a", URN: "um.sidebar.x", Enabled: true},
		{ID: "2", Title: "b", URN: "um.action.tenants.create", Enabled: true},
		{ID: "3", Title: "c", URN: "um.sidebar.tenants", Enabled: true},
		{ID: "4", Title: "d", URN: "um.sidebar.y", Enabled: false},
		{ID: "5", Title: "e", URN: "", Enabled: true},
	}
	urns := enabledMenuURNs(all)
	if len(urns) != 3 {
		t.Fatalf("enabled urns=%v", urns)
	}
	plat := platformURNsFromMenus(all)
	if len(plat) != 2 {
		t.Fatalf("platform urns=%v", plat)
	}
	filtered := filterPlatformURNs(urns)
	if len(filtered) != 2 {
		t.Fatalf("filterPlatformURNs=%v", filtered)
	}
}

func TestFilterMenuTreeByAllowKeepsParents(t *testing.T) {
	all := []Menu{
		{ID: "p", ParentID: "", Title: "父", Path: "", SortOrder: 1, Enabled: true},
		{ID: "c", ParentID: "p", Title: "子", Path: "/c", SortOrder: 1, Enabled: true, URN: "um.sidebar.c"},
	}
	tree := filterMenuTreeByAllow(all, map[string]bool{"c": true}, true)
	if len(tree) != 1 || tree[0].ID != "p" || len(tree[0].Children) != 1 {
		t.Fatalf("tree=%+v", tree)
	}
}
