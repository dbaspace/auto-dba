package instoverview

import "strings"

// RateHit 是一条云监控存储使用率。Rate 为 0–100。
type RateHit struct {
	Name string
	IDC  string
	Rate float64
}

// PickStorageRate 对齐 Grafana 图例：优先实例名，环境能对上 idc 时用那一条。
// 同名只有一条序列时直接采用，避免实例 idc 和云标签不一致时整列为空。
func PickStorageRate(name string, idcs []string, hits []RateHit) (float64, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return 0, false
	}
	var same []RateHit
	for _, h := range hits {
		if strings.ToLower(strings.TrimSpace(h.Name)) != name {
			continue
		}
		same = append(same, h)
	}
	if len(same) == 0 {
		return 0, false
	}
	for _, idc := range idcs {
		idc = strings.ToLower(strings.TrimSpace(idc))
		if idc == "" {
			continue
		}
		for _, h := range same {
			if strings.ToLower(strings.TrimSpace(h.IDC)) == idc {
				return h.Rate, true
			}
		}
	}
	if len(same) == 1 {
		return same[0].Rate, true
	}
	return 0, false
}
