package store

import (
	"bytes"
	"strings"
	"testing"
)

func TestDetectTodoImageMime(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if m, err := DetectTodoImageMime(png); err != nil || m != "image/png" {
		t.Fatalf("png: %s %v", m, err)
	}
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F'}
	if m, err := DetectTodoImageMime(jpg); err != nil || m != "image/jpeg" {
		t.Fatalf("jpeg: %s %v", m, err)
	}
	for name, data := range map[string][]byte{
		"空":   nil,
		"svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"文本":  []byte("hello"),
		"超限":  append(append([]byte{}, png...), bytes.Repeat([]byte{0}, TodoImageMaxBytes)...),
	} {
		if _, err := DetectTodoImageMime(data); err == nil {
			t.Fatalf("%s 应被拒绝", name)
		}
	}
}

func TestSanitizeImageFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":                "passwd",
		`C:\Users\a\截图 1.png`:             "截图 1.png",
		"  ":                              "image",
		"":                                "image",
		strings.Repeat("长", 120) + ".png": strings.Repeat("长", 96) + ".png",
	}
	for in, want := range cases {
		if got := SanitizeImageFilename(in); got != want {
			t.Fatalf("%q => %q want %q", in, got, want)
		}
	}
}

func TestNormalizeTodoImageIDs(t *testing.T) {
	got, err := NormalizeTodoImageIDs([]string{" a ", "a", "", "b"})
	if err != nil || len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("%v %v", got, err)
	}
	many := make([]string, TodoImageMaxPerTarget+1)
	for i := range many {
		many[i] = string(rune('a' + i))
	}
	if _, err := NormalizeTodoImageIDs(many); err == nil {
		t.Fatal("超过单处上限应报错")
	}
}
