package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

// ListOpts 列表参数。
type ListOpts struct {
	Namespace     string
	LabelSelector string
	FieldSelector string
	Limit         int64
	Continue      string
}

// ListResult 列表结果。
type ListResult struct {
	Items           []map[string]any `json:"items"`
	Continue        string           `json:"continue,omitempty"`
	ResourceVersion string           `json:"resource_version,omitempty"`
}

// List 列举资源。
func (c *Client) List(ctx context.Context, gvr schema.GroupVersionResource, opts ListOpts) (*ListResult, error) {
	lo := metav1.ListOptions{
		LabelSelector: opts.LabelSelector,
		FieldSelector: opts.FieldSelector,
		Continue:      opts.Continue,
	}
	if opts.Limit > 0 {
		lo.Limit = opts.Limit
	} else {
		lo.Limit = 200
	}
	var (
		list *unstructured.UnstructuredList
		err  error
	)
	if ClusterScoped(gvr) || opts.Namespace == "" || opts.Namespace == "*" {
		list, err = c.Dynamic.Resource(gvr).List(ctx, lo)
	} else {
		list, err = c.Dynamic.Resource(gvr).Namespace(opts.Namespace).List(ctx, lo)
	}
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(list.Items))
	for i := range list.Items {
		obj := sanitizeObject(list.Items[i].Object, gvr.Resource == "secrets")
		items = append(items, obj)
	}
	return &ListResult{
		Items:           items,
		Continue:        list.GetContinue(),
		ResourceVersion: list.GetResourceVersion(),
	}, nil
}

// Get 获取单个资源；asYAML=true 时返回 YAML 字符串放在 result["yaml"]。
func (c *Client) Get(ctx context.Context, gvr schema.GroupVersionResource, ns, name string, asYAML bool) (map[string]any, error) {
	var (
		obj *unstructured.Unstructured
		err error
	)
	if ClusterScoped(gvr) || ns == "" {
		obj, err = c.Dynamic.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = c.Dynamic.Resource(gvr).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, err
	}
	m := sanitizeObject(obj.Object, gvr.Resource == "secrets")
	if asYAML {
		b, err := yaml.Marshal(m)
		if err != nil {
			return nil, err
		}
		return map[string]any{"yaml": string(b), "object": m}, nil
	}
	return m, nil
}

// Delete 删除资源。
func (c *Client) Delete(ctx context.Context, gvr schema.GroupVersionResource, ns, name string) error {
	opts := metav1.DeleteOptions{}
	if ClusterScoped(gvr) || ns == "" {
		return c.Dynamic.Resource(gvr).Delete(ctx, name, opts)
	}
	return c.Dynamic.Resource(gvr).Namespace(ns).Delete(ctx, name, opts)
}

// Scale 设置 replicas（Deployment / StatefulSet）。
func (c *Client) Scale(ctx context.Context, kind, ns, name string, replicas int32) error {
	gvr, err := ResolveGVR(kind)
	if err != nil {
		return err
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	_, err = c.Dynamic.Resource(gvr).Namespace(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// RestartWorkload 通过注解触发 rollout restart。
func (c *Client) RestartWorkload(ctx context.Context, kind, ns, name string) error {
	gvr, err := ResolveGVR(kind)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`,
		metav1.Now().UTC().Format("2006-01-02T15:04:05Z"))
	_, err = c.Dynamic.Resource(gvr).Namespace(ns).Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

// Cordon 设置节点不可调度。
func (c *Client) Cordon(ctx context.Context, name string, unschedulable bool) error {
	patch := []byte(fmt.Sprintf(`{"spec":{"unschedulable":%v}}`, unschedulable))
	_, err := c.Clientset.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// Drain 驱逐节点上的 Pod（尽力而为）。
func (c *Client) Drain(ctx context.Context, nodeName string, grace int64) (evicted int, err error) {
	if err := c.Cordon(ctx, nodeName, true); err != nil {
		return 0, err
	}
	pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return 0, err
	}
	if grace <= 0 {
		grace = 30
	}
	for _, p := range pods.Items {
		if p.Namespace == "kube-system" {
			continue
		}
		if isDaemonSetPod(&p) {
			continue
		}
		pol := metav1.DeletePropagationForeground
		err := c.Clientset.CoreV1().Pods(p.Namespace).Delete(ctx, p.Name, metav1.DeleteOptions{
			GracePeriodSeconds: &grace,
			PropagationPolicy:  &pol,
		})
		if err == nil {
			evicted++
		}
	}
	return evicted, nil
}

func isDaemonSetPod(p *corev1.Pod) bool {
	for _, o := range p.OwnerReferences {
		if o.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

// DiscoverAPIResources 发现可浏览的 API 资源。
func (c *Client) DiscoverAPIResources(ctx context.Context) ([]map[string]any, error) {
	_, lists, err := c.Discovery.ServerGroupsAndResources()
	if err != nil && len(lists) == 0 {
		return nil, err
	}
	var out []map[string]any
	for _, l := range lists {
		gv, err := schema.ParseGroupVersion(l.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range l.APIResources {
			if strings.Contains(r.Name, "/") {
				continue
			}
			out = append(out, map[string]any{
				"group":         gv.Group,
				"version":       gv.Version,
				"resource":      r.Name,
				"kind":          r.Kind,
				"namespaced":    r.Namespaced,
				"verbs":         r.Verbs,
				"gvr":           fmt.Sprintf("%s/%s/%s", gv.Group, gv.Version, r.Name),
			})
		}
	}
	return out, nil
}

func sanitizeObject(obj map[string]any, secret bool) map[string]any {
	if obj == nil {
		return map[string]any{}
	}
	// 深拷贝简化：JSON round-trip
	b, err := json.Marshal(obj)
	if err != nil {
		return obj
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return obj
	}
	if secret {
		if data, ok := m["data"].(map[string]any); ok {
			keys := make([]string, 0, len(data))
			for k := range data {
				keys = append(keys, k)
			}
			m["data"] = map[string]any{"_keys": keys, "_redacted": true}
		}
		if stringData, ok := m["stringData"].(map[string]any); ok {
			keys := make([]string, 0, len(stringData))
			for k := range stringData {
				keys = append(keys, k)
			}
			m["stringData"] = map[string]any{"_keys": keys, "_redacted": true}
		}
	}
	return m
}
