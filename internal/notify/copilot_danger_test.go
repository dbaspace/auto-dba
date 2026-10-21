package notify

import (
	"strings"
	"testing"

	"auto-dba/internal/config"
)

func TestFormatCopilotDangerMessage(t *testing.T) {
	title, text, _ := formatCopilotDangerMessage("admin", "管理员", "2026-10-21 14:37:00", "delete from aa;", "命中危险操作: delete")
	if title != "AI助手危险操作" {
		t.Fatal(title)
	}
	for _, want := range []string{"操作人：管理员（admin）", "操作时间：2026-10-21 14:37:00", "请求内容：delete from aa;"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
}

func TestCopilotRobotChannels(t *testing.T) {
	robots := []config.NotifyRobotConfig{{Kind: "lark", URL: "https://open.feishu.cn/open-apis/bot/v2/hook/x"}}
	if ch := copilotRobotChannels(robots); len(ch) != 1 || ch[0] != "robot_lark" {
		t.Fatalf("robot: %v", ch)
	}
	if ch := copilotRobotChannels(nil); len(ch) != 0 {
		t.Fatalf("no robot: %v", ch)
	}
}
