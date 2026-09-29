package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseJavaVersion(t *testing.T) {
	cases := []struct {
		out   string
		major int
		ver   string
	}{
		{"openjdk version \"1.8.0_392\"\nOpenJDK Runtime Environment (build 1.8.0_392-b08)", 8, "1.8.0_392"},
		{"java version \"1.8.0_202\"", 8, "1.8.0_202"},
		{"openjdk version \"11.0.24\" 2024-07-16 LTS", 11, "11.0.24"},
		{"openjdk version \"21.0.4\" 2024-07-16", 21, "21.0.4"},
		{"openjdk version \"21\" 2023-09-19", 21, "21"},
		{"openjdk version \"17-ea\" 2021-09-14", 17, "17-ea"},
		{"openjdk 21.0.4 2024-07-16", 21, "21.0.4"},
		{"bash: java: command not found", 0, ""},
	}
	for _, c := range cases {
		major, ver := parseJavaVersion(c.out)
		if major != c.major || ver != c.ver {
			t.Fatalf("%q => %d %q, want %d %q", c.out, major, ver, c.major, c.ver)
		}
	}
}

func TestJDKInRange(t *testing.T) {
	cases := []struct {
		major, min, max int
		want            bool
	}{
		{11, 11, 17, true},
		{8, 11, 17, false},
		{21, 11, 17, false},
		{21, 17, 0, true},
		{17, 17, 0, true},
		{0, 8, 0, false},
	}
	for _, c := range cases {
		if got := jdkInRange(c.major, c.min, c.max); got != c.want {
			t.Fatalf("jdkInRange(%d,%d,%d)=%v", c.major, c.min, c.max, got)
		}
	}
	if jdkRangeText(11, 17) != "11–17" || jdkRangeText(17, 0) != "17+" {
		t.Fatal("jdkRangeText")
	}
}

func TestReadConfJavaHome(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fe.conf")
	body := "# JAVA_HOME=/opt/old\nLOG_DIR = ${STARROCKS_HOME}/log\nJAVA_HOME = \"/usr/lib/jvm/java-11\"\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readConfJavaHome(p); got != "/usr/lib/jvm/java-11" {
		t.Fatalf("got %q", got)
	}
	if err := os.WriteFile(p, []byte("export JAVA_HOME=/home/jdk-21.0.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readConfJavaHome(p); got != "/home/jdk-21.0.4" {
		t.Fatalf("got %q", got)
	}
	if got := readConfJavaHome(filepath.Join(dir, "none.conf")); got != "" {
		t.Fatalf("got %q", got)
	}
	soft := filepath.Join(dir, "be")
	if err := os.MkdirAll(filepath.Join(soft, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, source, _ := effectiveJava("be", soft); source != "path" {
		t.Fatalf("无 conf 时应走 PATH: %s", source)
	}
	if err := os.WriteFile(filepath.Join(soft, "conf", "be.conf"), []byte("JAVA_HOME=/opt/jdk8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin, source, conf := effectiveJava("be", soft)
	if source != "conf" || bin != "/opt/jdk8/bin/java" || conf != filepath.Join(soft, "conf", "be.conf") {
		t.Fatalf("bin=%s source=%s conf=%s", bin, source, conf)
	}
}
