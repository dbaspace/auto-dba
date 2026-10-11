package cmdb

import (
	"os"
	"testing"
	"time"

	"auto-dba/internal/store"

	"github.com/google/uuid"
)

func TestImpactDirectionValidation(t *testing.T) {
	svc := &Service{}
	_, err := svc.ImpactInTenant(TypeInstance, "x", "", "sideways", 1)
	if err == nil {
		t.Fatal("expected direction error")
	}
}

func TestImpactAndGraphDepth(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	tid := store.DefaultTenantID
	suffix := uuid.NewString()[:8]
	inst := &store.Instance{
		ID: "ci-i-" + suffix, TenantID: tid, Name: "cmdb-demo-" + suffix,
		Host: "10.0.0.1", Port: 3306, DBType: "mysql",
		DomainName: "cmdb-demo-" + suffix + ".local", Visibility: "global",
		Approvers: []string{"admin"}, Tags: []string{"app=demo"},
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DeleteInstance(inst.ID) })
	drow := &store.Database{
		ID: "ci-d-" + suffix, TenantID: tid, InstanceID: inst.ID, DBName: "appdb_" + suffix,
		Visibility: "global", Approvers: []string{"admin"}, Tags: []string{"biz=pay"},
	}
	if err := db.CreateDatabase(drow); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DeleteDatabase(drow.ID) })

	svc := New(db)
	g, err := svc.BuildGraphInTenant(TypeInstance, inst.ID, tid, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) < 2 {
		t.Fatalf("expected instance+database nodes, got %d", len(g.Nodes))
	}
	hasContains := false
	for _, e := range g.Edges {
		if e.RelType == "contains" && e.DstID == drow.ID {
			hasContains = true
		}
	}
	if !hasContains {
		t.Fatalf("missing contains edge: %+v", g.Edges)
	}

	rep, err := svc.ImpactInTenant(TypeInstance, inst.ID, tid, "down", 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Downstream < 1 {
		t.Fatalf("expected downstream >=1, got %+v", rep.Summary)
	}

	list, err := svc.ListCIs(ListOpts{Type: TypeInstance, TenantID: tid, Q: inst.Name, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range list {
		if c.ID == inst.ID {
			found = true
			if len(c.AppTags) == 0 {
				t.Fatalf("expected app tags, got %+v", c)
			}
		}
	}
	if !found {
		t.Fatalf("instance not listed")
	}
}
