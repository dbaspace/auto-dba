package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindNamedDir(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "bin"), 0o755)
	if got := findNamedDir(root, "bin"); got != filepath.Join(root, "bin") {
		t.Fatalf("direct: %s", got)
	}
	nested := t.TempDir()
	_ = os.MkdirAll(filepath.Join(nested, "StarRocks-x", "lib"), 0o755)
	if got := findNamedDir(nested, "lib"); got != filepath.Join(nested, "StarRocks-x", "lib") {
		t.Fatalf("nested: %s", got)
	}
	if findNamedDir(nested, "missing") != "" {
		t.Fatal("expected empty")
	}
}

func TestReplaceSubdirsInPlace(t *testing.T) {
	target := t.TempDir()
	extract := t.TempDir()
	_ = os.MkdirAll(filepath.Join(target, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(target, "bin", "old"), []byte("old"), 0o644)
	_ = os.MkdirAll(filepath.Join(target, "conf"), 0o755)
	_ = os.WriteFile(filepath.Join(target, "conf", "keep"), []byte("keep"), 0o644)
	_ = os.MkdirAll(filepath.Join(extract, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(extract, "bin", "new"), []byte("new"), 0o644)
	_ = os.MkdirAll(filepath.Join(extract, "lib"), 0o755)
	_ = os.WriteFile(filepath.Join(extract, "lib", "x"), []byte("x"), 0o644)

	msg, errMsg := replaceSubdirsInPlace(target, extract, []string{"bin", "lib"}, "ut1")
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if _, err := os.Stat(filepath.Join(target, "bak.ut1", "bin", "old")); err != nil {
		t.Fatalf("backup missing: %v msg=%s", err, msg)
	}
	if _, err := os.Stat(filepath.Join(target, "bin", "new")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "lib", "x")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "conf", "keep")); err != nil {
		t.Fatal("conf should remain")
	}
}
