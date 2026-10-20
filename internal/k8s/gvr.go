package k8s

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// 常用资源别名 → GVR。
var aliasGVR = map[string]schema.GroupVersionResource{
	"nodes":               {Group: "", Version: "v1", Resource: "nodes"},
	"namespaces":          {Group: "", Version: "v1", Resource: "namespaces"},
	"pods":                {Group: "", Version: "v1", Resource: "pods"},
	"services":            {Group: "", Version: "v1", Resource: "services"},
	"configmaps":          {Group: "", Version: "v1", Resource: "configmaps"},
	"secrets":             {Group: "", Version: "v1", Resource: "secrets"},
	"persistentvolumes":   {Group: "", Version: "v1", Resource: "persistentvolumes"},
	"persistentvolumeclaims": {Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
	"pvc":                 {Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
	"pv":                  {Group: "", Version: "v1", Resource: "persistentvolumes"},
	"events":              {Group: "", Version: "v1", Resource: "events"},
	"serviceaccounts":     {Group: "", Version: "v1", Resource: "serviceaccounts"},
	"deployments":         {Group: "apps", Version: "v1", Resource: "deployments"},
	"statefulsets":        {Group: "apps", Version: "v1", Resource: "statefulsets"},
	"daemonsets":          {Group: "apps", Version: "v1", Resource: "daemonsets"},
	"replicasets":         {Group: "apps", Version: "v1", Resource: "replicasets"},
	"jobs":                {Group: "batch", Version: "v1", Resource: "jobs"},
	"cronjobs":            {Group: "batch", Version: "v1", Resource: "cronjobs"},
	"ingresses":           {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
	"networkpolicies":     {Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
	"storageclasses":      {Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"},
	"resourcequotas":      {Group: "", Version: "v1", Resource: "resourcequotas"},
	"limitranges":         {Group: "", Version: "v1", Resource: "limitranges"},
}

// ClusterScoped 集群级资源（非命名空间）。
func ClusterScoped(gvr schema.GroupVersionResource) bool {
	switch gvr.Resource {
	case "nodes", "namespaces", "persistentvolumes", "storageclasses":
		return true
	default:
		return false
	}
}

// ResolveGVR 解析别名或 group/version/resource 形式。
func ResolveGVR(alias string) (schema.GroupVersionResource, error) {
	a := strings.ToLower(strings.TrimSpace(alias))
	if a == "" {
		return schema.GroupVersionResource{}, fmt.Errorf("gvr 为空")
	}
	if g, ok := aliasGVR[a]; ok {
		return g, nil
	}
	// group~version~resource 或 group/version/resource
	parts := strings.FieldsFunc(a, func(r rune) bool { return r == '/' || r == '~' })
	if len(parts) == 3 {
		return schema.GroupVersionResource{Group: parts[0], Version: parts[1], Resource: parts[2]}, nil
	}
	if len(parts) == 2 {
		return schema.GroupVersionResource{Group: "", Version: parts[0], Resource: parts[1]}, nil
	}
	return schema.GroupVersionResource{}, fmt.Errorf("未知资源类型: %s", alias)
}

// KnownAliases 返回内置别名列表。
func KnownAliases() []string {
	out := make([]string, 0, len(aliasGVR))
	seen := map[string]bool{}
	for k, g := range aliasGVR {
		if seen[g.Resource] {
			continue
		}
		seen[g.Resource] = true
		out = append(out, k)
	}
	return out
}
