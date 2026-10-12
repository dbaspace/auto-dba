package aiops

import (
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

// ResolvedTarget 从告警 labels 解析出的 CMDB / 实例定位。
type ResolvedTarget struct {
	CIType     string `json:"ci_type,omitempty"`
	CIID       string `json:"ci_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Name       string `json:"name,omitempty"`
}

// ResolveTarget 优先 instance_id / ci_*，再按 d_b_instance_name / instance(host:port) / name 反查。
func ResolveTarget(db *store.DB, labels map[string]string) *ResolvedTarget {
	if labels == nil {
		return nil
	}
	if id := firstLabel(labels, "instance_id", "instanceId"); id != "" {
		t := &ResolvedTarget{CIType: "instance", CIID: id, InstanceID: id}
		if db != nil {
			if it, _ := db.GetInstance(id); it != nil {
				t.Host, t.Port, t.Name = it.Host, it.Port, it.Name
			}
		}
		if ciType := firstLabel(labels, "ci_type"); ciType != "" {
			t.CIType = ciType
			if ciID := firstLabel(labels, "ci_id"); ciID != "" {
				t.CIID = ciID
			}
		}
		return t
	}
	if ciType, ciID := firstLabel(labels, "ci_type"), firstLabel(labels, "ci_id"); ciType != "" && ciID != "" {
		t := &ResolvedTarget{CIType: ciType, CIID: ciID}
		if ciType == "instance" {
			t.InstanceID = ciID
			if db != nil {
				if it, _ := db.GetInstance(ciID); it != nil {
					t.Host, t.Port, t.Name = it.Host, it.Port, it.Name
				}
			}
		}
		return t
	}
	if db != nil {
		if name := firstLabel(labels, "d_b_instance_name", "db_instance_name"); name != "" {
			if it := findInstanceByName(db, name); it != nil {
				return &ResolvedTarget{
					CIType: "instance", CIID: it.ID, InstanceID: it.ID,
					Host: it.Host, Port: it.Port, Name: it.Name,
				}
			}
		}
	}
	if inst := firstLabel(labels, "instance"); inst != "" {
		host, port := splitHostPort(inst)
		if host != "" {
			if db != nil {
				if it := findInstanceByHostPort(db, host, port); it != nil {
					return &ResolvedTarget{
						CIType: "instance", CIID: it.ID, InstanceID: it.ID,
						Host: it.Host, Port: it.Port, Name: it.Name,
					}
				}
			}
			return &ResolvedTarget{Host: host, Port: port}
		}
	}
	return nil
}

func firstLabel(labels map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(labels[k]); v != "" {
			return v
		}
	}
	return ""
}

func splitHostPort(s string) (string, int) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0
	}
	// Prometheus instance 常见 host:port；IPv6 暂不特殊处理。
	if i := strings.LastIndex(s, ":"); i > 0 && i < len(s)-1 {
		host := s[:i]
		if p, err := strconv.Atoi(s[i+1:]); err == nil && p > 0 {
			return host, p
		}
	}
	return s, 0
}

func findInstanceByName(db *store.DB, name string) *store.Instance {
	list, err := db.ListInstances("", name)
	if err != nil {
		return nil
	}
	name = strings.TrimSpace(name)
	for i := range list {
		if strings.EqualFold(list[i].Name, name) || strings.EqualFold(list[i].DomainName, name) {
			return &list[i]
		}
	}
	for i := range list {
		if strings.Contains(strings.ToLower(list[i].Name), strings.ToLower(name)) {
			return &list[i]
		}
	}
	return nil
}

func findInstanceByHostPort(db *store.DB, host string, port int) *store.Instance {
	list, err := db.ListInstances("", host)
	if err != nil {
		return nil
	}
	host = strings.TrimSpace(host)
	for i := range list {
		if !strings.EqualFold(list[i].Host, host) {
			continue
		}
		if port <= 0 || list[i].Port == port {
			return &list[i]
		}
	}
	return nil
}
