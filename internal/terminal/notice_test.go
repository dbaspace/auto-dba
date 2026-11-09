package terminal

import (
	"encoding/json"
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestTerminalNoticeIncludesProfileKey(t *testing.T) {
	raw, err := json.Marshal(execContent{Args: []string{"10.32.8.28", "sysadmin", "config/terminal/.runtime_prod"}})
	if err != nil {
		t.Fatal(err)
	}
	sess := &store.TerminalSession{Host: "10.32.8.28", ExecContent: string(raw)}
	msg := terminalNotice(sess, store.TermStatusFailed)
	for _, want := range []string{"10.32.8.28", "sysadmin", "config/terminal/.runtime_prod", "不是平台登录过期"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("通知缺少 %q: %s", want, msg)
		}
	}
	exp := terminalNotice(sess, store.TermStatusExpired)
	if !strings.Contains(exp, "最长存活时间") || strings.Contains(exp, "authorized_keys") {
		t.Fatalf("过期通知不应混成公钥失败: %s", exp)
	}
}
