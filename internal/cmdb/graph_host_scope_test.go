package cmdb

import (
	"testing"

	"auto-dba/internal/store"
)

func TestInstanceAllowedOnSrHost(t *testing.T) {
	srID := "sr-1"
	allow := map[string]bool{"host-agent-1": true}

	pg := &store.Instance{ID: "pg-1", DBType: "postgres", Tags: []string{}}
	if instanceAllowedOnSrHost(pg, srID, allow) {
		t.Fatal("postgres sibling should be filtered")
	}

	tagged := &store.Instance{ID: "sr-fe", DBType: "starrocks", Tags: []string{"sr-cluster=" + srID}}
	if !instanceAllowedOnSrHost(tagged, srID, allow) {
		t.Fatal("tagged SR instance should be allowed")
	}

	agent := &store.Instance{ID: "host-agent-1", DBType: "host"}
	if !instanceAllowedOnSrHost(agent, srID, allow) {
		t.Fatal("host agent should be allowed")
	}

	nodeBound := &store.Instance{ID: "host-agent-1", DBType: "mysql", Tags: nil}
	if !instanceAllowedOnSrHost(nodeBound, srID, allow) {
		t.Fatal("SR node HostInstanceID should be allowed")
	}
}

func TestResolveExpandCtx(t *testing.T) {
	svc := &Service{}
	ctx := svc.resolveExpandCtx(TypeHost, "host:10.0.0.1")
	if ctx.SrClusterID != "" {
		t.Fatalf("host root should not set SR scope, got %q", ctx.SrClusterID)
	}
	ctx = svc.resolveExpandCtx(TypeSrCluster, "sr-abc")
	if ctx.SrClusterID != "sr-abc" {
		t.Fatalf("sr_cluster root scope = %q", ctx.SrClusterID)
	}
}
