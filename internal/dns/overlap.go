package dns

import (
	"fmt"
	"net"
	"strings"
)

// CIDRsOverlap 判断两条 CIDR 是否重叠或互相包含。
func CIDRsOverlap(a, b string) (bool, error) {
	_, na, err := net.ParseCIDR(strings.TrimSpace(a))
	if err != nil {
		return false, fmt.Errorf("网段 %s 无效", a)
	}
	_, nb, err := net.ParseCIDR(strings.TrimSpace(b))
	if err != nil {
		return false, fmt.Errorf("网段 %s 无效", b)
	}
	if na.IP.To4() == nil && nb.IP.To4() != nil || na.IP.To4() != nil && nb.IP.To4() == nil {
		return false, nil
	}
	return na.Contains(nb.IP) || nb.Contains(na.IP), nil
}

// CheckCIDRList 拒绝列表内部以及与其他范围已有网段的重叠。
func CheckCIDRList(cidrs []string, others []string) error {
	norm := make([]string, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(c); err != nil {
			return fmt.Errorf("网段 %s 无效", c)
		}
		norm = append(norm, c)
	}
	all := append(append([]string{}, norm...), others...)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			// 只比较「新网段之间」以及「新网段与已有网段」。已有网段彼此不再报错。
			if i >= len(norm) {
				continue
			}
			over, err := CIDRsOverlap(all[i], all[j])
			if err != nil {
				return err
			}
			if over {
				return fmt.Errorf("网段 %s 与 %s 重叠或互相包含", all[i], all[j])
			}
		}
	}
	return nil
}
