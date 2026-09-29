// Package nodecheck 集群「添加节点」前的公共校验：Host/Agent 地址一致、校验结果结构。
package nodecheck

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"auto-dba/internal/store"
)

// Item 单项校验结果；Level=warn 表示仅提醒，不影响 OK。
type Item struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Level   string `json:"level,omitempty"`
	Message string `json:"message,omitempty"`
}

// Result 添加节点基础校验结果。
type Result struct {
	OK      bool   `json:"ok"`
	Items   []Item `json:"items"`
	Summary string `json:"summary,omitempty"`
}

func NewResult() *Result { return &Result{OK: true, Items: []Item{}} }

func (r *Result) Add(name string, ok bool, msg string) {
	r.Items = append(r.Items, Item{Name: name, OK: ok, Message: msg})
	if !ok {
		r.OK = false
	}
}

func (r *Result) Warn(name, msg string) {
	r.Items = append(r.Items, Item{Name: name, OK: true, Level: "warn", Message: msg})
}

// Get 按名称取项；不存在返回 nil。
func (r *Result) Get(name string) *Item {
	for i := range r.Items {
		if r.Items[i].Name == name {
			return &r.Items[i]
		}
	}
	return nil
}

// Set 覆盖同名项（不存在则追加），并重算 OK。
func (r *Result) Set(name string, ok bool, msg string) {
	if it := r.Get(name); it != nil {
		it.OK, it.Level, it.Message = ok, "", msg
	} else {
		r.Items = append(r.Items, Item{Name: name, OK: ok, Message: msg})
	}
	r.OK = true
	for _, it := range r.Items {
		if !it.OK {
			r.OK = false
			break
		}
	}
}

// Failed 指定项中是否有失败。
func (r *Result) Failed(names ...string) *Item {
	for i := range r.Items {
		if r.Items[i].OK {
			continue
		}
		for _, n := range names {
			if r.Items[i].Name == n {
				return &r.Items[i]
			}
		}
	}
	return nil
}

// Finish 生成 Summary。
func (r *Result) Finish() *Result {
	if r.OK {
		r.Summary = "基础校验通过"
		return r
	}
	var bad []string
	for _, it := range r.Items {
		if !it.OK {
			bad = append(bad, it.Message)
		}
	}
	r.Summary = "基础校验未通过：" + strings.Join(bad, "；")
	return r
}

// HostAddrs 主机实例可用于匹配 Agent 的地址（Host / DomainName / Domains）。
func HostAddrs(inst *store.Instance) []string {
	if inst == nil {
		return nil
	}
	out := []string{}
	seen := map[string]bool{}
	add := func(s string) {
		s = normHost(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(inst.Host)
	add(inst.DomainName)
	for _, d := range inst.Domains {
		add(d)
	}
	return out
}

// AgentAddrs Agent 所在主机的匹配地址：主机实例地址加 base_url 主机名（重新绑定节点用）。
func AgentAddrs(inst *store.Instance, baseURL string) []string {
	out := HostAddrs(inst)
	if h, err := AgentHost(baseURL); err == nil {
		for _, a := range out {
			if a == h {
				return out
			}
		}
		out = append(out, h)
	}
	return out
}

// PrimaryAddr 主机实例首选地址（Host，空则 DomainName）。
func PrimaryAddr(inst *store.Instance) string {
	if inst == nil {
		return ""
	}
	if h := strings.TrimSpace(inst.Host); h != "" {
		return h
	}
	return strings.TrimSpace(inst.DomainName)
}

// AgentHost 从 Agent base_url 解析主机名（小写，不含端口）。
func AgentHost(baseURL string) (string, error) {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return "", fmt.Errorf("Agent base_url 为空")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("Agent base_url %q 无法解析", baseURL)
	}
	return normHost(u.Hostname()), nil
}

// AgentMatchesHost Agent 是否属于该主机：host_instance_id 相同，或 base_url 主机名等于主机任一地址。
func AgentMatchesHost(agentBaseURL, agentHostInstID, hostInstID string, hostAddrs ...string) bool {
	if id := strings.TrimSpace(hostInstID); id != "" && strings.TrimSpace(agentHostInstID) == id {
		return true
	}
	h, err := AgentHost(agentBaseURL)
	if err != nil {
		return false
	}
	for _, a := range hostAddrs {
		if n := normHost(a); n != "" && n == h {
			return true
		}
	}
	return false
}

// MismatchMessage 统一的不一致提示。
func MismatchMessage(agentBaseURL, hostAddr string) string {
	h, err := AgentHost(agentBaseURL)
	if err != nil {
		h = strings.TrimSpace(agentBaseURL)
	}
	return fmt.Sprintf("Agent 地址 %s 与所选主机 %s 不一致", h, hostAddr)
}

func normHost(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(strings.TrimSuffix(s, "]"), "[")
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return strings.TrimSuffix(s, ".")
}
