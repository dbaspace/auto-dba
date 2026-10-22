package ticketenv

import (
	"strings"

	"auto-dba/internal/config"
)

const (
	ModeConfigured = "configured"
	ModeDefault    = "default"
	CodeProd       = "prod"
	CodeTest       = "test"
	CodeOther      = "other"
)

// Group 是申请页的一档环境。Codes 为环境管理 / 域名上的具体编码。
type Group struct {
	Code  string   `json:"code"`
	Label string   `json:"label"`
	Codes []string `json:"codes"`
}

// Resolved 是 GET /tickets/env-groups 的 data。
type Resolved struct {
	Mode   string  `json:"mode"`
	Groups []Group `json:"groups"`
}

// Resolve 把 Nacos 分组和当前可见的环境编码收成申请页下拉。
// 未配置时：编码含 test/dev 或等于 uat 归测试，其余归生产。
// 已配置时：未写入任何一组的编码归「其他」。
func Resolve(configured []config.TicketEnvGroup, known []string) Resolved {
	known = uniqueCodes(known)
	if !hasConfigured(configured) {
		prod, test := splitDefault(known)
		return Resolved{
			Mode: ModeDefault,
			Groups: []Group{
				{Code: CodeProd, Label: "生产", Codes: prod},
				{Code: CodeTest, Label: "测试", Codes: test},
			},
		}
	}
	groups := make([]Group, 0, len(configured)+1)
	taken := map[string]struct{}{}
	for _, raw := range configured {
		code := strings.TrimSpace(raw.Code)
		if code == "" {
			continue
		}
		g := Group{Code: code, Label: labelOr(code, raw.Label)}
		for _, c := range raw.Codes {
			n := norm(c)
			if n == "" {
				continue
			}
			if _, ok := taken[n]; ok {
				continue
			}
			taken[n] = struct{}{}
			g.Codes = append(g.Codes, strings.TrimSpace(c))
		}
		if g.Codes == nil {
			g.Codes = []string{}
		}
		groups = append(groups, g)
	}
	var other []string
	for _, c := range known {
		if _, ok := taken[norm(c)]; ok {
			continue
		}
		other = append(other, strings.TrimSpace(c))
	}
	if len(other) > 0 {
		groups = append(groups, Group{Code: CodeOther, Label: "其他", Codes: other})
	}
	return Resolved{Mode: ModeConfigured, Groups: groups}
}

func hasConfigured(groups []config.TicketEnvGroup) bool {
	for _, g := range groups {
		if strings.TrimSpace(g.Code) != "" {
			return true
		}
	}
	return false
}

func splitDefault(known []string) (prod, test []string) {
	for _, c := range known {
		if isDefaultTest(c) {
			test = append(test, strings.TrimSpace(c))
			continue
		}
		prod = append(prod, strings.TrimSpace(c))
	}
	if prod == nil {
		prod = []string{}
	}
	if test == nil {
		test = []string{}
	}
	return prod, test
}

func isDefaultTest(code string) bool {
	s := norm(code)
	if s == "" || s == "default" || s == "unknown" {
		return false
	}
	return s == "uat" || strings.Contains(s, "test") || strings.Contains(s, "dev")
}

func labelOr(code, label string) string {
	if s := strings.TrimSpace(label); s != "" {
		return s
	}
	switch norm(code) {
	case CodeProd:
		return "生产"
	case CodeTest:
		return "测试"
	case CodeOther:
		return "其他"
	default:
		return strings.TrimSpace(code)
	}
}

func uniqueCodes(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, c := range in {
		n := norm(c)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, strings.TrimSpace(c))
	}
	return out
}

func norm(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
