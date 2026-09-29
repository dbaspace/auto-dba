package handler

import (
	"strings"
	"testing"
)

func TestMatchHostAgents(t *testing.T) {
	agents := []agentLite{
		{ID: "a1", Name: "sr-17", BaseURL: "http://10.100.112.17:8091", HostInstID: ""},
		{ID: "a2", Name: "", BaseURL: "http://10.100.112.99:8091", HostInstID: "h17"},
		{ID: "a3", Name: "other", BaseURL: "http://10.100.112.18:8091", HostInstID: "h18"},
		{ID: "a4", Name: "dns", BaseURL: "http://Node17.example.com:8092", HostInstID: ""},
	}
	refs := matchHostAgents("sr_agent", agents, "h17", []string{"10.100.112.17", "node17.example.com"})
	got := []string{}
	for _, r := range refs {
		got = append(got, r.ID)
	}
	if strings.Join(got, ",") != "a1,a2,a4" {
		t.Fatalf("got %v", got)
	}
	if refs[1].Name != "a2" {
		t.Fatalf("空名称应回退为 ID: %+v", refs[1])
	}
	if len(matchHostAgents("sr_agent", agents, "h99", []string{"10.0.0.1"})) != 0 {
		t.Fatal("无引用时应为空")
	}
}

func TestFormatHostRefs(t *testing.T) {
	s := formatHostRefs([]hostRef{
		{Kind: "sr_agent", Name: "sr-17", Detail: "http://10.100.112.17:8091"},
		{Kind: "sr_node", Name: "be@10.100.112.17", Detail: "集群 starrocks-test-etl"},
		{Kind: "platform_agent", Name: "p1"},
	})
	want := "Node Agent sr-17（http://10.100.112.17:8091）、StarRocks 节点 be@10.100.112.17（集群 starrocks-test-etl）、Platform Agent p1"
	if s != want {
		t.Fatalf("got %q", s)
	}
}
