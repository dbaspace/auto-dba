package operator

import "testing"

func TestCNPGRender(t *testing.T) {
	a := NewCNPG()
	obj, err := a.Render(DBClusterSpec{
		Name: "demo", Namespace: "db", Engine: "postgres", Version: "16", Replicas: 2, StorageSize: "10Gi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if obj.GetKind() != "Cluster" {
		t.Fatalf("kind=%s", obj.GetKind())
	}
	if obj.GetName() != "demo" || obj.GetNamespace() != "db" {
		t.Fatalf("meta %+v", obj.Object["metadata"])
	}
}
