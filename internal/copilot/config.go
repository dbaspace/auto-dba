package copilot

import (
	"encoding/json"
	"net/url"
	"strings"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

const (
	ProviderOpenAI = "openai"
	ProviderCursor = "cursor"
)

// ModelOption 可选模型（value 发给上游，label 给前端展示）。
type ModelOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Config LLM 上游配置。
type Config struct {
	BaseURL  string
	APIKey   string
	Provider string // openai | cursor
	// Models 来自 copilot.model：单值 / 逗号列表 / JSON [{value,label}]。
	Models []ModelOption
}

// LoadConfig 只读 Nacos/本地 YAML 合并后的 config.Live().Copilot，不读库表全局参数。
// db 参数保留以兼容既有调用方，忽略。
func LoadConfig(_ *store.DB) Config {
	return LoadFromLive()
}

// LoadFromLive 从生效配置加载 Copilot；Live 为空或未配则离线。
func LoadFromLive() Config {
	c := Config{Provider: ProviderOpenAI}
	live := config.Live()
	if live == nil {
		c.Models = []ModelOption{{Value: "gpt-4o-mini", Label: "gpt-4o-mini"}}
		return c
	}
	src := live.Copilot
	if v := sanitizeYAMLScalar(src.BaseURL); v != "" {
		c.BaseURL = NormalizeBaseURL(v)
	}
	if v := sanitizeYAMLScalar(src.APIKey); v != "" {
		c.APIKey = v
	}
	c.Models = ParseModels(src.Model)
	c.Provider = ResolveProvider(sanitizeYAMLScalar(src.Provider), c.BaseURL)
	if len(c.Models) == 0 {
		if c.Provider == ProviderCursor {
			c.Models = []ModelOption{{Value: "auto", Label: "Auto（Cursor 默认）"}}
		} else {
			c.Models = []ModelOption{{Value: "gpt-4o-mini", Label: "gpt-4o-mini"}}
		}
	}
	return c
}

// sanitizeYAMLScalar 去掉首尾空白与一对包裹引号（粘贴进 Nacos 时常见）。
func sanitizeYAMLScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}

// authFailureHint 401 时补充 Nacos 排查说明。
func authFailureHint(provider string) string {
	if provider == ProviderCursor {
		return "；请检查 Nacos copilot.api_key 是否为有效 Cursor User API Key（crsr_…）；通义/qwen 等需改用 openai 兼容 base_url 与 provider: openai，勿挂 api.cursor.com"
	}
	return "；请检查 Nacos copilot.api_key / base_url（OpenAI 兼容网关 /v1）；Cursor Agents 勿与通义模型混用同一段配置"
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.BaseURL) != "" && strings.TrimSpace(c.APIKey) != ""
}

func (c Config) DefaultModel() string {
	if len(c.Models) == 0 {
		if c.Provider == ProviderCursor {
			return "auto"
		}
		return "gpt-4o-mini"
	}
	return c.Models[0].Value
}

// ResolveModel 选用请求模型；空则默认。
// Cursor provider 允许任意 GET /v1/models 返回的 id（含 auto）；OpenAI 则须在配置列表内。
func (c Config) ResolveModel(want string) (string, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		return c.DefaultModel(), nil
	}
	if c.Provider == ProviderCursor {
		return want, nil
	}
	if len(c.Models) == 0 {
		return want, nil
	}
	for _, m := range c.Models {
		if m.Value == want {
			return want, nil
		}
	}
	var ids []string
	for _, m := range c.Models {
		ids = append(ids, m.Value)
	}
	return "", errUnknownModel(want, ids)
}

func errUnknownModel(want string, allowed []string) error {
	return &modelError{want: want, allowed: allowed}
}

type modelError struct {
	want    string
	allowed []string
}

func (e *modelError) Error() string {
	return "未知模型 " + e.want + "，可选：" + strings.Join(e.allowed, ", ")
}

// NormalizeBaseURL 去掉误填的 /chat/completions、/v1/agents、尾斜杠。
func NormalizeBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimRight(s, "/")
	lower := strings.ToLower(s)
	for _, suf := range []string{"/chat/completions", "/v1/agents", "/v1/chat/completions"} {
		if strings.HasSuffix(lower, suf) {
			s = s[:len(s)-len(suf)]
			s = strings.TrimRight(s, "/")
			lower = strings.ToLower(s)
		}
	}
	return s
}

// ResolveProvider 显式 provider 优先；否则按 host 识别 api.cursor.com。
func ResolveProvider(explicit, baseURL string) string {
	switch strings.ToLower(strings.TrimSpace(explicit)) {
	case ProviderCursor:
		return ProviderCursor
	case ProviderOpenAI:
		return ProviderOpenAI
	}
	u, err := url.Parse(baseURL)
	if err == nil {
		host := strings.ToLower(u.Host)
		if host == "api.cursor.com" || (strings.HasPrefix(host, "api.") && strings.HasSuffix(host, ".cursor.com")) {
			return ProviderCursor
		}
	}
	if strings.Contains(strings.ToLower(baseURL), "api.cursor.com") {
		return ProviderCursor
	}
	return ProviderOpenAI
}

// ParseModels 解析 copilot.model：
//   - 单值：gpt-4o-mini
//   - 多值：gpt-4o-mini,qwen-plus 或换行/分号分隔
//   - JSON 字符串数组：["gpt-4o-mini","qwen-plus"]
//   - JSON 对象数组：[{"value":"gpt-4o-mini","label":"轻量"},{"value":"qwen-plus","label":"通义"}]
//     也认 id/model 作 value，name 作 label；第一项为默认。
func ParseModels(raw string) []ModelOption {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var objs []map[string]any
		if err := json.Unmarshal([]byte(raw), &objs); err == nil && len(objs) > 0 {
			var out []ModelOption
			for _, o := range objs {
				v := firstString(o, "value", "id", "model")
				if v == "" {
					continue
				}
				label := firstString(o, "label", "name")
				if label == "" {
					label = v
				}
				out = append(out, ModelOption{Value: v, Label: label})
			}
			return dedupeModels(out)
		}
		var arr []string
		if err := json.Unmarshal([]byte(raw), &arr); err == nil {
			var out []ModelOption
			for _, s := range arr {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				out = append(out, ModelOption{Value: s, Label: s})
			}
			return dedupeModels(out)
		}
	}
	replacer := strings.NewReplacer("\n", ",", ";", ",", "，", ",", "；", ",")
	parts := strings.Split(replacer.Replace(raw), ",")
	var out []ModelOption
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		val, label := p, p
		if i := strings.IndexAny(p, "|:"); i > 0 {
			val = strings.TrimSpace(p[:i])
			label = strings.TrimSpace(p[i+1:])
			if label == "" {
				label = val
			}
		}
		if val == "" {
			continue
		}
		out = append(out, ModelOption{Value: val, Label: label})
	}
	return dedupeModels(out)
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if s := strings.TrimSpace(t); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func dedupeModels(in []ModelOption) []ModelOption {
	seen := map[string]bool{}
	var out []ModelOption
	for _, m := range in {
		if m.Value == "" || seen[m.Value] {
			continue
		}
		seen[m.Value] = true
		out = append(out, m)
	}
	return out
}
