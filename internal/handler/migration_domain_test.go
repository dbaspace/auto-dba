package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestMatchAccessDomain(t *testing.T) {
	rows := []store.AccessDomain{
		{InstanceID: "i1", Domain: "pg.test.example", EnvCode: "test", DBType: "postgres"},
		{InstanceID: "i1", Domain: "pg.prod.example", EnvCode: "prod", DBType: "postgres"},
		{InstanceID: "i2", Domain: "pg.test.example", EnvCode: "test", DBType: "mysql"},
	}
	host, ok := matchAccessDomain(rows, "i1", "PG.TEST.EXAMPLE.", "postgresql", "test")
	if !ok || host != "pg.test.example" {
		t.Fatalf("match got %q %v", host, ok)
	}
	if _, ok := matchAccessDomain(rows, "i1", "pg.test.example", "postgres", "prod"); ok {
		t.Fatal("env mismatch should fail")
	}
	if _, ok := matchAccessDomain(rows, "i9", "pg.test.example", "postgres", "test"); ok {
		t.Fatal("instance mismatch should fail")
	}
	if _, ok := matchAccessDomain(rows, "i2", "pg.test.example", "postgres", "test"); ok {
		t.Fatal("engine mismatch should fail")
	}
	host, ok = matchAccessDomain(rows, "i1", "pg.prod.example", "postgres", "")
	if !ok || host != "pg.prod.example" {
		t.Fatalf("empty env should match stored domain, got %q %v", host, ok)
	}
}

func TestApplyStoredEndpointHostsKeepsDomain(t *testing.T) {
	tks := &store.MigrationTask{SourceHost: "pg.test.example", TargetHost: "pg.bj.example"}
	src := &store.Instance{Host: "10.0.0.1", Port: 5432, DBType: "postgresql"}
	tgt := &store.Instance{Host: "10.0.0.2", Port: 5432}
	applyStoredEndpointHosts(tks, src, tgt)
	if tks.SourceHost != "pg.test.example" || tks.TargetHost != "pg.bj.example" {
		t.Fatalf("hosts overwritten: %+v", tks)
	}
	if tks.SourcePort != 5432 || tks.TargetPort != 5432 {
		t.Fatalf("ports %d %d", tks.SourcePort, tks.TargetPort)
	}
	if tks.DBType != "postgres" {
		t.Fatalf("db type %q", tks.DBType)
	}

	blank := &store.MigrationTask{}
	applyStoredEndpointHosts(blank, src, tgt)
	if blank.SourceHost != "10.0.0.1" || blank.TargetHost != "10.0.0.2" {
		t.Fatalf("empty task should fall back to instance host: %+v", blank)
	}
}
