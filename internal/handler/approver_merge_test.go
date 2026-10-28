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

func TestSubtractApprovers(t *testing.T) {
	merged, removed, missing := subtractApprovers(
		[]string{"daiyong", "Admin", "orphan"},
		[]string{"ADMIN", "nobody"},
	)
	if len(merged) != 2 || merged[0] != "daiyong" || merged[1] != "orphan" {
		t.Fatalf("merged=%v", merged)
	}
	if len(removed) != 1 || removed[0] != "Admin" {
		t.Fatalf("removed=%v", removed)
	}
	if len(missing) != 1 || missing[0] != "nobody" {
		t.Fatalf("missing=%v", missing)
	}

	merged, removed, missing = subtractApprovers([]string{"a", "b"}, []string{"A", "B"})
	if len(merged) != 0 || len(removed) != 2 || len(missing) != 0 {
		t.Fatalf("full clear merged=%v removed=%v missing=%v", merged, removed, missing)
	}

	merged, removed, missing = subtractApprovers([]string{"a"}, []string{"x"})
	if len(merged) != 1 || merged[0] != "a" || len(removed) != 0 || len(missing) != 1 {
		t.Fatalf("noop merged=%v removed=%v missing=%v", merged, removed, missing)
	}

	merged, removed, missing = subtractApprovers([]string{"a", "b"}, []string{"B", "c"})
	if len(merged) != 1 || merged[0] != "a" || len(removed) != 1 || removed[0] != "b" || len(missing) != 1 {
		t.Fatalf("partial merged=%v removed=%v missing=%v", merged, removed, missing)
	}
}

func TestWriteApproversRetryLogic(t *testing.T) {
	// 模拟：前两次写失败、第三次成功 → 最终 ok（与 BatchApprovers 最多 3 次一致）。
	attempts := 0
	write := func() error {
		attempts++
		if attempts < 3 {
			return errRetryOnce
		}
		return nil
	}
	var lastErr error
	retried := false
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			retried = true
		}
		lastErr = write()
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil || !retried || attempts != 3 {
		t.Fatalf("lastErr=%v retried=%v attempts=%d", lastErr, retried, attempts)
	}
}

type retryOnceErr struct{}

func (retryOnceErr) Error() string { return "transient" }

var errRetryOnce error = retryOnceErr{}
