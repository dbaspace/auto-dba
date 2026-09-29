package proxy

import (
	"fmt"
	"strings"
)

// HA 拓扑：三种代理部署至少主备；Nginx 允许更多成员做冗余扩容。
const (
	TopologyHAPair  = "ha_pair"  // 严格 2 节点主备（HAProxy / LVS 默认）
	TopologyHAMulti = "ha_multi" // ≥2 节点，Nginx 可扩容
	RoleMaster      = "master"
	RoleBackup      = "backup"
	MaxNginxNodes   = 8
)

// HANodeSpec 代理 HA 成员（挂在 OptionsJSON.nodes）。
type HANodeSpec struct {
	Name         string `json:"name,omitempty"`
	Role         string `json:"role"` // master | backup
	Priority     int    `json:"priority,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	AgentBaseURL string `json:"agent_base_url,omitempty"`
	AgentUser    string `json:"agent_user,omitempty"`
	Interface    string `json:"interface,omitempty"`
}

// ValidateAndNormalizeHANodes 校验主备拓扑并补齐 priority / role。
// haproxy、lvs：必须恰好 2 节点；nginx：2～MaxNginxNodes。
func ValidateAndNormalizeHANodes(kind string, nodes []HANodeSpec) ([]HANodeSpec, string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if len(nodes) < 2 {
		return nil, "", fmt.Errorf("高可用部署至少需要主+备 2 个节点（均安装 keepalived）")
	}
	switch kind {
	case "haproxy", "lvs":
		if len(nodes) != 2 {
			return nil, "", fmt.Errorf("%s 仅支持主备 2 节点（当前 %d）", kind, len(nodes))
		}
	case "nginx":
		if len(nodes) > MaxNginxNodes {
			return nil, "", fmt.Errorf("nginx 最多 %d 个节点（当前 %d）", MaxNginxNodes, len(nodes))
		}
	default:
		return nil, "", fmt.Errorf("非法 kind: %s", kind)
	}

	out := make([]HANodeSpec, len(nodes))
	copy(out, nodes)
	masterIdx := -1
	seenAgent := map[string]bool{}
	for i := range out {
		n := &out[i]
		role := strings.ToLower(strings.TrimSpace(n.Role))
		if role == "" {
			if i == 0 {
				role = RoleMaster
			} else {
				role = RoleBackup
			}
		}
		if role != RoleMaster && role != RoleBackup {
			return nil, "", fmt.Errorf("节点 role 须为 master|backup")
		}
		n.Role = role
		if role == RoleMaster {
			if masterIdx >= 0 {
				return nil, "", fmt.Errorf("只能有一个 master 节点")
			}
			masterIdx = i
		}
		key := strings.TrimSpace(n.AgentID)
		if key == "" {
			key = strings.TrimSpace(n.AgentBaseURL)
		}
		if key == "" {
			return nil, "", fmt.Errorf("节点须绑定平台 Agent 或填写 Agent URL")
		}
		lk := strings.ToLower(key)
		if seenAgent[lk] {
			return nil, "", fmt.Errorf("节点 Agent 不可重复: %s", key)
		}
		seenAgent[lk] = true
		if strings.TrimSpace(n.Name) == "" {
			if role == RoleMaster {
				n.Name = "master"
			} else {
				n.Name = fmt.Sprintf("backup%d", i)
			}
		}
	}
	if masterIdx < 0 {
		return nil, "", fmt.Errorf("必须指定一个 master 节点")
	}

	// 自动分配 priority：master 最高，其余依次递减
	base := 150
	out[masterIdx].Priority = base
	p := base - 20
	for i := range out {
		if i == masterIdx {
			continue
		}
		if out[i].Priority <= 0 || out[i].Priority >= base {
			out[i].Priority = p
			if p > 50 {
				p -= 10
			}
		}
		out[i].Role = RoleBackup
	}

	topo := TopologyHAPair
	if len(out) > 2 {
		topo = TopologyHAMulti
	}
	return out, topo, nil
}

// LVSOptionsForNode 按节点 role/priority 覆盖共享 VRRP 字段。
func LVSOptionsForNode(shared *LVSOptions, name string, node HANodeSpec) LVSOptions {
	opt := DefaultLVSOptions(name)
	if shared != nil {
		opt = *shared
	}
	if strings.TrimSpace(node.Interface) != "" {
		opt.Interface = node.Interface
		if strings.TrimSpace(opt.VIPDev) == "" || opt.VIPDev == "eth0" {
			opt.VIPDev = node.Interface
		}
	}
	if node.Priority > 0 {
		opt.Priority = node.Priority
	}
	if node.Role == RoleMaster {
		opt.State = "MASTER"
	} else {
		opt.State = "BACKUP"
		opt.Nopreempt = true
	}
	return opt
}

// BuildKeepalivedConfForNode 为单个 HA 成员生成 keepalived 配置。
func BuildKeepalivedConfForNode(kind, name, vip string, port int, backends []Backend, shared *LVSOptions, node HANodeSpec) (string, error) {
	opt := LVSOptionsForNode(shared, name, node)
	return BuildKeepalivedConf(kind, name, vip, port, backends, &opt)
}

// MasterNode 返回 master 成员。
func MasterNode(nodes []HANodeSpec) *HANodeSpec {
	for i := range nodes {
		if nodes[i].Role == RoleMaster {
			return &nodes[i]
		}
	}
	if len(nodes) > 0 {
		return &nodes[0]
	}
	return nil
}
