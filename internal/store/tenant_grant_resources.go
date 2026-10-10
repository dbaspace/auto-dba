package store

import (
	"fmt"
	"strings"
)

// GrantResourceOption 跨租户授权资源下拉项。
type GrantResourceOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ListGrantResources 列出资源方租户下可授权资源（供租户管理授权弹窗）。
func (d *DB) ListGrantResources(tenantID, resourceType string) ([]GrantResourceOption, error) {
	tenantID = strings.TrimSpace(tenantID)
	resourceType = strings.TrimSpace(resourceType)
	if tenantID == "" {
		return nil, fmt.Errorf("租户必填")
	}
	switch resourceType {
	case "instance":
		list, err := d.ListInstancesInTenant(tenantID, "", "")
		if err != nil {
			return nil, err
		}
		out := make([]GrantResourceOption, 0, len(list))
		for _, it := range list {
			label := strings.TrimSpace(it.Name)
			if label == "" {
				label = it.ID
			}
			if host := strings.TrimSpace(it.DomainName); host != "" {
				label = fmt.Sprintf("%s (%s:%d)", label, host, it.Port)
			}
			out = append(out, GrantResourceOption{ID: it.ID, Label: label})
		}
		return out, nil
	case "database":
		list, err := d.ListDatabasesInTenant(tenantID, "", "")
		if err != nil {
			return nil, err
		}
		instNames := map[string]string{}
		out := make([]GrantResourceOption, 0, len(list))
		for _, it := range list {
			iname := instNames[it.InstanceID]
			if iname == "" {
				if inst, _ := d.GetInstance(it.InstanceID); inst != nil {
					iname = inst.Name
					instNames[it.InstanceID] = iname
				}
			}
			label := strings.TrimSpace(it.DBName)
			if label == "" {
				label = it.ID
			}
			if iname != "" {
				label = fmt.Sprintf("%s @ %s", label, iname)
			}
			out = append(out, GrantResourceOption{ID: it.ID, Label: label})
		}
		return out, nil
	case "sr_cluster":
		list, err := d.ListSrClustersInTenant(tenantID, "")
		if err != nil {
			return nil, err
		}
		out := make([]GrantResourceOption, 0, len(list))
		for _, it := range list {
			label := strings.TrimSpace(it.Name)
			if label == "" {
				label = it.ID
			}
			out = append(out, GrantResourceOption{ID: it.ID, Label: label})
		}
		return out, nil
	case "db_deploy_cluster":
		list, err := d.ListDbDeployClustersInTenant(tenantID, "")
		if err != nil {
			return nil, err
		}
		out := make([]GrantResourceOption, 0, len(list))
		for _, it := range list {
			label := strings.TrimSpace(it.Name)
			if label == "" {
				label = it.ID
			}
			if eng := strings.TrimSpace(it.Engine); eng != "" {
				label = fmt.Sprintf("%s (%s)", label, eng)
			}
			out = append(out, GrantResourceOption{ID: it.ID, Label: label})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("不支持的资源类型 %s", resourceType)
	}
}
