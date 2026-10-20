package k8s

import "testing"

func TestResolveGVR(t *testing.T) {
	g, err := ResolveGVR("pods")
	if err != nil || g.Resource != "pods" {
		t.Fatalf("pods: %+v %v", g, err)
	}
	g, err = ResolveGVR("apps/v1/deployments")
	if err != nil || g.Group != "apps" || g.Resource != "deployments" {
		t.Fatalf("deployments: %+v %v", g, err)
	}
	g, err = ResolveGVR("apps~v1~deployments")
	if err != nil || g.Group != "apps" {
		t.Fatalf("~ form: %+v %v", g, err)
	}
	nodes, _ := ResolveGVR("nodes")
	if !ClusterScoped(nodes) {
		t.Fatal("nodes should be cluster scoped")
	}
	pods, _ := ResolveGVR("pods")
	if ClusterScoped(pods) {
		t.Fatal("pods should be namespaced")
	}
}
