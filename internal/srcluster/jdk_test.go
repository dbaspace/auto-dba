package srcluster

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestDefaultJDKRange(t *testing.T) {
	cases := []struct {
		ver      string
		min, max int
	}{
		{"2.5.21", 8, 11},
		{"3.0.9", 11, 17},
		{"3.4.2", 11, 17},
		{"3.5.0", 17, 0},
		{"4.0.1", 17, 0},
		{"v3.3.8-rc", 11, 17},
		{"unknown", 11, 0},
	}
	for _, c := range cases {
		if lo, hi := DefaultJDKRange(c.ver); lo != c.min || hi != c.max {
			t.Fatalf("%s => %d,%d want %d,%d", c.ver, lo, hi, c.min, c.max)
		}
	}
}

func TestNormalizeReleaseJDK(t *testing.T) {
	if lo, hi, err := NormalizeReleaseJDK("3.5.2", "", ""); err != nil || lo != "17" || hi != "" {
		t.Fatalf("默认 3.5: %s %s %v", lo, hi, err)
	}
	if lo, hi, err := NormalizeReleaseJDK("3.4.2", "", ""); err != nil || lo != "11" || hi != "17" {
		t.Fatalf("默认 3.4: %s %s %v", lo, hi, err)
	}
	if lo, hi, err := NormalizeReleaseJDK("3.4.2", "11", ""); err != nil || lo != "11" || hi != "" {
		t.Fatalf("只填最低时不限上限: %s %s %v", lo, hi, err)
	}
	if _, _, err := NormalizeReleaseJDK("3.4.2", "17", "11"); err == nil {
		t.Fatal("最低大于最高应报错")
	}
	if _, _, err := NormalizeReleaseJDK("3.4.2", "abc", ""); err == nil {
		t.Fatal("非整数应报错")
	}
	if lo, hi := releaseJDKRange(&store.SrRelease{Version: "3.4.2", JDKMin: "11", JDKMax: "17"}); lo != 11 || hi != 17 {
		t.Fatalf("releaseJDKRange %d %d", lo, hi)
	}
}

func TestPickOSSJDK(t *testing.T) {
	objs := []OSSObject{
		{Key: "starrocks/jdk-11.tar.gz"},
		{Key: "starrocks/jdk-21.tar.gz"},
		{Key: "starrocks/jdk-21.tar.gz.sha256"},
		{Key: "starrocks/3.5.2/fe/jdk-25.tar.gz"},
		{Key: "starrocks/StarRocks-3.5.2.fe.tar.gz"},
	}
	if p := pickOSSJDK(objs, "starrocks/", 17, 0); p == nil || p.Major != 21 || p.SHA256Key != "starrocks/jdk-21.tar.gz.sha256" {
		t.Fatalf("3.5+ 应选 jdk-21: %+v", p)
	}
	if p := pickOSSJDK(objs, "starrocks/", 11, 17); p == nil || p.Major != 11 || p.SHA256Key != "" {
		t.Fatalf("上限 17 时应选 jdk-11: %+v", p)
	}
	if p := pickOSSJDK(objs, "starrocks/", 8, 8); p != nil {
		t.Fatalf("无匹配应为 nil: %+v", p)
	}
	if h := ossJDKHint("starrocks/", 17, 0); h != "starrocks/jdk-21.tar.gz" {
		t.Fatalf("hint %s", h)
	}
}

func TestAddJDKResults(t *testing.T) {
	res := &UpgradePrecheckResult{OK: true}
	be := store.SrNode{ID: "n1", Component: "be", Host: "h1"}
	fe := store.SrNode{ID: "n2", Component: "fe", Host: "h2"}
	res.addJDKResults([]jdkNodeResult{
		{Node: be, Decision: jdkDecision{Status: jdkInstall, Message: "将安装 jdk-21"}},
		{Node: fe, Decision: jdkDecision{Status: jdkOK, Message: "满足"}},
	})
	if !res.OK || res.Items[0].Level != "warn" || res.Items[0].Name != "jdk_n1" {
		t.Fatalf("warn 不应阻断: %+v", res)
	}
	res.addJDKResults([]jdkNodeResult{{Node: fe, Err: errString(OldAgentJavaMsg)}})
	if res.OK || !strings.Contains(res.Items[2].Message, "版本过旧") {
		t.Fatalf("旧 Agent 应阻断: %+v", res.Items[2])
	}
	res2 := &UpgradePrecheckResult{OK: true}
	res2.addJDKResults([]jdkNodeResult{{Node: be, Decision: jdkDecision{Status: jdkFail, Message: "无包"}}})
	if res2.OK {
		t.Fatal("fail 应阻断")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestDecideJDK(t *testing.T) {
	pkg := &ossJDK{Key: "starrocks/jdk-21.tar.gz", Major: 21}
	ok := decideJDK("be@h1", "3.5.2", &JavaCheckResp{Major: 21, Version: "21.0.4", Path: "/usr/bin/java", Source: "path", SystemChecked: true, SystemMajor: 21}, 17, 0, nil, "starrocks/")
	if ok.Status != jdkOK || !strings.Contains(ok.Message, "满足 3.5.2 要求 JDK 17+") {
		t.Fatalf("ok: %+v", ok)
	}
	old := &JavaCheckResp{Major: 11, Version: "11.0.24", Path: "/usr/bin/java", Source: "path", SystemChecked: true, SystemMajor: 11}
	if d := decideJDK("be@h1", "3.5.2", old, 17, 0, pkg, "starrocks/"); d.Status != jdkInstall || d.Pkg != pkg || !strings.Contains(d.Message, "jdk-21.tar.gz") {
		t.Fatalf("install oss: %+v", d)
	}
	withCand := *old
	withCand.Candidates = []JavaCandidate{{Home: "/home/jdk-21.0.4", Major: 21, Version: "21.0.4"}}
	if d := decideJDK("be@h1", "3.5.2", &withCand, 17, 0, pkg, "starrocks/"); d.Status != jdkInstall || d.JavaHome != "/home/jdk-21.0.4" || d.Pkg != nil {
		t.Fatalf("优先本机已有: %+v", d)
	}
	if d := decideJDK("be@h1", "3.5.2", old, 17, 0, nil, "starrocks/"); d.Status != jdkFail || !strings.Contains(d.Message, "starrocks/jdk-21.tar.gz") {
		t.Fatalf("无包 fail: %+v", d)
	}
	conf := &JavaCheckResp{Major: 8, Version: "1.8.0_392", Path: "/opt/jdk8/bin/java", Source: "conf", ConfFile: "/home/dorisdb_base/be/conf/be.conf", SystemChecked: true, SystemMajor: 21}
	if d := decideJDK("be@h1", "3.5.2", conf, 17, 0, pkg, "starrocks/"); d.Status != jdkFail || !strings.Contains(d.Message, "be.conf") {
		t.Fatalf("conf 锁定 fail: %+v", d)
	}
	if d := decideJDK("fe@h1", "3.5.2", &JavaCheckResp{Message: "未找到 java", SystemChecked: true}, 17, 0, pkg, "starrocks/"); d.Status != jdkInstall || !strings.Contains(d.Message, "未检测到可用 java") {
		t.Fatalf("无 java 时安装: %+v", d)
	}
}

func TestDecideJDKSystemJava(t *testing.T) {
	// 线上场景：fe.conf JAVA_HOME=/home/jdk（11），但 /usr/bin/java → Kona 8，systemd 启动推断到 JDK 8。
	chk := &JavaCheckResp{
		Major: 11, Version: "11.0.20", Path: "/home/jdk/bin/java", Source: "conf", ConfFile: "/opt/sr/fe/conf/fe.conf",
		SystemChecked: true, SystemMajor: 8, SystemVersion: "1.8.0_412", SystemPath: "/usr/lib/jvm/TencentKona-8.0.18-412/bin/java",
		Candidates: []JavaCandidate{
			{Home: "/usr/lib/jvm/java-17", Major: 17, Version: "17.0.9"},
			{Home: "/home/jdk", Major: 11, Version: "11.0.20", Preferred: true},
		},
	}
	d := decideJDK("fe@h1", "3.3.22", chk, 11, 0, nil, "starrocks/")
	if d.Status != jdkInstall || d.JavaHome != "/home/jdk" {
		t.Fatalf("应切换到 /home/jdk: %+v", d)
	}
	if !strings.Contains(d.Message, "TencentKona") || !strings.Contains(d.Message, "推断 JAVA_HOME") {
		t.Fatalf("原因应说明系统 java: %s", d.Message)
	}
	if jdkReady(chk, 11, 0) {
		t.Fatal("系统 java 8 时不应 ready")
	}

	fixed := *chk
	fixed.SystemMajor, fixed.SystemVersion, fixed.SystemPath = 11, "11.0.20", "/home/jdk/bin/java"
	if d := decideJDK("fe@h1", "3.3.22", &fixed, 11, 0, nil, "starrocks/"); d.Status != jdkOK || !strings.Contains(d.Message, "也满足") {
		t.Fatalf("两者都满足应 ok: %+v", d)
	}

	// 线上第二次失败：旧 Agent 不报系统 java，conf 满足就放行，FE 实际仍用 Kona 8 启动。
	oldAgent := &JavaCheckResp{Major: 11, Version: "11.0.20", Path: "/home/jdk/bin/java", Source: "conf"}
	if d := decideJDK("fe@h1", "3.3.22", oldAgent, 11, 0, nil, "starrocks/"); d.Status != jdkFail || !strings.Contains(d.Message, "Agent 版本过旧") || !strings.Contains(d.Message, "未停服") {
		t.Fatalf("旧 Agent 应阻断: %+v", d)
	}
	if jdkReady(oldAgent, 11, 0) {
		t.Fatal("旧 Agent 无法确认系统 java，不应 ready")
	}

	noCand := *chk
	noCand.Candidates = nil
	if d := decideJDK("fe@h1", "3.3.22", &noCand, 11, 0, nil, "starrocks/"); d.Status != jdkFail {
		t.Fatalf("无候选无包应 fail: %+v", d)
	}
}
