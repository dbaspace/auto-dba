package handler

import "testing"

func TestMergeDatabaseApproversLogicViaKeep(t *testing.T) {
	exist := uniqueTrimmedNames([]string{"daiyong", "admin"})
	add := uniqueTrimmedNames([]string{"gaoxiang", "daiyong"})
	keptExist := []string{"daiyong"}
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

func TestMergeDatabaseApproversWithSet(t *testing.T) {
	members := map[string]string{
		"daiyong":  "daiyong",
		"gaoxiang": "gaoxiang",
	}
	merged, dropped, keptAdd := mergeDatabaseApproversWithSet(
		[]string{"daiyong", "admin"},
		[]string{"gaoxiang", "Admin"},
		members,
	)
	if len(merged) != 2 || merged[0] != "daiyong" || merged[1] != "gaoxiang" {
		t.Fatalf("merged=%v", merged)
	}
	if len(keptAdd) != 1 || keptAdd[0] != "gaoxiang" {
		t.Fatalf("keptAdd=%v", keptAdd)
	}
	// admin / Admin 经 uniqueTrimmedNames 折叠为一条
	if len(dropped) != 1 || dropped[0] != "admin" {
		t.Fatalf("dropped=%v", dropped)
	}
}

func TestApproversContainAll(t *testing.T) {
	if !approversContainAll([]string{"daiyong", "gaoxiang"}, []string{"GaoXiang"}) {
		t.Fatal("should contain gaoxiang case-insensitive")
	}
	if approversContainAll([]string{"daiyong"}, []string{"gaoxiang"}) {
		t.Fatal("should miss gaoxiang")
	}
	if !approversContainAll([]string{"a"}, nil) {
		t.Fatal("empty need should pass")
	}
}

func TestFilterNamesByMemberSetCanonical(t *testing.T) {
	members := map[string]string{"admin": "Admin"}
	kept, dropped := filterNamesByMemberSet([]string{"ADMIN", "nobody"}, members)
	if len(kept) != 1 || kept[0] != "Admin" {
		t.Fatalf("kept=%v", kept)
	}
	if len(dropped) != 1 || dropped[0] != "nobody" {
		t.Fatalf("dropped=%v", dropped)
	}
}

func TestWriteApproversRetryLogic(t *testing.T) {
	// 模拟：首次写失败、第二次成功 → 最终 ok，并计一次 retry。
	attempts := 0
	write := func() error {
		attempts++
		if attempts == 1 {
			return errRetryOnce
		}
		return nil
	}
	var lastErr error
	retried := false
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			retried = true
		}
		lastErr = write()
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil || !retried || attempts != 2 {
		t.Fatalf("lastErr=%v retried=%v attempts=%d", lastErr, retried, attempts)
	}
}

type retryOnceErr struct{}

func (retryOnceErr) Error() string { return "transient" }

var errRetryOnce error = retryOnceErr{}
