package operator

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"auto-dba/internal/k8s"
)

var srGVR = schema.GroupVersionResource{
	Group: "starrocks.com", Version: "v1", Resource: "starrocksclusters",
}

type starrocksAdapter struct{}

func NewStarRocks() Adapter { return &starrocksAdapter{} }

func (a *starrocksAdapter) Engine() string { return "starrocks" }

func (a *starrocksAdapter) Detect(ctx context.Context, c *k8s.Client) (*OperatorInfo, error) {
	info := &OperatorInfo{Engine: "starrocks", Name: "starrocks-operator", CRDs: []string{"starrocksclusters.starrocks.com"}}
	_, err := c.Discovery.ServerResourcesForGroupVersion("starrocks.com/v1")
	info.Installed = err == nil
	return info, nil
}

func (a *starrocksAdapter) Install(ctx context.Context, c *k8s.Client, opt InstallOpt) error {
	return fmt.Errorf("请使用 StarRocks Operator 官方安装方式")
}

func (a *starrocksAdapter) Render(spec DBClusterSpec) (*unstructured.Unstructured, error) {
	fe := int64(1)
	be := int64(spec.Replicas)
	if be <= 0 {
		be = 3
	}
	size := spec.StorageSize
	if size == "" {
		size = "50Gi"
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "starrocks.com/v1",
		"kind":       "StarRocksCluster",
		"metadata":   map[string]any{"name": spec.Name, "namespace": spec.Namespace},
		"spec": map[string]any{
			"starRocksFeSpec": map[string]any{
				"replicas": fe,
				"image":    "starrocks/fe-ubuntu:" + defaultVer(spec.Version, "3.2.8"),
			},
			"starRocksBeSpec": map[string]any{
				"replicas": be,
				"image":    "starrocks/be-ubuntu:" + defaultVer(spec.Version, "3.2.8"),
				"storageVolumes": []any{
					map[string]any{
						"name":             "be-data",
						"storageClassName": spec.StorageClass,
						"storageSize":      size,
						"mountPath":        "/opt/starrocks/be/storage",
					},
				},
			},
		},
	}}, nil
}

func (a *starrocksAdapter) Status(ctx context.Context, c *k8s.Client, ref CRRef) (*DBClusterStatus, error) {
	obj, err := c.Dynamic.Resource(srGVR).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	st := &DBClusterStatus{}
	st.Phase, _, _ = unstructured.NestedString(obj.Object, "status", "phase")
	st.Endpoint = fmt.Sprintf("%s-fe-0.%s.svc", ref.Name, ref.Namespace)
	return st, nil
}

func (a *starrocksAdapter) Scale(ctx context.Context, c *k8s.Client, ref CRRef, replicas int, res Resources) error {
	patch := fmt.Sprintf(`{"spec":{"starRocksBeSpec":{"replicas":%d}}}`, replicas)
	_, err := c.Dynamic.Resource(srGVR).Namespace(ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

func (a *starrocksAdapter) Upgrade(ctx context.Context, c *k8s.Client, ref CRRef, version string) error {
	v := defaultVer(version, "3.2.8")
	patch := fmt.Sprintf(`{"spec":{"starRocksFeSpec":{"image":%q},"starRocksBeSpec":{"image":%q}}}`,
		"starrocks/fe-ubuntu:"+v, "starrocks/be-ubuntu:"+v)
	_, err := c.Dynamic.Resource(srGVR).Namespace(ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

func (a *starrocksAdapter) Switchover(ctx context.Context, c *k8s.Client, ref CRRef, target string) error {
	return fmt.Errorf("StarRocks FE Leader 由集群自动选举")
}

func (a *starrocksAdapter) Backup(ctx context.Context, c *k8s.Client, ref CRRef, dest BackupDest) error {
	return fmt.Errorf("请使用 StarRocks REPOSITORY/BACKUP 语法或平台备份")
}

func (a *starrocksAdapter) Endpoints(ctx context.Context, c *k8s.Client, ref CRRef) ([]Endpoint, error) {
	return []Endpoint{{Name: "fe", Host: fmt.Sprintf("%s-fe-0.%s.svc", ref.Name, ref.Namespace), Port: 9030, Role: "rw"}}, nil
}
