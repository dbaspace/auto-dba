package rediscmd

import (
	"strings"
	"testing"
)

func TestSplitArgs_QuotesAndJSON(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`SET k v`, []string{"SET", "k", "v"}},
		{`SET k "hello world"`, []string{"SET", "k", "hello world"}},
		{`SET k 'hello world'`, []string{"SET", "k", "hello world"}},
		{`SET user:1 "{\"name\":\"alice\",\"age\":1}"`, []string{"SET", "user:1", `{"name":"alice","age":1}`}},
		{`SET user:1 '{"name":"alice","age":1}'`, []string{"SET", "user:1", `{"name":"alice","age":1}`}},
		{`SET k "{\"a\": \"b c\"}"`, []string{"SET", "k", `{"a": "b c"}`}},
		{`HSET h f "a\nb"`, []string{"HSET", "h", "f", "a\nb"}},
		{`SET path "c:\\users\\a"`, []string{"SET", "path", `c:\users\a`}},
		{`SET path 'c:\users\a'`, []string{"SET", "path", `c:\users\a`}},
		{`SET k "say \"hi\""`, []string{"SET", "k", `say "hi"`}},
		{`SET k 'it\'s ok'`, []string{"SET", "k", `it's ok`}},
		{`SET k "\x41\x42"`, []string{"SET", "k", "AB"}},
		{`SET k ""`, []string{"SET", "k", ""}},
		{`SET k {plain:1}`, []string{"SET", "k", `{plain:1}`}}, // 无引号、无空格可整段作为参数
		{`LPUSH q "item with space"`, []string{"LPUSH", "q", "item with space"}},
	}
	for _, tc := range cases {
		got, err := SplitArgs(tc.in)
		if err != nil {
			t.Fatalf("SplitArgs(%q): %v", tc.in, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("SplitArgs(%q) len=%d want %d got=%v", tc.in, len(got), len(tc.want), got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("SplitArgs(%q)[%d]=%q want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestSplitArgs_Errors(t *testing.T) {
	for _, in := range []string{
		`SET k "unclosed`,
		`SET k 'unclosed`,
		`SET k "foo"bar`,
		`SET k 'foo'bar`,
	} {
		if _, err := SplitArgs(in); err == nil {
			t.Fatalf("want error for %q", in)
		}
	}
}

func TestValidateCommand_SpecialValues(t *testing.T) {
	ok := []string{
		`SET k "hello world"`,
		`SET k "{\"x\":1,\"y\":\"a b\"}"`,
		`SET k '{"x":1,"y":"a b"}'`,
		`SET path "c:\\data\\file.json"`,
		`HSET user:1 profile "{\"nick\":\"张三\"}"`,
		`LPUSH jobs '{"type":"sync","id":1}'`,
		"SET k value\\with\\backslash", // 无引号时反斜杠为字面量
	}
	for _, line := range ok {
		if err := ValidateCommand(line); err != nil {
			t.Errorf("want OK %q: %v", line, err)
		}
	}
	bad := []string{
		`SET k "hello world`,    // 未闭合
		`SET k "{\"a\": \"b\"}`, // 双引号 JSON 未正确闭合/转义
		`SET k {"a":1}`,         // 裸 JSON 内含 "，须整体加引号
		`SET "only-one-arg"`,    // 少参
	}
	for _, line := range bad {
		if err := ValidateCommand(line); err == nil {
			t.Errorf("want FAIL %q", line)
		}
	}
}

func TestValidateScript_JSONMultiLine(t *testing.T) {
	script := strings.Join([]string{
		`# json / escape samples`,
		`SET cfg "{\"enabled\":true,\"path\":\"c:\\\\tmp\"}"`,
		`HSET u meta '{"role":"admin","note":"a b"}'`,
		`SADD tags "tag with space"`,
	}, "\n")
	if err := ValidateScript(script); err != nil {
		t.Fatal(err)
	}
}
