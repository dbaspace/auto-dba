package goinception

import "testing"

func TestParseAddr(t *testing.T) {
	cfg, err := ParseAddr("10.0.0.8:4000")
	if err != nil || cfg.Host != "10.0.0.8" || cfg.Port != 4000 {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	if _, err := ParseAddr(""); err == nil {
		t.Fatal("empty should fail")
	}
}

func TestWrapAuditSQL(t *testing.T) {
	cmt := BuildMagicComment("u", "p", "h", 3306, "db1")
	if !contains(cmt, "--check=1") || !contains(cmt, "--db=db1;") {
		t.Fatalf("comment=%s", cmt)
	}
	got := WrapAuditSQL("SELECT 1", cmt)
	if !contains(got, "inception_magic_start") || !contains(got, "SELECT 1") {
		t.Fatalf("wrapped=%s", got)
	}
}

func TestFormatSetSQL(t *testing.T) {
	got, err := FormatSetSQL("check_dml_where", "true")
	if err != nil || got != "inception set check_dml_where = true;" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = FormatSetSQL("lang", "zh-CN")
	if err != nil || got != "inception set lang = 'zh-CN';" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := FormatSetSQL("a;drop", "1"); err == nil {
		t.Fatal("invalid name should fail")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
