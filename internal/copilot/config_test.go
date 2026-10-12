package copilot

import (
	"testing"

	"auto-dba/internal/config"
)

func TestParseModelsFormats(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
		lab0 string
	}{
		{"gpt-4o-mini", []string{"gpt-4o-mini"}, "gpt-4o-mini"},
		{"gpt-4o-mini, qwen-plus;deepseek-chat", []string{"gpt-4o-mini", "qwen-plus", "deepseek-chat"}, "gpt-4o-mini"},
		{`["a","b"]`, []string{"a", "b"}, "a"},
		{`[{"value":"gpt-4o-mini","label":"轻量"},{"id":"qwen-plus","name":"通义"}]`, []string{"gpt-4o-mini", "qwen-plus"}, "轻量"},
		{"fast|轻量, strong|增强", []string{"fast", "strong"}, "轻量"},
	}
	for _, tc := range cases {
		got := ParseModels(tc.raw)
		if len(got) != len(tc.want) {
			t.Fatalf("raw=%q len=%d want %d", tc.raw, len(got), len(tc.want))
		}
		for i := range tc.want {
			if got[i].Value != tc.want[i] {
				t.Fatalf("raw=%q [%d]=%s want %s", tc.raw, i, got[i].Value, tc.want[i])
			}
		}
		if got[0].Label != tc.lab0 {
			t.Fatalf("raw=%q label=%s want %s", tc.raw, got[0].Label, tc.lab0)
		}
	}
}

func TestResolveModel(t *testing.T) {
	c := Config{Models: ParseModels(`[{"value":"a","label":"A"},{"value":"b","label":"B"}]`)}
	if m, err := c.ResolveModel(""); err != nil || m != "a" {
		t.Fatalf("default got %s %v", m, err)
	}
	if m, err := c.ResolveModel("b"); err != nil || m != "b" {
		t.Fatalf("pick b got %s %v", m, err)
	}
	if _, err := c.ResolveModel("c"); err == nil {
		t.Fatal("expected unknown model error")
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://api.cursor.com/":                    "https://api.cursor.com",
		"https://api.cursor.com/v1/agents":           "https://api.cursor.com",
		"https://api.openai.com/v1/chat/completions": "https://api.openai.com/v1",
		"https://api.openai.com/v1":                  "https://api.openai.com/v1",
	}
	for in, want := range cases {
		if got := NormalizeBaseURL(in); got != want {
			t.Fatalf("%s => %s want %s", in, got, want)
		}
	}
}

func TestResolveProvider(t *testing.T) {
	if ResolveProvider("", "https://api.cursor.com") != ProviderCursor {
		t.Fatal("expected cursor")
	}
	if ResolveProvider("openai", "https://api.cursor.com") != ProviderOpenAI {
		t.Fatal("explicit openai")
	}
	if ResolveProvider("", "https://api.openai.com/v1") != ProviderOpenAI {
		t.Fatal("expected openai")
	}
}

func TestSanitizeYAMLScalar(t *testing.T) {
	cases := map[string]string{
		`  "crsr_abc"  `: "crsr_abc",
		`'sk-xyz'`:       "sk-xyz",
		`crsr_plain`:     "crsr_plain",
		`""`:             "",
	}
	for in, want := range cases {
		if got := sanitizeYAMLScalar(in); got != want {
			t.Fatalf("%q => %q want %q", in, got, want)
		}
	}
}

func TestLoadFromLiveOnlyYAML(t *testing.T) {
	prev := config.Live()
	t.Cleanup(func() { config.BindLive(prev) })

	config.BindLive(nil)
	off := LoadFromLive()
	if off.Enabled() {
		t.Fatal("nil live should be offline")
	}

	config.BindLive(&config.Config{
		Copilot: config.CopilotConfig{
			BaseURL: `"https://api.cursor.com/"`,
			APIKey:  `"crsr_test"`,
			Model:   `[{"value":"auto","label":"Auto"}]`,
		},
	})
	on := LoadFromLive()
	if !on.Enabled() {
		t.Fatal("expected enabled from Live.Copilot")
	}
	if on.Provider != ProviderCursor {
		t.Fatalf("provider=%s", on.Provider)
	}
	if on.BaseURL != "https://api.cursor.com" {
		t.Fatalf("base=%s", on.BaseURL)
	}
	if on.APIKey != "crsr_test" {
		t.Fatalf("api_key not sanitized: %q", on.APIKey)
	}
	if on.DefaultModel() != "auto" {
		t.Fatalf("model=%s", on.DefaultModel())
	}
	// LoadConfig 忽略 db，不回退库表
	if got := LoadConfig(nil); !got.Enabled() || got.APIKey != "crsr_test" {
		t.Fatalf("LoadConfig should read Live only: %+v", got)
	}
}
