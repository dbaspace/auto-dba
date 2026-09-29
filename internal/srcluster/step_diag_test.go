package srcluster

import (
	"strings"
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestAppendStepLine(t *testing.T) {
	at := time.Date(2026, 9, 29, 15, 7, 47, 0, time.Local)
	if got := appendStepLine("", at, "停服 fe-9030"); got != "15:07:47 停服 fe-9030" {
		t.Fatalf("empty: %q", got)
	}
	if got := appendStepLine("a", at, "b"); got != "a\n15:07:47 b" {
		t.Fatalf("append: %q", got)
	}
	long := strings.Repeat("x", maxStepLogLen+100)
	got := appendStepLine(long, at, "tail")
	if !strings.HasPrefix(got, "…（前文已截断）") || !strings.HasSuffix(got, "tail") {
		t.Fatalf("truncate head/tail wrong")
	}
}

func TestBinReplacedBefore(t *testing.T) {
	cases := []struct {
		log  string
		want bool
	}{
		{"", false},
		{"15:00:00 停服 fe-9030\n15:00:01 失败: 停服失败", false},
		{"15:00:02 [replaced] ok\n15:00:03 失败: 升级后启服失败，已停止后续节点", true},
		{"升级后启服失败，已停止后续节点: exit status 1", true}, // 旧版本日志
		{"15:00:02 [replaced] ok\n16:00:00 [rolled_back] 已回滚", false},
		{"[replaced] ok\n[rolled_back] x\n---- 重试 ----\n[replaced] ok", true},
	}
	for i, c := range cases {
		if got := binReplacedBefore(c.log); got != c.want {
			t.Fatalf("case %d: got %v want %v, log=%q", i, got, c.want, c.log)
		}
	}
}

func TestNodeStillUpgraded(t *testing.T) {
	if !nodeStillUpgraded(&store.SrChangeStep{Status: store.SrStepSuccess, LogText: "ok"}) {
		t.Fatal("旧版成功步骤（无标记）应视为已升级")
	}
	if nodeStillUpgraded(&store.SrChangeStep{Status: store.SrStepSuccess, LogText: "ok\n[rolled_back] x"}) {
		t.Fatal("成功后已回滚")
	}
	if nodeStillUpgraded(&store.SrChangeStep{Status: store.SrStepFailed, LogText: "停服失败"}) {
		t.Fatal("未替换的失败步骤无需回滚")
	}
	if !nodeStillUpgraded(&store.SrChangeStep{Status: store.SrStepFailed, LogText: "[replaced] ok\n失败: 升级后启服失败"}) {
		t.Fatal("替换后启动失败应可回滚")
	}
}

func TestUnitMissingDaemon(t *testing.T) {
	cases := []struct {
		name, unit string
		want       bool
	}{
		{"平台模板", "[Service]\nType=forking\nExecStart=/home/dorisdb_base/fe/bin/start_fe.sh --daemon\n", false},
		{"forking 缺 daemon", "# /etc/systemd/system/fe-9030.service\n[Service]\nType=forking\nExecStart=/home/dorisdb_base/fe/bin/start_fe.sh\nRestart=on-failure\n", true},
		{"BE 缺 daemon", "[Service]\nType = forking\nExecStart=-/opt/be/bin/start_be.sh --helper x:9010\n", true},
		{"simple 前台正常", "[Service]\nExecStart=/home/dorisdb_base/fe/bin/start_fe.sh\nRestart=always\n", false},
		{"drop-in 覆盖 ExecStart", "[Service]\nType=forking\nExecStart=/a/bin/start_fe.sh\n# /etc/systemd/system/fe-9030.service.d/x.conf\n[Service]\nExecStart=\nExecStart=/a/bin/start_fe.sh --daemon\n", false},
		{"非 StarRocks 脚本", "[Service]\nType=forking\nExecStart=/usr/sbin/nginx\n", false},
		{"dry-run 输出", "dry-run systemctl cat fe-9030.service", false},
	}
	for _, c := range cases {
		if _, got := unitMissingDaemon(c.unit); got != c.want {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestTruncateDiagKeepsHead(t *testing.T) {
	s := "==== systemctl status ====\nActive: activating\n" + strings.Repeat("warn line\n", 3000) + "LAST"
	got := truncateDiag(s)
	if len(got) > maxDiagLen+64 {
		t.Fatalf("too long: %d", len(got))
	}
	if !strings.HasPrefix(got, "==== systemctl status ====") || !strings.HasSuffix(got, "LAST") || !strings.Contains(got, "中间已截断") {
		t.Fatalf("head/tail not kept: %q...%q", got[:40], got[len(got)-20:])
	}
	if truncateDiag(" short ") != "short" {
		t.Fatal("short unchanged")
	}
}

func TestVersionMatches(t *testing.T) {
	cases := []struct {
		got, want string
		ok        bool
	}{
		{"3.3.22-3b8e4b8", "3.3.22", true},
		{"3.3.22", "3.3.22", true},
		{"3.2.10-e1d8f5b", "3.3.22", false},
		{"3.3.22-abc", "3.3.2", false},
		{"", "3.3.22", false},
	}
	for _, c := range cases {
		if got := versionMatches(c.got, c.want); got != c.ok {
			t.Fatalf("versionMatches(%q,%q)=%v want %v", c.got, c.want, got, c.ok)
		}
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("  systemctl start fe-9030: exit 1\n诊断信息：\n...  "); got != "systemctl start fe-9030: exit 1" {
		t.Fatalf("got %q", got)
	}
}
