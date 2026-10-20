package operator

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"auto-dba/internal/k8s"
)

var pxcGVR = schema.GroupVersionResource{
	Group: "pxc.percona.com", Version: "v1", Resource: "perconaxtradbclusters",
}

type perconaAdapter struct{}

func NewPerconaMySQL() Adapter { return &perconaAdapter{} }

func (a *perconaAdapter) Engine() string { return "mysql" }

func (a *perconaAdapter) Detect(ctx context.Context, c *k8s.Client) (*OperatorInfo, error) {
	info := &OperatorInfo{Engine: "mysql", Name: "percona-xtradb-cluster-operator", CRDs: []string{"perconaxtradbclusters.pxc.percona.com"}}
	_, err := c.Discovery.ServerResourcesForGroupVersion("pxc.percona.com/v1")
	info.Installed = err == nil
	return info, nil
}

func (a *perconaAdapter) Install(ctx context.Context, c *k8s.Client, opt InstallOpt) error {
	if u := strings.TrimSpace(opt.Version); strings.HasPrefix(u, "http") {
		_, err := FetchAndApply(ctx, c, u)
		return err
	}
	return fmt.Errorf("Percona 请提供 InstallOpt.Version=清单 URL，或使用官方 Helm；也可仅检测已装 Operator")
}

func (a *perconaAdapter) Render(spec DBClusterSpec) (*unstructured.Unstructured, error) {
	replicas := int64(spec.Replicas)
	if replicas <= 0 {
		replicas = 3
	}
	size := spec.StorageSize
	if size == "" {
		size = "20Gi"
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "pxc.percona.com/v1",
		"kind":       "PerconaXtraDBCluster",
		"metadata": map[string]any{
			"name": spec.Name, "namespace": spec.Namespace, "labels": spec.Labels,
		},
		"spec": map[string]any{
			"crVersion": "1.14.0",
			"secretsName": spec.Name + "-secrets",
			"pxc": map[string]any{
				"size":    replicas,
				"image":   "percona/percona-xtradb-cluster:" + defaultVer(spec.Version, "8.0.36"),
				"volumeSpec": map[string]any{
					"persistentVolumeClaim": map[string]any{
						"storageClassName": spec.StorageClass,
						"resources": map[string]any{
							"requests": map[string]any{"storage": size},
						},
					},
				},
			},
			"haproxy": map[string]any{
				"enabled": true,
				"size":    int64(1),
				"image":   "percona/percona-xtradb-cluster-operator:1.14.0-haproxy",
			},
		},
	}}, nil
}

func (a *perconaAdapter) Status(ctx context.Context, c *k8s.Client, ref CRRef) (*DBClusterStatus, error) {
	obj, err := c.Dynamic.Resource(pxcGVR).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	st := &DBClusterStatus{}
	st.Phase, _, _ = unstructured.NestedString(obj.Object, "status", "state")
	st.Endpoint = fmt.Sprintf("%s-haproxy.%s.svc", ref.Name, ref.Namespace)
	return st, nil
}

func (a *perconaAdapter) Scale(ctx context.Context, c *k8s.Client, ref CRRef, replicas int, res Resources) error {
	patch := fmt.Sprintf(`{"spec":{"pxc":{"size":%d}}}`, replicas)
	_, err := c.Dynamic.Resource(pxcGVR).Namespace(ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

func (a *perconaAdapter) Upgrade(ctx context.Context, c *k8s.Client, ref CRRef, version string) error {
	img := "percona/percona-xtradb-cluster:" + strings.TrimPrefix(version, "v")
	patch := fmt.Sprintf(`{"spec":{"pxc":{"image":%q}}}`, img)
	_, err := c.Dynamic.Resource(pxcGVR).Namespace(ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

func (a *perconaAdapter) Switchover(ctx context.Context, c *k8s.Client, ref CRRef, target string) error {
	return fmt.Errorf("Percona PXC 切换请按 Operator 手册执行；平台已记录目标 %s", target)
}

func (a *perconaAdapter) Backup(ctx context.Context, c *k8s.Client, ref CRRef, dest BackupDest) error {
	return fmt.Errorf("请创建 PerconaXtraDBClusterBackup CR；目标 kind=%s", dest.Kind)
}

func (a *perconaAdapter) Endpoints(ctx context.Context, c *k8s.Client, ref CRRef) ([]Endpoint, error) {
	return []Endpoint{{Name: "haproxy", Host: fmt.Sprintf("%s-haproxy.%s.svc", ref.Name, ref.Namespace), Port: 3306, Role: "rw"}}, nil
}

func defaultVer(v, def string) string {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if v == "" {
		return def
	}
	return v
}
