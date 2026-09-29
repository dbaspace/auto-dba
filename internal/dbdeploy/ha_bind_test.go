package dbdeploy

import (
	"strings"
	"testing"

	"auto-dba/internal/ha"
	"auto-dba/internal/store"
)

func TestValidateDeployHAOptions(t *testing.T) {
	cl := &store.DbDeployCluster{
		Engine: store.DbDeployEngineMySQL, DeployKind: store.DbDeployKindSelf,
		Topology: store.DbDeployTopoMasterReplica, HaEnabled: true, HaMode: "mha", ControllerID: "c1",
	}
	if err := ValidateDeployHAOptions(cl); err != nil {
		t.Fatal(err)
	}
	cl.Topology = store.DbDeployTopoStandalone
	if err := ValidateDeployHAOptions(cl); err == nil {
		t.Fatal("standalone should fail")
	}
	cl.Topology = store.DbDeployTopoMasterReplica
	cl.Engine = store.DbDeployEnginePostgres
	cl.HaMode = "pg_autofailover"
	cl.ControllerID = "c1"
	if err := ValidateDeployHAOptions(cl); err != nil {
		t.Fatal("postgres pg_autofailover should pass:", err)
	}
	cl.HaMode = "mha"
	if err := ValidateDeployHAOptions(cl); err == nil {
		t.Fatal("postgres+mha should fail")
	}
	cl.Engine = store.DbDeployEngineMySQL
	cl.HaMode = "mha"
	cl.DeployKind = store.DbDeployKindCloud
	cl.Topology = store.DbDeployTopoCloudEndpoint
	if err := ValidateDeployHAOptions(cl); err == nil {
		t.Fatal("cloud should fail")
	}
}

func TestValidateDeployHARoles(t *testing.T) {
	cl := &store.DbDeployCluster{
		Engine: store.DbDeployEngineMySQL, DeployKind: store.DbDeployKindSelf,
		Topology: store.DbDeployTopoMasterReplica, HaEnabled: true, HaMode: "manual",
	}
	nodes := []store.DbDeployNode{
		{Role: "primary", Host: "a"},
		{Role: "replica", Host: "b"},
	}
	if err := ValidateDeployHA(nil, cl, nodes); err != nil {
		t.Fatal(err)
	}
	nodes = []store.DbDeployNode{{Role: "primary", Host: "a"}}
	if err := ValidateDeployHA(nil, cl, nodes); err == nil {
		t.Fatal("need replica")
	}
}

func TestHAStepPrefix(t *testing.T) {
	if !haStepFailedAfterInstall("ha_precheck") {
		t.Fatal("expected ha_ prefix")
	}
	if haStepFailedAfterInstall("install_x") {
		t.Fatal("install should not count as HA")
	}
}

func TestParseHAOptionsAliasesAndScripts(t *testing.T) {
	cl := &store.DbDeployCluster{
		Name: "demo",
		HaOptionsJSON: `{
			"user":"mha",
			"mha_failover_script":"/usr/local/bin/master_ip_failover",
			"mha_online_change_script":"/usr/local/bin/master_ip_online_change",
			"master_ip_failover_script":""
		}`,
	}
	opts := ParseHAOptions(cl)
	if opts.MasterIPFailoverScript != "/usr/local/bin/master_ip_failover" {
		t.Fatalf("alias failover: got %q", opts.MasterIPFailoverScript)
	}
	if opts.MasterIPOnlineChangeScript != "/usr/local/bin/master_ip_online_change" {
		t.Fatalf("alias online: got %q", opts.MasterIPOnlineChangeScript)
	}
	// 标准字段名优先于别名
	cl.HaOptionsJSON = `{"master_ip_failover_script":"/a","mha_failover_script":"/b"}`
	opts = ParseHAOptions(cl)
	if opts.MasterIPFailoverScript != "/a" {
		t.Fatalf("canonical should win: got %q", opts.MasterIPFailoverScript)
	}
}

func TestBuildHAClusterRemarkHook(t *testing.T) {
	cl := &store.DbDeployCluster{
		HaOptionsJSON: `{"vip_hook_script":"/etc/auto-dba/hooks/vip.sh","vip_hook_args":["--vip","10.0.0.50"]}`,
	}
	remark := BuildHAClusterRemark(cl)
	if remark == "由集群部署自动接入" {
		t.Fatal("expected JSON remark with hook")
	}
	hook := ParseDeployHAHookOptions(cl)
	if hook.Script != "/etc/auto-dba/hooks/vip.sh" {
		t.Fatalf("script: %q", hook.Script)
	}
	if len(hook.Args) != 2 || hook.Args[0] != "--vip" {
		t.Fatalf("args: %#v", hook.Args)
	}
	// 无钩子时纯文本
	cl2 := &store.DbDeployCluster{HaOptionsJSON: `{"user":"mha"}`}
	if got := BuildHAClusterRemark(cl2); got != "由集群部署自动接入" {
		t.Fatalf("plain remark: %q", got)
	}
}

func TestCollectHAVIPWarnings(t *testing.T) {
	cl := &store.DbDeployCluster{HaEnabled: true, HaMode: "mha"}
	w := CollectHAVIPWarnings(cl)
	if len(w) != 1 || !strings.Contains(w[0], "未配置业务 VIP") {
		t.Fatalf("empty vip+script: %#v", w)
	}
	cl.HaVIP = "10.0.0.50"
	w = CollectHAVIPWarnings(cl)
	if len(w) != 1 || !strings.Contains(w[0], "未配置 failover") {
		t.Fatalf("vip only: %#v", w)
	}
	cl.HaOptionsJSON = `{"master_ip_failover_script":"/bin/fail"}`
	w = CollectHAVIPWarnings(cl)
	if len(w) != 0 {
		t.Fatalf("vip+script should be clean: %#v", w)
	}
	cl.HaMode = "manual"
	cl.HaVIP = ""
	cl.HaOptionsJSON = ""
	if w = CollectHAVIPWarnings(cl); len(w) != 0 {
		t.Fatalf("non-mha should not warn: %#v", w)
	}
}

func TestFunctionalVIPMHASelfCheck(t *testing.T) {
	// 模拟部署向导提交的 ha_options_json（前端字段名 → 标准 MHA / 钩子）
	optsJSON := `{
		"user":"mha",
		"password":"secret",
		"ssh_user":"root",
		"master_ip_failover_script":"/usr/local/bin/master_ip_failover",
		"master_ip_online_change_script":"/usr/local/bin/master_ip_online_change",
		"vip_hook_script":"/etc/auto-dba/hooks/vip.sh",
		"vip_hook_args":["--vip","10.0.0.100"]
	}`
	cl := &store.DbDeployCluster{
		Name:          "vip-mha-selftest",
		Engine:        store.DbDeployEngineMySQL,
		DeployKind:    store.DbDeployKindSelf,
		Topology:      store.DbDeployTopoMasterReplica,
		HaEnabled:     true,
		HaMode:        "mha",
		ControllerID:  "ctrl-mha",
		HaVIP:         "10.0.0.100",
		HaOptionsJSON: optsJSON,
	}
	if err := ValidateDeployHAOptions(cl); err != nil {
		t.Fatal("ValidateDeployHAOptions:", err)
	}
	if w := CollectHAVIPWarnings(cl); len(w) != 0 {
		t.Fatalf("expected no vip warnings, got %#v", w)
	}
	opts := ParseHAOptions(cl)
	if opts.MasterIPFailoverScript != "/usr/local/bin/master_ip_failover" {
		t.Fatal("failover script not mapped")
	}
	remark := BuildHAClusterRemark(cl)
	hook := ha.ParseHAHookOptions(remark)
	if hook.Script != "/etc/auto-dba/hooks/vip.sh" {
		t.Fatalf("remark hook script: %q remark=%s", hook.Script, remark)
	}
	if len(hook.Args) != 2 {
		t.Fatalf("hook args: %#v", hook.Args)
	}
	cluster := &store.HACluster{
		ID: "hc-selftest", Name: cl.Name, VIP: cl.HaVIP, Remark: remark,
		Members: []store.HAMember{
			{Role: "primary", Host: "10.0.0.1", Port: 3306, Enabled: true},
			{Role: "replica", Host: "10.0.0.2", Port: 3306, Enabled: true},
		},
	}
	conf, err := ha.RenderMHAAppConf(cluster, opts)
	if err != nil {
		t.Fatal(err)
	}
	needles := []string{
		"master_ip_failover_script=/usr/local/bin/master_ip_failover",
		"master_ip_online_change_script=/usr/local/bin/master_ip_online_change",
		"user=mha",
		"hostname=10.0.0.1",
		"hostname=10.0.0.2",
	}
	for _, n := range needles {
		if !strings.Contains(conf, n) {
			t.Fatalf("app.cnf missing %q\n%s", n, conf)
		}
	}
	audit := FormatHAVIPAuditLine(cl)
	if !strings.Contains(audit, "10.0.0.100") || !strings.Contains(audit, "master_ip_failover") {
		t.Fatalf("audit line: %s", audit)
	}
	// 仅 VIP 无脚本 → warning
	clWarn := *cl
	clWarn.HaOptionsJSON = `{"user":"mha"}`
	w := CollectHAVIPWarnings(&clWarn)
	if len(w) != 1 || !strings.Contains(w[0], "未配置 failover") {
		t.Fatalf("vip-only warning: %#v", w)
	}
}
