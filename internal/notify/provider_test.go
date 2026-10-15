package notify

import (
	"testing"

	"auto-dba/internal/config"
)

func TestResolveLarkUserID(t *testing.T) {
	if got := ResolveLarkUserID("lark", "u123"); got != "u123" {
		t.Fatalf("got %q", got)
	}
	if got := ResolveLarkUserID("", "u123"); got != "u123" {
		t.Fatalf("empty type: %q", got)
	}
	if got := ResolveLarkUserID("wecom", "u123"); got != "" {
		t.Fatalf("wecom should skip: %q", got)
	}
}

func TestInitProvidersDisabled(t *testing.T) {
	InitProviders(config.NotifyModuleConfig{})
	st := ProviderStatus()
	if st["lark_ready"] == true || st["wecom_ready"] == true {
		t.Fatalf("expected no providers: %+v", st)
	}
}

func TestNormalizeRobotKind(t *testing.T) {
	if k := normalizeRobotKind("", "https://open.feishu.cn/open-apis/bot/v2/hook/x"); k != "lark" {
		t.Fatalf("feishu url: %s", k)
	}
	if k := normalizeRobotKind("dingtalk", "https://example.com"); k != "dingtalk" {
		t.Fatalf("kind: %s", k)
	}
}
