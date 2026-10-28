package handler

import "testing"

func TestMergeDatabaseApproversLogicViaKeep(t *testing.T) {
	// 纯函数层：uniqueTrimmedNames + 合并顺序
	exist := uniqueTrimmedNames([]string{"daiyong", "admin"})
	add := uniqueTrimmedNames([]string{"gaoxiang", "daiyong"})
	// 模拟已过滤后的 keptExist / keptAdd
	keptExist := []string{"daiyong"} // admin 被剥掉
	keptAdd := []string{"gaoxiang", "daiyong"}
	merged := uniqueTrimmedNames(append(keptExist, keptAdd...))
	if len(merged) != 2 || merged[0] != "daiyong" || merged[1] != "gaoxiang" {
		t.Fatalf("merged=%v", merged)
	}
	_ = exist
	_ = add
}

func TestUniqueTrimmedNames(t *testing.T) {
	got := uniqueTrimmedNames([]string{" admin ", "Admin", "daiyong", ""})
	if len(got) != 2 || got[0] != "admin" || got[1] != "daiyong" {
		t.Fatalf("got=%v", got)
	}
}
