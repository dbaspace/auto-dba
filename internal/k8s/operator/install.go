package operator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"auto-dba/internal/k8s"
)

// 官方安装清单 URL（平台可代装；客户也可自行装好后仅检测）。
var defaultInstallURLs = map[string]string{
	"postgres":  "https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.24/releases/cnpg-1.24.1.yaml",
	"mysql":     "", // Percona 建议 Helm；空则提示
	"redis":     "",
	"starrocks": "",
	"kubeblocks": "",
}

// ApplyManifestYAML 将多文档 YAML apply 到集群（server-side apply）。
func ApplyManifestYAML(ctx context.Context, c *k8s.Client, docs string) (applied int, err error) {
	parts := splitYAMLDocs(docs)
	for _, doc := range parts {
		doc = strings.TrimSpace(doc)
		if doc == "" || doc == "---" {
			continue
		}
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal([]byte(doc), &obj.Object); err != nil {
			return applied, fmt.Errorf("解析 YAML: %w", err)
		}
		if obj.GetKind() == "" || obj.GetName() == "" {
			continue
		}
		gvr, err := resolveInstallGVR(c, &obj)
		if err != nil {
			return applied, err
		}
		ns := obj.GetNamespace()
		data, err := yaml.Marshal(obj.Object)
		if err != nil {
			return applied, err
		}
		jsonBytes, err := yaml.YAMLToJSON(data)
		if err != nil {
			return applied, err
		}
		opts := metav1.PatchOptions{FieldManager: "keeplus", Force: boolPtr(true)}
		var patchErr error
		if ns == "" || !resourceNamespaced(gvr) {
			_, patchErr = c.Dynamic.Resource(gvr).Patch(ctx, obj.GetName(), types.ApplyPatchType, jsonBytes, opts)
		} else {
			_, patchErr = c.Dynamic.Resource(gvr).Namespace(ns).Patch(ctx, obj.GetName(), types.ApplyPatchType, jsonBytes, opts)
		}
		if patchErr != nil {
			// 回退 Create
			if ns == "" || !resourceNamespaced(gvr) {
				_, patchErr = c.Dynamic.Resource(gvr).Create(ctx, &obj, metav1.CreateOptions{})
			} else {
				_, patchErr = c.Dynamic.Resource(gvr).Namespace(ns).Create(ctx, &obj, metav1.CreateOptions{})
			}
		}
		if patchErr != nil {
			return applied, fmt.Errorf("%s/%s: %w", obj.GetKind(), obj.GetName(), patchErr)
		}
		applied++
	}
	return applied, nil
}

// FetchAndApply 从 URL 拉取清单并安装。
func FetchAndApply(ctx context.Context, c *k8s.Client, rawURL string) (int, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return 0, fmt.Errorf("安装 URL 为空")
	}
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("下载清单失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("下载清单 HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, err
	}
	return ApplyManifestYAML(ctx, c, string(b))
}

// DefaultInstallURL 引擎默认清单。
func DefaultInstallURL(engine string) string {
	return defaultInstallURLs[strings.ToLower(strings.TrimSpace(engine))]
}

func splitYAMLDocs(s string) []string {
	return strings.Split(s, "\n---")
}

func resolveInstallGVR(c *k8s.Client, obj *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	gvk := obj.GroupVersionKind()
	_, lists, err := c.Discovery.ServerGroupsAndResources()
	if err != nil && len(lists) == 0 {
		return schema.GroupVersionResource{}, err
	}
	for _, l := range lists {
		if l.GroupVersion != gvk.GroupVersion().String() {
			continue
		}
		for _, r := range l.APIResources {
			if r.Kind == gvk.Kind && !strings.Contains(r.Name, "/") {
				return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: r.Name}, nil
			}
		}
	}
	// CRD 尚未注册时的常见映射
	switch gvk.Kind {
	case "CustomResourceDefinition":
		return schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}, nil
	case "Namespace":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, nil
	case "Deployment":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, nil
	case "Service":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, nil
	case "ServiceAccount":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, nil
	case "ClusterRole":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, nil
	case "ClusterRoleBinding":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, nil
	case "ValidatingWebhookConfiguration":
		return schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations"}, nil
	case "MutatingWebhookConfiguration":
		return schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations"}, nil
	case "ConfigMap":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, nil
	}
	return schema.GroupVersionResource{}, fmt.Errorf("无法解析 GVR: %s", gvk.String())
}

func resourceNamespaced(gvr schema.GroupVersionResource) bool {
	switch gvr.Resource {
	case "namespaces", "nodes", "persistentvolumes", "storageclasses",
		"customresourcedefinitions", "clusterroles", "clusterrolebindings",
		"validatingwebhookconfigurations", "mutatingwebhookconfigurations":
		return false
	default:
		return true
	}
}

func boolPtr(b bool) *bool { return &b }
