package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeJDK(t *testing.T, home, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho 'openjdk version \"" + version + "\"' 1>&2\n"
	if err := os.WriteFile(filepath.Join(home, "bin", "java"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestScanJavaCandidatesPreferred(t *testing.T) {
	root := t.TempDir()
	pref := filepath.Join(root, "jdk")
	fakeJDK(t, pref, "11.0.26")
	fakeJDK(t, filepath.Join(root, "jdk-21.0.2"), "21.0.2")
	fakeJDK(t, filepath.Join(root, "jdk-8"), "1.8.0_392")

	got := scanJavaCandidatesIn(pref, []string{filepath.Join(root, "jdk-*")}, 11, 0)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Home != pref || !got[0].Preferred || got[0].Major != 11 {
		t.Fatalf("preferred /home/jdk should come first: %+v", got)
	}
	if got[1].Major != 21 || got[1].Preferred {
		t.Fatalf("%+v", got[1])
	}
}

func TestSystemJavaInfo(t *testing.T) {
	root := t.TempDir()
	fakeJDK(t, root, "1.8.0_392")
	info := systemJavaInfo(filepath.Join(root, "bin", "java"))
	if info["system_checked"] != true || info["system_major"] != 8 {
		t.Fatalf("%+v", info)
	}
	missing := systemJavaInfo(filepath.Join(root, "nope"))
	if missing["system_checked"] != true || missing["system_major"] != nil {
		t.Fatalf("%+v", missing)
	}
}

func TestDiagLogFilesWhitelist(t *testing.T) {
	files, err := diagLogFiles("fe", "/home/dorisdb_base/fe/")
	if err != nil || len(files) != 2 || files[0] != "/home/dorisdb_base/fe/log/fe.out" {
		t.Fatalf("%v %v", files, err)
	}
	if _, err := diagLogFiles("fe", "/home/../etc"); err == nil {
		t.Fatal("path with .. should be rejected")
	}
	if _, err := diagLogFiles("fe", "relative/fe"); err == nil {
		t.Fatal("relative path should be rejected")
	}
	if files, _ := diagLogFiles("mysql", "/data"); len(files) != 0 {
		t.Fatalf("unknown role should read nothing: %v", files)
	}
}

func TestDiagUnitCommandsIncludesUnitFile(t *testing.T) {
	cmds := diagUnitCommands("fe-9030.service", 80)
	var joined []string
	for _, c := range cmds {
		joined = append(joined, strings.Join(c, " "))
	}
	all := strings.Join(joined, "\n")
	for _, want := range []string{"systemctl status fe-9030.service", "journalctl -u fe-9030.service -n 80", "systemctl cat fe-9030.service"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in:\n%s", want, all)
		}
	}
}

func TestTailFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fe.out")
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("line\n")
	}
	b.WriteString("last-1\nlast-2\n")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := tailFile(p, 2)
	if got != "last-1\nlast-2" {
		t.Fatalf("got %q", got)
	}
}

func TestRestoreSubdirsInPlace(t *testing.T) {
	target := t.TempDir()
	for _, d := range []string{"bin", "lib"} {
		mustWrite(t, filepath.Join(target, d, "v"), "new")
		mustWrite(t, filepath.Join(target, "bak.s1", d, "v"), "old")
	}
	msg, errMsg := restoreSubdirsInPlace(target, "s1", []string{"bin", "lib"})
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if !strings.Contains(msg, "restored bin") {
		t.Fatalf("msg %q", msg)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "bin", "v")); string(b) != "old" {
		t.Fatalf("bin not restored: %q", b)
	}
	matches, _ := filepath.Glob(filepath.Join(target, "bak.s1.failed-*", "lib", "v"))
	if len(matches) != 1 {
		t.Fatalf("failed copy missing: %v", matches)
	}
	if _, errMsg := restoreSubdirsInPlace(target, "s1", []string{"bin"}); errMsg == "" {
		t.Fatal("second restore should fail: backup consumed")
	}
	if _, errMsg := restoreSubdirsInPlace(target, "../x", nil); errMsg == "" {
		t.Fatal("bad suffix should fail")
	}
}

func TestReplaceSubdirsKeepsFirstBackup(t *testing.T) {
	target := t.TempDir()
	extract := t.TempDir()
	mustWrite(t, filepath.Join(target, "bin", "v"), "v1")
	mustWrite(t, filepath.Join(extract, "bin", "v"), "v2")
	if _, errMsg := replaceSubdirsInPlace(target, extract, []string{"bin"}, "s1"); errMsg != "" {
		t.Fatal(errMsg)
	}
	extract2 := t.TempDir()
	mustWrite(t, filepath.Join(extract2, "bin", "v"), "v2")
	if _, errMsg := replaceSubdirsInPlace(target, extract2, []string{"bin"}, "s1"); errMsg != "" {
		t.Fatal(errMsg)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "bak.s1", "bin", "v")); string(b) != "v1" {
		t.Fatalf("first backup overwritten: %q", b)
	}
	if m, _ := filepath.Glob(filepath.Join(target, "bak.s1.retry-*", "bin", "v")); len(m) != 1 {
		t.Fatalf("retry copy missing: %v", m)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
