package nodecheck

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestAgentMatchesHost(t *testing.T) {
	cases := []struct {
		name      string
		baseURL   string
		agentInst string
		hostInst  string
		addrs     []string
		want      bool
	}{
		{"host_instance_id 相同", "http://1.1.1.1:8091", "h1", "h1", []string{"10.100.112.17"}, true},
		{"base_url IP 相同", "http://10.100.112.17:8091", "", "h1", []string{"10.100.112.17"}, true},
		{"https 且无端口", "https://10.100.112.17", "", "h1", []string{" 10.100.112.17 "}, true},
		{"无 scheme", "10.100.112.17:8092", "", "h1", []string{"10.100.112.17"}, true},
		{"域名忽略大小写", "http://SR-BE01.example.com:8091", "", "h1", []string{"10.0.0.1", "sr-be01.example.com"}, true},
		{"IP 不一致", "http://10.100.112.18:8091", "h2", "h1", []string{"10.100.112.17"}, false},
		{"base_url 非法", "http://", "", "h1", []string{"10.100.112.17"}, false},
		{"主机 id 为空不算匹配", "http://10.0.0.9:8091", "", "", []string{"10.100.112.17"}, false},
	}
	for _, c := range cases {
		if got := AgentMatchesHost(c.baseURL, c.agentInst, c.hostInst, c.addrs...); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestHostAddrsAndMessage(t *testing.T) {
	inst := &store.Instance{Host: "10.100.112.17", DomainName: "Node17.Local", Domains: []string{"node17.local", ""}}
	addrs := HostAddrs(inst)
	if len(addrs) != 2 || addrs[0] != "10.100.112.17" || addrs[1] != "node17.local" {
		t.Fatalf("HostAddrs = %v", addrs)
	}
	msg := MismatchMessage("http://10.100.112.18:8091", "10.100.112.17")
	if !strings.Contains(msg, "10.100.112.18") || !strings.Contains(msg, "10.100.112.17") {
		t.Fatalf("MismatchMessage = %s", msg)
	}
}

func TestAgentAddrs(t *testing.T) {
	inst := &store.Instance{Host: "10.100.112.17", DomainName: "node17.example.com"}
	if got := strings.Join(AgentAddrs(inst, "http://Node17.example.com:8091"), ","); got != "10.100.112.17,node17.example.com" {
		t.Fatalf("base_url 主机已在主机地址中时不应重复: %s", got)
	}
	if got := strings.Join(AgentAddrs(nil, "http://10.100.112.18:8092"), ","); got != "10.100.112.18" {
		t.Fatalf("无主机实例时取 base_url 主机: %s", got)
	}
	if got := AgentAddrs(nil, ""); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestResultFailedAndSummary(t *testing.T) {
	r := NewResult()
	r.Add("cluster", true, "ok")
	r.Warn("fe_count", "偶数")
	if !r.Finish().OK {
		t.Fatal("warn 不应影响 OK")
	}
	r.Add("agent_match", false, "不一致")
	r.Finish()
	if r.OK || r.Failed("duplicate", "agent_match") == nil || r.Failed("duplicate") != nil {
		t.Fatalf("Failed 判定错误: %+v", r)
	}
	if !strings.Contains(r.Summary, "不一致") {
		t.Fatalf("Summary = %s", r.Summary)
	}
}
