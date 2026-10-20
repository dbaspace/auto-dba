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

var cnpgGVR = schema.GroupVersionResource{
	Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters",
}
var cnpgBackupGVR = schema.GroupVersionResource{
	Group: "postgresql.cnpg.io", Version: "v1", Resource: "backups",
}

type cnpgAdapter struct{}

func NewCNPG() Adapter { return &cnpgAdapter{} }

func (a *cnpgAdapter) Engine() string { return "postgres" }

func (a *cnpgAdapter) Detect(ctx context.Context, c *k8s.Client) (*OperatorInfo, error) {
	info := &OperatorInfo{Engine: "postgres", Name: "cloudnative-pg", CRDs: []string{"clusters.postgresql.cnpg.io"}}
	_, err := c.Discovery.ServerResourcesForGroupVersion("postgresql.cnpg.io/v1")
	if err != nil {
		info.Installed = false
		return info, nil
	}
	info.Installed = true
	// 尝试找部署版本
	deps, err := c.Clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=cloudnative-pg",
	})
	if err == nil && len(deps.Items) > 0 {
		info.Namespace = deps.Items[0].Namespace
		if len(deps.Items[0].Spec.Template.Spec.Containers) > 0 {
			img := deps.Items[0].Spec.Template.Spec.Containers[0].Image
			if i := strings.LastIndex(img, ":"); i >= 0 {
				info.Version = img[i+1:]
			}
		}
	}
	return info, nil
}

func (a *cnpgAdapter) Install(ctx context.Context, c *k8s.Client, opt InstallOpt) error {
	return fmt.Errorf("请使用官方 manifest/Helm 安装 CloudNativePG；平台默认仅检测已安装 Operator")
}

func (a *cnpgAdapter) Render(spec DBClusterSpec) (*unstructured.Unstructured, error) {
	if spec.Name == "" || spec.Namespace == "" {
		return nil, fmt.Errorf("name/namespace 必填")
	}
	replicas := spec.Replicas
	if replicas <= 0 {
		replicas = 1
	}
	storage := spec.StorageSize
	if storage == "" {
		storage = "20Gi"
	}
	image := "ghcr.io/cloudnative-pg/postgresql:16"
	if spec.Version != "" {
		image = "ghcr.io/cloudnative-pg/postgresql:" + strings.TrimPrefix(spec.Version, "v")
	}
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "postgresql.cnpg.io/v1",
			"kind":       "Cluster",
			"metadata": map[string]any{
				"name":      spec.Name,
				"namespace": spec.Namespace,
				"labels":    spec.Labels,
			},
			"spec": map[string]any{
				"instances": replicas,
				"imageName": image,
				"storage": map[string]any{
					"size": storage,
				},
			},
		},
	}
	if spec.StorageClass != "" {
		_ = unstructured.SetNestedField(obj.Object, spec.StorageClass, "spec", "storage", "storageClass")
	}
	if spec.Resources.CPU != "" || spec.Resources.Memory != "" {
		res := map[string]any{}
		req := map[string]any{}
		if spec.Resources.CPU != "" {
			req["cpu"] = spec.Resources.CPU
		}
		if spec.Resources.Memory != "" {
			req["memory"] = spec.Resources.Memory
		}
		if len(req) > 0 {
			res["requests"] = req
			res["limits"] = req
			_ = unstructured.SetNestedMap(obj.Object, res, "spec", "resources")
		}
	}
	return obj, nil
}

func (a *cnpgAdapter) Status(ctx context.Context, c *k8s.Client, ref CRRef) (*DBClusterStatus, error) {
	obj, err := c.Dynamic.Resource(cnpgGVR).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	st := &DBClusterStatus{}
	st.Phase, _, _ = unstructured.NestedString(obj.Object, "status", "phase")
	if n, ok, _ := unstructured.NestedInt64(obj.Object, "status", "readyInstances"); ok {
		st.Ready = int(n)
	}
	if n, ok, _ := unstructured.NestedInt64(obj.Object, "spec", "instances"); ok {
		st.Replicas = int(n)
	}
	st.Primary, _, _ = unstructured.NestedString(obj.Object, "status", "currentPrimary")
	st.Endpoint = fmt.Sprintf("%s-rw.%s.svc", ref.Name, ref.Namespace)
	return st, nil
}

func (a *cnpgAdapter) Scale(ctx context.Context, c *k8s.Client, ref CRRef, replicas int, res Resources) error {
	patch := fmt.Sprintf(`{"spec":{"instances":%d}}`, replicas)
	_, err := c.Dynamic.Resource(cnpgGVR).Namespace(ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

func (a *cnpgAdapter) Upgrade(ctx context.Context, c *k8s.Client, ref CRRef, version string) error {
	img := "ghcr.io/cloudnative-pg/postgresql:" + strings.TrimPrefix(version, "v")
	patch := fmt.Sprintf(`{"spec":{"imageName":%q}}`, img)
	_, err := c.Dynamic.Resource(cnpgGVR).Namespace(ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

func (a *cnpgAdapter) Switchover(ctx context.Context, c *k8s.Client, ref CRRef, target string) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("target primary 必填")
	}
	patch := fmt.Sprintf(`{"spec":{"primaryUpdateStrategy":"unsupervised","primaryUpdateMethod":"switchover"},"status":{"targetPrimary":%q}}`, target)
	// CNPG 推荐通过 annotation / 专用 API；此处 patch targetPrimary 为常见做法
	patch = fmt.Sprintf(`{"metadata":{"annotations":{"cnpg.io/switchover":%q}}}`, target)
	_, err := c.Dynamic.Resource(cnpgGVR).Namespace(ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	_ = patch
	// 更稳妥：直接 patch status 不被允许时，用 spec 亲和
	patch2 := fmt.Sprintf(`{"spec":{"failoverDelay":0},"metadata":{"annotations":{"keeplus.io/target-primary":%q}}}`, target)
	_, err = c.Dynamic.Resource(cnpgGVR).Namespace(ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, []byte(patch2), metav1.PatchOptions{})
	return err
}

func (a *cnpgAdapter) Backup(ctx context.Context, c *k8s.Client, ref CRRef, dest BackupDest) error {
	name := ref.Name + "-backup-" + metav1.Now().Format("20060102150405")
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Backup",
		"metadata": map[string]any{
			"name":      name,
			"namespace": ref.Namespace,
		},
		"spec": map[string]any{
			"cluster": map[string]any{"name": ref.Name},
		},
	}}
	_, err := c.Dynamic.Resource(cnpgBackupGVR).Namespace(ref.Namespace).Create(ctx, obj, metav1.CreateOptions{})
	return err
}

func (a *cnpgAdapter) Endpoints(ctx context.Context, c *k8s.Client, ref CRRef) ([]Endpoint, error) {
	return []Endpoint{
		{Name: "rw", Host: fmt.Sprintf("%s-rw.%s.svc", ref.Name, ref.Namespace), Port: 5432, Role: "rw"},
		{Name: "ro", Host: fmt.Sprintf("%s-ro.%s.svc", ref.Name, ref.Namespace), Port: 5432, Role: "ro"},
	}, nil
}
