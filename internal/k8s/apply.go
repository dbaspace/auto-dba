package k8s

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

// ApplyResult dry-run / apply 结果。
type ApplyResult struct {
	DryRun   bool           `json:"dry_run"`
	Applied  bool           `json:"applied"`
	Diff     string         `json:"diff,omitempty"`
	Object   map[string]any `json:"object,omitempty"`
	GVR      string         `json:"gvr,omitempty"`
	Namespace string        `json:"namespace,omitempty"`
	Name     string         `json:"name,omitempty"`
}

// ApplyYAML server-side apply；dryRun=true 时仅试算并生成简单 diff。
func (c *Client) ApplyYAML(ctx context.Context, yamlDoc string, dryRun bool) (*ApplyResult, error) {
	yamlDoc = strings.TrimSpace(yamlDoc)
	if yamlDoc == "" {
		return nil, fmt.Errorf("yaml 为空")
	}
	var obj unstructured.Unstructured
	if err := yaml.Unmarshal([]byte(yamlDoc), &obj.Object); err != nil {
		return nil, fmt.Errorf("解析 YAML: %w", err)
	}
	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" {
		return nil, fmt.Errorf("缺少 kind")
	}
	mapping, err := c.guessGVR(gvk.GroupVersion().String(), gvk.Kind)
	if err != nil {
		return nil, err
	}
	ns := obj.GetNamespace()
	name := obj.GetName()
	if name == "" {
		return nil, fmt.Errorf("缺少 metadata.name")
	}

	res := &ApplyResult{
		DryRun:    dryRun,
		GVR:       mapping.String(),
		Namespace: ns,
		Name:      name,
	}

	var existing *unstructured.Unstructured
	if ClusterScoped(mapping) || ns == "" {
		existing, _ = c.Dynamic.Resource(mapping).Get(ctx, name, metav1.GetOptions{})
	} else {
		existing, _ = c.Dynamic.Resource(mapping).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	}
	if existing != nil {
		oldYAML, _ := yaml.Marshal(sanitizeObject(existing.Object, mapping.Resource == "secrets"))
		newYAML, _ := yaml.Marshal(sanitizeObject(obj.Object, mapping.Resource == "secrets"))
		res.Diff = simpleDiff(string(oldYAML), string(newYAML))
	} else {
		res.Diff = "+ (create)\n" + yamlDoc
	}

	opts := metav1.PatchOptions{
		FieldManager: "keeplus",
		Force:        boolPtr(true),
	}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	}
	data, err := yaml.Marshal(obj.Object)
	if err != nil {
		return nil, err
	}
	// SSA 需要 JSON
	jsonBytes, err := yaml.YAMLToJSON(data)
	if err != nil {
		return nil, err
	}

	var out *unstructured.Unstructured
	if ClusterScoped(mapping) || ns == "" {
		out, err = c.Dynamic.Resource(mapping).Patch(ctx, name, types.ApplyPatchType, jsonBytes, opts)
	} else {
		out, err = c.Dynamic.Resource(mapping).Namespace(ns).Patch(ctx, name, types.ApplyPatchType, jsonBytes, opts)
	}
	if err != nil {
		return nil, err
	}
	res.Object = sanitizeObject(out.Object, mapping.Resource == "secrets")
	res.Applied = !dryRun
	return res, nil
}

func (c *Client) guessGVR(groupVersion, kind string) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(groupVersion)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	_, lists, err := c.Discovery.ServerGroupsAndResources()
	if err != nil && len(lists) == 0 {
		return schema.GroupVersionResource{}, err
	}
	for _, l := range lists {
		if l.GroupVersion != groupVersion && l.GroupVersion != gv.String() {
			continue
		}
		for _, r := range l.APIResources {
			if r.Kind == kind && !strings.Contains(r.Name, "/") {
				return schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: r.Name}, nil
			}
		}
	}
	// fallback 常见 Kind
	alias := strings.ToLower(kind) + "s"
	if g, e := ResolveGVR(alias); e == nil {
		return g, nil
	}
	return schema.GroupVersionResource{}, fmt.Errorf("无法解析 GVR: %s %s", groupVersion, kind)
}

func boolPtr(b bool) *bool { return &b }

func simpleDiff(a, b string) string {
	if a == b {
		return "(no change)"
	}
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	var sb strings.Builder
	max := len(al)
	if len(bl) > max {
		max = len(bl)
	}
	for i := 0; i < max; i++ {
		var left, right string
		if i < len(al) {
			left = al[i]
		}
		if i < len(bl) {
			right = bl[i]
		}
		if left == right {
			continue
		}
		if left != "" {
			sb.WriteString("- " + left + "\n")
		}
		if right != "" {
			sb.WriteString("+ " + right + "\n")
		}
	}
	out := sb.String()
	if out == "" {
		return "(changed)"
	}
	return out
}
