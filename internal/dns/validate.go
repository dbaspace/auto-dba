package dns

import (
	"fmt"
	"net"
	"strings"
	"unicode"
)

func NormalizeOrigin(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimSuffix(s, ".")
	if s == "" || strings.ContainsAny(s, " \t") || !strings.Contains(s, ".") {
		return "", fmt.Errorf("解析域无效")
	}
	for _, p := range strings.Split(s, ".") {
		if p == "" {
			return "", fmt.Errorf("解析域无效")
		}
	}
	return s + ".", nil
}

func NormalizeHost(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("主机名为空")
	}
	if s == "@" {
		return "@", nil
	}
	if strings.ContainsAny(s, " \t") || strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return "", fmt.Errorf("主机名 %s 无效", raw)
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("主机名 %s 无效", raw)
	}
	return s, nil
}

func NormalizeType(raw string) (string, error) {
	t := strings.ToUpper(strings.TrimSpace(raw))
	switch t {
	case "A", "AAAA", "CNAME", "TXT", "MX":
		return t, nil
	default:
		return "", fmt.Errorf("记录类型仅支持 A、AAAA、CNAME、TXT、MX")
	}
}

func NormalizeTTL(ttl int64) (int64, error) {
	if ttl <= 0 || ttl > 604800 {
		return 0, fmt.Errorf("TTL 无效")
	}
	return ttl, nil
}

// NormalizeData 校验记录值。MX 传入邮件主机和优先级，返回库内 data。
func NormalizeData(rrType, data, mailHost string, pref int) (string, error) {
	data = strings.TrimSpace(data)
	switch rrType {
	case "A":
		ip := net.ParseIP(data)
		if ip == nil || ip.To4() == nil {
			return "", fmt.Errorf("A 记录值须为 IPv4")
		}
		return ip.To4().String(), nil
	case "AAAA":
		ip := net.ParseIP(data)
		if ip == nil || ip.To4() != nil {
			return "", fmt.Errorf("AAAA 记录值须为 IPv6")
		}
		return ip.String(), nil
	case "CNAME", "TXT":
		if data == "" {
			return "", fmt.Errorf("记录值为空")
		}
		return data, nil
	case "MX":
		host := mailHost
		if host == "" {
			host = data
		}
		return EncodeMX(host, pref)
	default:
		return "", fmt.Errorf("记录类型无效")
	}
}
