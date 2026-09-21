package handler

import (
	"testing"
)

func TestJSONAnyString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"  abc  ", "abc"},
		{float64(42), "42"},
		{int64(7), "7"},
		{map[string]any{"id": "inst-1"}, "inst-1"},
		{map[string]any{"instance_id": "inst-2"}, "inst-2"},
		{[]any{"x"}, "x"},
	}
	for _, tc := range cases {
		if got := jsonAnyString(tc.in); got != tc.want {
			t.Fatalf("jsonAnyString(%v)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestCDCInstanceIDsFromPayload(t *testing.T) {
	src, dst := cdcInstanceIDsFromPayload(map[string]any{
		"source_instance_id": 12.0,
		"target_instance_id": map[string]any{"id": "sr-1"},
	})
	if src != "12" || dst != "sr-1" {
		t.Fatalf("got %q %q", src, dst)
	}

	src, dst = cdcInstanceIDsFromPayload(map[string]any{
		"description": "demo\n-- auto-dba-cdc:{\"source_instance_id\":\"pg-1\",\"target_instance_id\":\"sr-2\"}",
	})
	if src != "pg-1" || dst != "sr-2" {
		t.Fatalf("extra fallback got %q %q", src, dst)
	}
}

func TestCDCAutoCreateEnabled(t *testing.T) {
	if !cdcAutoCreateEnabled(nil) {
		t.Fatal("nil 默认视为开启自动建表")
	}
	if cdcAutoCreateEnabled(map[string]any{"auto_create_table": false}) {
		t.Fatal("显式关闭应返回 false")
	}
	if !cdcAutoCreateEnabled(map[string]any{"auto_create_table": true}) {
		t.Fatal("显式开启应返回 true")
	}
	if cdcAutoCreateEnabled(map[string]any{
		"description": "demo\n-- auto-dba-cdc:{\"auto_create_table\":false}",
	}) {
		t.Fatal("description extra 关闭自动建表应返回 false")
	}
	if !cdcAutoCreateEnabled(map[string]any{"description": "no extra"}) {
		t.Fatal("无 extra 默认开启")
	}
}

func TestCDCShouldSkipSQLPreviews(t *testing.T) {
	if cdcShouldSkipSQLPreviews(0, 99) {
		t.Fatal("99 tables should not skip")
	}
	if !cdcShouldSkipSQLPreviews(0, 100) {
		t.Fatal("100 tables should skip")
	}
	if !cdcShouldSkipSQLPreviews(100, 1) {
		t.Fatal("100 mappings should skip even if tables=1")
	}
	if cdcShouldSkipSQLPreviews(50, 200) {
		t.Fatal("mapping count takes precedence when non-zero; 50 should not skip")
	}
}
