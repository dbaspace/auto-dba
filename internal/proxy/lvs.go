package proxy

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// LVSOptions 对齐 keepalived.conf(5) / 官方 LVS 配置模型。
// 参考：https://www.keepalived.org/documentation/user-guide/configuration-synopsis/
type LVSOptions struct {
	// VRRP
	Interface        string `json:"interface"`          // eth0 / ens192
	RouterID         string `json:"router_id"`          // global_defs router_id
	VirtualRouterID  int    `json:"virtual_router_id"`  // 1-255
	Priority         int    `json:"priority"`           // 1-255，MASTER 通常更高
	State            string `json:"state"`              // MASTER | BACKUP
	AuthPass         string `json:"auth_pass"`          // ≤8 字符（PASS）
	AdvertInt        int    `json:"advert_int"`         // 秒
	Nopreempt        bool   `json:"nopreempt"`          // 仅 BACKUP 有意义
	VIPDev           string `json:"vip_dev"`            // VIP 绑定网卡，默认同 interface
	VIPMask          string `json:"vip_mask"`           // 如 /32；空则不加
	// IPVS virtual_server
	LbAlgo             string `json:"lb_algo"`              // rr|wrr|lc|wlc|sh|mh|dh|lblc|lblcr|sed|nq|fo|ovf
	LbKind             string `json:"lb_kind"`              // DR|NAT|TUN（官方 lvs_method）
	DelayLoop          int    `json:"delay_loop"`           // 健康检查轮询秒
	PersistenceTimeout int    `json:"persistence_timeout"`  // 0=关闭会话保持
	Protocol           string `json:"protocol"`             // TCP|UDP
	InhibitOnFailure   bool   `json:"inhibit_on_failure"`   // RS 失败置 weight=0
	ConnectTimeout     int    `json:"connect_timeout"`      // TCP_CHECK
	NbGetRetry         int    `json:"nb_get_retry"`
	DelayBeforeRetry   int    `json:"delay_before_retry"`
	SorryServer        string `json:"sorry_server"` // host:port 可选
}

func DefaultLVSOptions(name string) LVSOptions {
	rid := "AUTO_DBA_" + strings.ToUpper(sanitizeIdent(name))
	if len(rid) > 32 {
		rid = rid[:32]
	}
	return LVSOptions{
		Interface: "eth0", RouterID: rid, VirtualRouterID: 51, Priority: 100,
		State: "BACKUP", AuthPass: "chgme01", AdvertInt: 1,
		LbAlgo: "wrr", LbKind: "DR", DelayLoop: 6, PersistenceTimeout: 0,
		Protocol: "TCP", InhibitOnFailure: true,
		ConnectTimeout: 3, NbGetRetry: 3, DelayBeforeRetry: 3,
		VIPMask: "/32",
	}
}

func sanitizeIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "proxy"
	}
	return out
}

// NormalizeLVSOptions 填充默认值并校验关键字段。
func NormalizeLVSOptions(opt LVSOptions, name string) (LVSOptions, error) {
	def := DefaultLVSOptions(name)
	if strings.TrimSpace(opt.Interface) == "" {
		opt.Interface = def.Interface
	}
	if strings.TrimSpace(opt.RouterID) == "" {
		opt.RouterID = def.RouterID
	}
	if opt.VirtualRouterID <= 0 || opt.VirtualRouterID > 255 {
		opt.VirtualRouterID = def.VirtualRouterID
	}
	if opt.Priority <= 0 || opt.Priority > 255 {
		opt.Priority = def.Priority
	}
	st := strings.ToUpper(strings.TrimSpace(opt.State))
	if st != "MASTER" && st != "BACKUP" {
		st = def.State
	}
	opt.State = st
	if strings.TrimSpace(opt.AuthPass) == "" {
		opt.AuthPass = def.AuthPass
	}
	if len(opt.AuthPass) > 8 {
		return opt, fmt.Errorf("auth_pass 最长 8 字符（keepalived PASS）")
	}
	if opt.AdvertInt <= 0 {
		opt.AdvertInt = def.AdvertInt
	}
	algo := strings.ToLower(strings.TrimSpace(opt.LbAlgo))
	if !validLbAlgo(algo) {
		algo = def.LbAlgo
	}
	opt.LbAlgo = algo
	kind := strings.ToUpper(strings.TrimSpace(opt.LbKind))
	if kind != "DR" && kind != "NAT" && kind != "TUN" {
		kind = def.LbKind
	}
	opt.LbKind = kind
	if opt.DelayLoop <= 0 {
		opt.DelayLoop = def.DelayLoop
	}
	if opt.PersistenceTimeout < 0 {
		opt.PersistenceTimeout = 0
	}
	proto := strings.ToUpper(strings.TrimSpace(opt.Protocol))
	if proto != "TCP" && proto != "UDP" {
		proto = "TCP"
	}
	opt.Protocol = proto
	if opt.ConnectTimeout <= 0 {
		opt.ConnectTimeout = def.ConnectTimeout
	}
	if opt.NbGetRetry <= 0 {
		opt.NbGetRetry = def.NbGetRetry
	}
	if opt.DelayBeforeRetry <= 0 {
		opt.DelayBeforeRetry = def.DelayBeforeRetry
	}
	if strings.TrimSpace(opt.VIPDev) == "" {
		opt.VIPDev = opt.Interface
	}
	return opt, nil
}

func validLbAlgo(a string) bool {
	switch a {
	case "rr", "wrr", "lc", "wlc", "lblc", "lblcr", "dh", "sh", "mh", "sed", "nq", "fo", "ovf", "twos":
		return true
	default:
		return false
	}
}

// ValidateVIP 校验 VIP 为合法 IP（可带 /prefix）。
func ValidateVIP(vip string) error {
	vip = strings.TrimSpace(vip)
	if vip == "" {
		return fmt.Errorf("VIP 必填")
	}
	host := vip
	if i := strings.IndexByte(vip, '/'); i >= 0 {
		host = vip[:i]
	}
	if net.ParseIP(host) == nil {
		return fmt.Errorf("VIP 非法: %s", vip)
	}
	return nil
}

// RenderKeepalivedConf 生成官方风格 keepalived.conf（VRRP + virtual_server）。
func RenderKeepalivedConf(name, vip string, port int, backends []Backend, opt LVSOptions) (string, error) {
	opt, err := NormalizeLVSOptions(opt, name)
	if err != nil {
		return "", err
	}
	if err := ValidateVIP(vip); err != nil {
		return "", err
	}
	if port <= 0 {
		port = 3306
	}
	name = sanitizeIdent(name)
	vipLine := formatVIPLine(vip, opt.VIPMask)
	if len(backends) == 0 {
		backends = []Backend{{Name: "rs1", Host: "10.0.0.11", Port: port, Weight: 100}}
	}

	var b strings.Builder
	b.WriteString("# managed by auto-dba — Keepalived LVS (IPVS)\n")
	b.WriteString("# 对齐 keepalived.conf(5): global_defs / vrrp_instance / virtual_server\n")
	b.WriteString("# lb_kind: DR|NAT|TUN；调度: rr|wrr|lc|wlc|...\n")
	b.WriteString("# DR 模式：RS 需在 lo 配置 VIP 并调 arp_ignore/arp_announce（见文档），平台不代改 RS\n\n")

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
	b.WriteString("}\n\n")

	vipHost := vip
	if i := strings.IndexByte(vip, '/'); i >= 0 {
		vipHost = vip[:i]
	}
	b.WriteString(fmt.Sprintf("virtual_server %s %d {\n", vipHost, port))
	b.WriteString(fmt.Sprintf("  delay_loop %d\n", opt.DelayLoop))
	// 官方同时认可 lb_algo/lb_kind 与 lvs_sched/lvs_method
	b.WriteString(fmt.Sprintf("  lb_algo %s\n", opt.LbAlgo))
	b.WriteString(fmt.Sprintf("  lb_kind %s\n", opt.LbKind))
	b.WriteString(fmt.Sprintf("  protocol %s\n", opt.Protocol))
	if opt.PersistenceTimeout > 0 {
		b.WriteString(fmt.Sprintf("  persistence_timeout %d\n", opt.PersistenceTimeout))
	}
	b.WriteString("\n")

	if ss := strings.TrimSpace(opt.SorryServer); ss != "" {
		h, p, ok := parseHostPort(ss, port)
		if ok {
			b.WriteString(fmt.Sprintf("  sorry_server %s %d\n\n", h, p))
		}
	}

	for i, x := range backends {
		host := strings.TrimSpace(x.Host)
		if host == "" {
			continue
		}
		rp := x.Port
		if rp <= 0 {
			rp = port
		}
		w := x.Weight
		if w <= 0 {
			w = 1
		}
		_ = i
		b.WriteString(fmt.Sprintf("  real_server %s %d {\n", host, rp))
		b.WriteString(fmt.Sprintf("    weight %d\n", w))
		if opt.InhibitOnFailure {
			b.WriteString("    inhibit_on_failure\n")
		}
		b.WriteString("    TCP_CHECK {\n")
		b.WriteString(fmt.Sprintf("      connect_timeout %d\n", opt.ConnectTimeout))
		b.WriteString(fmt.Sprintf("      connect_port %d\n", rp))
		b.WriteString(fmt.Sprintf("      retry %d\n", opt.NbGetRetry))
		b.WriteString(fmt.Sprintf("      delay_before_retry %d\n", opt.DelayBeforeRetry))
		b.WriteString("    }\n")
		b.WriteString("  }\n\n")
	}
	b.WriteString("}\n")

	// DR 模式 RS 侧提示（注释，不执行）
	if opt.LbKind == "DR" {
		b.WriteString("\n# --- RS (real server) DR 侧参考（勿在 Director 执行）---\n")
		b.WriteString(fmt.Sprintf("# ip addr add %s/32 dev lo\n", vipHost))
		b.WriteString("# echo 1 > /proc/sys/net/ipv4/conf/all/arp_ignore\n")
		b.WriteString("# echo 2 > /proc/sys/net/ipv4/conf/all/arp_announce\n")
	}
	return b.String(), nil
}

func formatVIPLine(vip, mask string) string {
	vip = strings.TrimSpace(vip)
	if strings.Contains(vip, "/") {
		return vip
	}
	mask = strings.TrimSpace(mask)
	if mask == "" {
		return vip
	}
	if !strings.HasPrefix(mask, "/") {
		mask = "/" + mask
	}
	return vip + mask
}

func parseHostPort(s string, defPort int) (host string, port int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, false
	}
	if h, p, err := net.SplitHostPort(s); err == nil {
		port, _ = strconv.Atoi(p)
		if port <= 0 {
			port = defPort
		}
		return h, port, true
	}
	if i := strings.LastIndexByte(s, ':'); i > 0 {
		host = s[:i]
		port, _ = strconv.Atoi(s[i+1:])
		if port <= 0 {
			port = defPort
		}
		return host, port, host != ""
	}
	return s, defPort, true
}
