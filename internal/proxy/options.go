package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ProxyExtraOptions 存于 ProxyService.OptionsJSON（Keepalived 副配置等）。
type ProxyExtraOptions struct {
	KeepalivedPath    string       `json:"keepalived_path,omitempty"`
	KeepalivedContent string       `json:"keepalived_content,omitempty"` // master 节点参考副本
	LVS               *LVSOptions  `json:"lvs,omitempty"`
	Topology          string       `json:"topology,omitempty"` // ha_pair | ha_multi
	Nodes             []HANodeSpec `json:"nodes,omitempty"`
}

const DefaultKeepalivedPath = "/etc/keepalived/keepalived.conf"

// NormalizeVIPHost 去掉 CIDR，返回纯 IP。
func NormalizeVIPHost(vip string) string {
	vip = strings.TrimSpace(vip)
	if i := strings.IndexByte(vip, '/'); i >= 0 {
		vip = vip[:i]
	}
	return strings.TrimSpace(vip)
}

// ParseProxyOptions 解析 OptionsJSON。
func ParseProxyOptions(raw string) ProxyExtraOptions {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ProxyExtraOptions{}
	}
	var o ProxyExtraOptions
	_ = json.Unmarshal([]byte(raw), &o)
	return o
}

func (o ProxyExtraOptions) Encode() string {
	b, err := json.Marshal(o)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// BuildKeepalivedConf 三种代理统一生成 keepalived 配置。
// LVS：VRRP + virtual_server；HAProxy/Nginx：仅 VRRP（VIP）。
func BuildKeepalivedConf(kind, name, vip string, port int, backends []Backend, lvs *LVSOptions) (string, error) {
	if err := ValidateVIP(vip); err != nil {
		return "", err
	}
	opt := DefaultLVSOptions(name)
	if lvs != nil {
		opt = *lvs
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "lvs" {
		return RenderKeepalivedConf(name, vip, port, backends, opt)
	}
	return RenderKeepalivedVRRPOnly(name, vip, opt)
}

// RenderKeepalivedVRRPOnly 仅 VRRP 接管 VIP（供 HAProxy/Nginx 前置 Keepalived）。
func RenderKeepalivedVRRPOnly(name, vip string, opt LVSOptions) (string, error) {
	opt, err := NormalizeLVSOptions(opt, name)
	if err != nil {
		return "", err
	}
	if err := ValidateVIP(vip); err != nil {
		return "", err
	}
	name = sanitizeIdent(name)
	vipLine := formatVIPLine(vip, opt.VIPMask)

	var b strings.Builder
	b.WriteString("# managed by auto-dba — Keepalived VRRP (VIP for proxy)\n")
	b.WriteString("# HAProxy/Nginx 流量面由各自 conf 管理；本文件只负责 VIP 漂移\n\n")
	b.WriteString("global_defs {\n")
	b.WriteString(fmt.Sprintf("  router_id %s\n", opt.RouterID))
	b.WriteString("  vrrp_skip_check_adv_addr\n")
	b.WriteString("  vrrp_garp_master_refresh 10\n")
	b.WriteString("}\n\n")
	b.WriteString(fmt.Sprintf("vrrp_instance VI_%s {\n", name))
	b.WriteString(fmt.Sprintf("  state %s\n", opt.State))
	b.WriteString(fmt.Sprintf("  interface %s\n", opt.Interface))
	b.WriteString(fmt.Sprintf("  virtual_router_id %d\n", opt.VirtualRouterID))
	b.WriteString(fmt.Sprintf("  priority %d\n", opt.Priority))
	b.WriteString(fmt.Sprintf("  advert_int %d\n", opt.AdvertInt))
	if opt.Nopreempt && opt.State == "BACKUP" {
		b.WriteString("  nopreempt\n")
	}
	b.WriteString("  authentication {\n")
	b.WriteString("    auth_type PASS\n")
	b.WriteString(fmt.Sprintf("    auth_pass %s\n", opt.AuthPass))
	b.WriteString("  }\n")
	b.WriteString("  virtual_ipaddress {\n")
	b.WriteString(fmt.Sprintf("    %s dev %s\n", vipLine, opt.VIPDev))
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String(), nil
}

// AttachKeepalivedToOptions 写入 OptionsJSON 中的 keepalived 内容。
func AttachKeepalivedToOptions(existingJSON, path, content string, lvs *LVSOptions) string {
	o := ParseProxyOptions(existingJSON)
	if strings.TrimSpace(path) == "" {
		path = DefaultKeepalivedPath
	}
	o.KeepalivedPath = path
	o.KeepalivedContent = content
	if lvs != nil {
		cp := *lvs
		o.LVS = &cp
	}
	return o.Encode()
}

// AttachHANodesToOptions 写入拓扑与节点列表。
func AttachHANodesToOptions(existingJSON, topology string, nodes []HANodeSpec, lvs *LVSOptions) string {
	o := ParseProxyOptions(existingJSON)
	o.Topology = topology
	o.Nodes = nodes
	if lvs != nil {
		cp := *lvs
		o.LVS = &cp
	}
	return o.Encode()
}
