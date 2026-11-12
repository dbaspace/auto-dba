package store

import "testing"

func TestMergeEnvTagValuesKeepsCustomAndAppendsMissing(t *testing.T) {
	got := MergeEnvTagValues("idc", []string{"beijing", "a-beijing", " elbconsulting "}, []string{"beijing", "singapore", "test"})
	want := []string{"beijing", "a-beijing", "elbconsulting", "singapore", "test"}
	if len(got) != len(want) {
		t.Fatalf("len=%d got=%v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v", got)
		}
	}
	app := MergeEnvTagValues("app", []string{"redis"}, []string{"singapore"})
	if len(app) != 1 || app[0] != "redis" {
		t.Fatalf("app=%v", app)
	}
	env := MergeEnvTagValues("env", nil, []string{"", "singapore", "singapore"})
	if len(env) != 1 || env[0] != "singapore" {
		t.Fatalf("env=%v", env)
	}
}
