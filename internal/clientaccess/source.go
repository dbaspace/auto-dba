package clientaccess

import (
	"fmt"
	"net"
	"strings"
)

// NormalizeSourceCIDRs 把换行、逗号分隔的 IP 或 CIDR 收成一行一条。空或非法条目返回错误。
func NormalizeSourceCIDRs(raw string) (string, error) {
	parts := splitSourceParts(raw)
	if len(parts) == 0 {
		return "", fmt.Errorf("来源地址不能为空")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		canon, err := canonicalSource(p)
		if err != nil {
			return "", fmt.Errorf("来源地址无效：%s", p)
		}
		if seen[canon] {
			continue
		}
		seen[canon] = true
		out = append(out, canon)
	}
	return strings.Join(out, "\n"), nil
}

// SourceAllows 判断 ip 是否落在授权来源里。来源为空或无法解析时拒绝。
func SourceAllows(raw, ip string) bool {
	parts := splitSourceParts(raw)
	if len(parts) == 0 {
		return false
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	for _, p := range parts {
		n, err := parseSourceNet(p)
		if err != nil {
			continue
		}
		if n.Contains(matchIP(parsed, n)) {
			return true
		}
	}
	return false
}

// ParseTrustedProxies 解析信任的反向代理。非法条目跳过。
func ParseTrustedProxies(raw string) []*net.IPNet {
	var out []*net.IPNet
	for _, p := range splitSourceParts(raw) {
		n, err := parseSourceNet(p)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// ObservedClientIP 以 TCP 对端为来源。只有对端落在信任代理里，才取 X-Forwarded-For 最左侧的地址。
func ObservedClientIP(remoteAddr, forwardedFor string, trusted []*net.IPNet) string {
	host := hostOnly(remoteAddr)
	if host == "" {
		return ""
	}
	peer := net.ParseIP(host)
	if peer != nil && len(trusted) > 0 && ipInNets(peer, trusted) {
		if client := leftMostIP(forwardedFor); client != "" {
			return client
		}
	}
	return host
}

func splitSourceParts(raw string) []string {
	raw = strings.ReplaceAll(raw, "，", ",")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	raw = strings.ReplaceAll(raw, ",", "\n")
	var out []string
	for _, p := range strings.Split(raw, "\n") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func canonicalSource(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if ip := net.ParseIP(raw); ip != nil {
		return ip.String(), nil
	}
	_, n, err := net.ParseCIDR(raw)
	if err != nil {
		return "", err
	}
	return n.String(), nil
}

func parseSourceNet(raw string) (*net.IPNet, error) {
	raw = strings.TrimSpace(raw)
	if ip := net.ParseIP(raw); ip != nil {
		bits := 32
		if ip.To4() == nil {
			bits = 128
			ip = ip.To16()
		} else {
			ip = ip.To4()
		}
		return &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}, nil
	}
	_, n, err := net.ParseCIDR(raw)
	return n, err
}

func matchIP(ip net.IP, n *net.IPNet) net.IP {
	if n != nil && n.IP.To4() != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4
		}
	}
	return ip
}

func ipInNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n != nil && n.Contains(matchIP(ip, n)) {
			return true
		}
	}
	return false
}

func leftMostIP(forwarded string) string {
	for _, p := range strings.Split(forwarded, ",") {
		p = strings.TrimSpace(p)
		if ip := net.ParseIP(p); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func hostOnly(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(remote, "[]")
}
