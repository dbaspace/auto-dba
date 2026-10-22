package ticketenv

import (
	"testing"

	"auto-dba/internal/config"
)

func TestResolveDefault(t *testing.T) {
	got := Resolve(nil, []string{"beijing", "test", "ai-dev", "UAT", "beijing"})
	if got.Mode != ModeDefault {
		t.Fatalf("mode=%s", got.Mode)
	}
	if len(got.Groups) != 2 || got.Groups[0].Code != CodeProd || got.Groups[1].Code != CodeTest {
		t.Fatalf("groups=%+v", got.Groups)
	}
	if join(got.Groups[0].Codes) != "beijing" {
		t.Fatalf("prod=%v", got.Groups[0].Codes)
	}
	if join(got.Groups[1].Codes) != "test,ai-dev,UAT" {
		t.Fatalf("test=%v", got.Groups[1].Codes)
	}
}

func TestResolveConfiguredOther(t *testing.T) {
	got := Resolve([]config.TicketEnvGroup{
		{Code: "prod", Label: "生产", Codes: []string{"Beijing", "pobeice"}},
		{Code: "test", Label: "测试", Codes: []string{"test", "dev"}},
	}, []string{"beijing", "singapore", "test"})
	if got.Mode != ModeConfigured {
		t.Fatalf("mode=%s", got.Mode)
	}
	if join(got.Groups[0].Codes) != "Beijing,pobeice" {
		t.Fatalf("prod=%v", got.Groups[0].Codes)
	}
	if got.Groups[2].Code != CodeOther || join(got.Groups[2].Codes) != "singapore" {
		t.Fatalf("other=%+v", got.Groups[2])
	}
}

func TestResolveFirstGroupWins(t *testing.T) {
	got := Resolve([]config.TicketEnvGroup{
		{Code: "prod", Codes: []string{"beijing"}},
		{Code: "test", Codes: []string{"beijing", "test"}},
	}, nil)
	if join(got.Groups[1].Codes) != "test" {
		t.Fatalf("test=%v", got.Groups[1].Codes)
	}
}

func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
