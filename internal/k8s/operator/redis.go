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

var redisGVR = schema.GroupVersionResource{
	Group: "redis.redis.opstreelabs.in", Version: "v1beta2", Resource: "redis",
}

type redisAdapter struct{}

func NewRedisOT() Adapter { return &redisAdapter{} }

func (a *redisAdapter) Engine() string { return "redis" }

func (a *redisAdapter) Detect(ctx context.Context, c *k8s.Client) (*OperatorInfo, error) {
	info := &OperatorInfo{Engine: "redis", Name: "redis-operator", CRDs: []string{"redis.redis.redis.opstreelabs.in"}}
	_, err := c.Discovery.ServerResourcesForGroupVersion("redis.redis.opstreelabs.in/v1beta2")
	info.Installed = err == nil
	return info, nil
}

func (a *redisAdapter) Install(ctx context.Context, c *k8s.Client, opt InstallOpt) error {
	return fmt.Errorf("请使用 OT-Container-Kit redis-operator Helm 安装")
}

func (a *redisAdapter) Render(spec DBClusterSpec) (*unstructured.Unstructured, error) {
	size := spec.StorageSize
	if size == "" {
		size = "10Gi"
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "redis.redis.opstreelabs.in/v1beta2",
		"kind":       "Redis",
		"metadata": map[string]any{"name": spec.Name, "namespace": spec.Namespace},
		"spec": map[string]any{
			"kubernetesConfig": map[string]any{
				"image": "quay.io/opstree/redis:" + defaultVer(spec.Version, "v7.0.12"),
			},
			"storage": map[string]any{
				"volumeClaimTemplate": map[string]any{
					"spec": map[string]any{
						"storageClassName": spec.StorageClass,
						"accessModes":     []any{"ReadWriteOnce"},
						"resources": map[string]any{
							"requests": map[string]any{"storage": size},
						},
					},
				},
			},
		},
	}}, nil
}

func (a *redisAdapter) Status(ctx context.Context, c *k8s.Client, ref CRRef) (*DBClusterStatus, error) {
	obj, err := c.Dynamic.Resource(redisGVR).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	st := &DBClusterStatus{Phase: "Unknown"}
	st.Phase, _, _ = unstructured.NestedString(obj.Object, "status", "state")
	st.Endpoint = fmt.Sprintf("%s.%s.svc", ref.Name, ref.Namespace)
	return st, nil
}

func (a *redisAdapter) Scale(ctx context.Context, c *k8s.Client, ref CRRef, replicas int, res Resources) error {
	return fmt.Errorf("单机 Redis CR 不支持副本扩缩；请使用 RedisCluster CR")
}

func (a *redisAdapter) Upgrade(ctx context.Context, c *k8s.Client, ref CRRef, version string) error {
	img := "quay.io/opstree/redis:v" + defaultVer(version, "7.0.12")
	patch := fmt.Sprintf(`{"spec":{"kubernetesConfig":{"image":%q}}}`, img)
	_, err := c.Dynamic.Resource(redisGVR).Namespace(ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

func (a *redisAdapter) Switchover(ctx context.Context, c *k8s.Client, ref CRRef, target string) error {
	return fmt.Errorf("Redis 切换请使用 Sentinel/Cluster 语义")
}

func (a *redisAdapter) Backup(ctx context.Context, c *k8s.Client, ref CRRef, dest BackupDest) error {
	return fmt.Errorf("Redis 备份请结合 RDB/AOF 与平台备份模块")
}

func (a *redisAdapter) Endpoints(ctx context.Context, c *k8s.Client, ref CRRef) ([]Endpoint, error) {
	return []Endpoint{{Name: "redis", Host: fmt.Sprintf("%s.%s.svc", ref.Name, ref.Namespace), Port: 6379, Role: "rw"}}, nil
}
