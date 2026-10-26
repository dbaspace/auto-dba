package sqlexec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveArtifactInTempDir(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "deploy", "sql-agent", "bin", "sql-agent")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveArtifactIn([]string{root}); got != bin {
		t.Fatalf("got %q want %q", got, bin)
	}
	if got := resolveArtifactIn([]string{t.TempDir()}); got != "" {
		t.Fatalf("empty dir matched %q", got)
	}
}

func TestResolveArtifactUsesWorkingDirWithoutGoMod(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "deploy", "sql-agent", "bin", "sql-agent")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if got := ResolveArtifact(); filepath.Clean(got) != filepath.Clean(bin) {
		gotReal, _ := filepath.EvalSymlinks(got)
		wantReal, _ := filepath.EvalSymlinks(bin)
		if gotReal != wantReal {
			t.Fatalf("got %q want %q", got, bin)
		}
	}
}

func TestModuleRootFromWithoutGoMod(t *testing.T) {
	dir := t.TempDir()
	got := moduleRootFrom(dir)
	if got == "" {
		return
	}
	if got == filepath.Dir(got) {
		t.Fatalf("module root fell through to filesystem root %q", got)
	}
	if _, err := os.Stat(filepath.Join(got, "go.mod")); err != nil {
		t.Fatalf("module root %q has no go.mod", got)
	}
}
