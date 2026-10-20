package operator

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"auto-dba/internal/k8s"
)

var kbGVR = schema.GroupVersionResource{
	Group: "apps.kubeblocks.io", Version: "v1alpha1", Resource: "clusters",
}

// KubeBlocks 可选统一适配器（P4）。
type kubeblocksAdapter struct{}

func NewKubeBlocks() Adapter { return &kubeblocksAdapter{} }

func (a *kubeblocksAdapter) Engine() string { return "kubeblocks" }

func (a *kubeblocksAdapter) Detect(ctx context.Context, c *k8s.Client) (*OperatorInfo, error) {
	info := &OperatorInfo{Engine: "kubeblocks", Name: "kubeblocks", CRDs: []string{"clusters.apps.kubeblocks.io"}}
	_, err := c.Discovery.ServerResourcesForGroupVersion("apps.kubeblocks.io/v1alpha1")
	info.Installed = err == nil
	return info, nil
}

func (a *kubeblocksAdapter) Install(ctx context.Context, c *k8s.Client, opt InstallOpt) error {
	return fmt.Errorf("请使用 kbcli / Helm 安装 KubeBlocks")
}

func (a *kubeblocksAdapter) Render(spec DBClusterSpec) (*unstructured.Unstructured, error) {
	replicas := int64(spec.Replicas)
	if replicas <= 0 {
		replicas = 1
	}
	compDef := "postgresql"
	switch spec.Engine {
	case "mysql":
		compDef = "mysql"
	case "redis":
		compDef = "redis"
	case "postgres", "postgresql", "":
		compDef = "postgresql"
	default:
		compDef = spec.Engine
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps.kubeblocks.io/v1alpha1",
		"kind":       "Cluster",
		"metadata":   map[string]any{"name": spec.Name, "namespace": spec.Namespace},
		"spec": map[string]any{
			"clusterDefinitionRef": compDef,
			"componentSpecs": []any{
				map[string]any{
					"name":     "main",
					"replicas": replicas,
				},
			},
		},
	}}, nil
}

func (a *kubeblocksAdapter) Status(ctx context.Context, c *k8s.Client, ref CRRef) (*DBClusterStatus, error) {
	obj, err := c.Dynamic.Resource(kbGVR).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	st := &DBClusterStatus{}
	st.Phase, _, _ = unstructured.NestedString(obj.Object, "status", "phase")
	return st, nil
}

func (a *kubeblocksAdapter) Scale(ctx context.Context, c *k8s.Client, ref CRRef, replicas int, res Resources) error {
	return fmt.Errorf("KubeBlocks Scale 请通过 OpsRequest")
}

func (a *kubeblocksAdapter) Upgrade(ctx context.Context, c *k8s.Client, ref CRRef, version string) error {
	return fmt.Errorf("KubeBlocks Upgrade 请通过 OpsRequest")
}

func (a *kubeblocksAdapter) Switchover(ctx context.Context, c *k8s.Client, ref CRRef, target string) error {
	return fmt.Errorf("KubeBlocks Switchover 请通过 OpsRequest")
}

func (a *kubeblocksAdapter) Backup(ctx context.Context, c *k8s.Client, ref CRRef, dest BackupDest) error {
	return fmt.Errorf("KubeBlocks Backup 请创建 Backup CR")
}

func (a *kubeblocksAdapter) Endpoints(ctx context.Context, c *k8s.Client, ref CRRef) ([]Endpoint, error) {
	return []Endpoint{{Name: "svc", Host: fmt.Sprintf("%s-main.%s.svc", ref.Name, ref.Namespace), Port: 5432}}, nil
}
