package rediscmd

import (
	"fmt"
	"strconv"
	"strings"
)

// CmdOverride 可配置的单命令覆盖（相对内置目录）。
type CmdOverride struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled,omitempty"`
	MinArgs *int   `json:"min_args,omitempty"`
	Label   string `json:"label,omitempty"`
}

// RulesConfig 持久化的 Redis 工单命令规则（GlobalArg）。
type RulesConfig struct {
	Blacklist []string      `json:"blacklist"`
	Commands  []CmdOverride `json:"commands"`
}

// EffectiveCommand 合并后的可执行命令视图（含内置元数据）。
type EffectiveCommand struct {
	Group   string `json:"group"`
	Name    string `json:"name"`
	Syntax  string `json:"syntax"`
	Args    []Arg  `json:"args"`
	DocsURL string `json:"docs_url"`
	CRUD    string `json:"crud"`
	MinArgs int    `json:"min_args"`
	Enabled bool   `json:"enabled"`
	Label   string `json:"label,omitempty"`
}

// DefaultRulesConfig 从内置目录生成默认配置快照。
func DefaultRulesConfig() RulesConfig {
	bl := make([]string, 0, len(blacklist))
	for k := range blacklist {
		bl = append(bl, k)
	}
	// 稳定顺序
	sortStrings(bl)
	cmds := make([]CmdOverride, 0, len(catalog))
	en := true
	for _, c := range catalog {
		min := c.MinArgs
		name := c.Name
		cmds = append(cmds, CmdOverride{
			Name:    name,
			Enabled: boolPtr(en),
			MinArgs: intPtr(min),
			Label:   c.Group + " / " + c.Name,
		})
	}
	return RulesConfig{Blacklist: bl, Commands: cmds}
}

// MergeRules 用 saved 覆盖默认：黑名单若 saved 非 nil 则整表替换；命令按 name 合并 enabled/min_args。
func MergeRules(saved *RulesConfig) RulesConfig {
	base := DefaultRulesConfig()
	if saved == nil {
		return base
	}
	if saved.Blacklist != nil {
		base.Blacklist = normalizeUpperList(saved.Blacklist)
	}
	byName := map[string]CmdOverride{}
	for _, c := range saved.Commands {
		n := strings.ToUpper(strings.TrimSpace(c.Name))
		if n == "" {
			continue
		}
		c.Name = n
		byName[n] = c
	}
	for i := range base.Commands {
		ov, ok := byName[base.Commands[i].Name]
		if !ok {
			continue
		}
		if ov.Enabled != nil {
			base.Commands[i].Enabled = boolPtr(*ov.Enabled)
		}
		if ov.MinArgs != nil && *ov.MinArgs >= 0 {
			base.Commands[i].MinArgs = intPtr(*ov.MinArgs)
		}
		if strings.TrimSpace(ov.Label) != "" {
			base.Commands[i].Label = ov.Label
		}
	}
	return base
}

// EffectiveCatalog 返回合并后的完整命令列表（含 disabled，供配置页）。
func EffectiveCatalog(saved *RulesConfig) []EffectiveCommand {
	merged := MergeRules(saved)
	ov := map[string]CmdOverride{}
	for _, c := range merged.Commands {
		ov[c.Name] = c
	}
	out := make([]EffectiveCommand, 0, len(catalog))
	for _, c := range catalog {
		ec := EffectiveCommand{
			Group: c.Group, Name: c.Name, Syntax: c.Syntax, Args: c.Args,
			DocsURL: c.DocsURL, CRUD: c.CRUD, MinArgs: c.MinArgs, Enabled: true,
			Label: c.Group + " / " + c.Name,
		}
		if o, ok := ov[c.Name]; ok {
			if o.Enabled != nil {
				ec.Enabled = *o.Enabled
			}
			if o.MinArgs != nil {
				ec.MinArgs = *o.MinArgs
			}
			if o.Label != "" {
				ec.Label = o.Label
			}
		}
		out = append(out, ec)
	}
	return out
}

// EnabledCatalog 仅返回启用中的命令（供工单页选型/校验白名单）。
func EnabledCatalog(saved *RulesConfig) []Command {
	var out []Command
	for _, ec := range EffectiveCatalog(saved) {
		if !ec.Enabled {
			continue
		}
		out = append(out, Command{
			Group: ec.Group, Name: ec.Name, Syntax: ec.Syntax, Args: ec.Args,
			DocsURL: ec.DocsURL, CRUD: ec.CRUD, MinArgs: ec.MinArgs,
		})
	}
	return out
}

func (r RulesConfig) blacklistSet() map[string]struct{} {
	m := make(map[string]struct{}, len(r.Blacklist))
	for _, x := range r.Blacklist {
		x = strings.ToUpper(strings.TrimSpace(x))
		if x != "" {
			m[x] = struct{}{}
		}
	}
	return m
}

func indexFromMerged(merged RulesConfig) map[string]EffectiveCommand {
	ov := map[string]CmdOverride{}
	for _, c := range merged.Commands {
		ov[strings.ToUpper(c.Name)] = c
	}
	m := make(map[string]EffectiveCommand, len(catalog))
	for _, c := range catalog {
		ec := EffectiveCommand{
			Group: c.Group, Name: c.Name, Syntax: c.Syntax, Args: c.Args,
			DocsURL: c.DocsURL, CRUD: c.CRUD, MinArgs: c.MinArgs, Enabled: true,
		}
		if o, ok := ov[c.Name]; ok {
			if o.Enabled != nil {
				ec.Enabled = *o.Enabled
			}
			if o.MinArgs != nil {
				ec.MinArgs = *o.MinArgs
			}
		}
		m[c.Name] = ec
	}
	return m
}

// ValidateCommandWith 使用指定规则配置校验单行命令。
func ValidateCommandWith(saved *RulesConfig, line string) error {
	merged := MergeRules(saved)
	parts, err := SplitArgs(line)
	if err != nil {
		return fmt.Errorf("命令解析失败: %v", err)
	}
	if len(parts) == 0 {
		return fmt.Errorf("命令不能为空")
	}
	cmd := strings.ToUpper(parts[0])
	if _, bad := merged.blacklistSet()[cmd]; bad {
		return fmt.Errorf("禁止执行危险命令: %s", cmd)
	}
	meta, ok := indexFromMerged(merged)[cmd]
	if !ok || !meta.Enabled {
		return fmt.Errorf("命令不在允许的 CRUD 白名单内: %s", cmd)
	}
	argc := len(parts) - 1
	if argc < meta.MinArgs {
		return fmt.Errorf("%s 至少需要 %d 个参数（当前 %d）", meta.Name, meta.MinArgs, argc)
	}
	return nil
}

// ParseScriptWith 使用配置解析多行脚本。
func ParseScriptWith(saved *RulesConfig, text string) ScriptStats {
	merged := MergeRules(saved)
	idx := indexFromMerged(merged)
	var st ScriptStats
	best := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			st.CommentCount++
			continue
		}
		if err := ValidateCommandWith(&merged, line); err != nil {
			st.Invalid = append(st.Invalid, line)
			continue
		}
		st.Valid = append(st.Valid, line)
		parts := ParseLine(line)
		if len(parts) == 0 {
			continue
		}
		if meta, ok := idx[strings.ToUpper(parts[0])]; ok {
			if r := crudRank[strings.ToLower(meta.CRUD)]; r > best {
				best = r
				st.HighestCRUD = meta.CRUD
			}
		}
	}
	st.HighestRiskCN = crudCN(st.HighestCRUD)
	return st
}

// ValidateScriptWith 校验整段脚本。
func ValidateScriptWith(saved *RulesConfig, text string) error {
	st := ParseScriptWith(saved, text)
	if len(st.Invalid) > 0 {
		return fmt.Errorf("存在不支持的命令: %s", st.Invalid[0])
	}
	if len(st.Valid) == 0 {
		return fmt.Errorf("至少需要一条有效 Redis 命令")
	}
	return nil
}

// ValidCommandsWith 返回可执行命令列表。
func ValidCommandsWith(saved *RulesConfig, text string) ([]string, error) {
	if err := ValidateScriptWith(saved, text); err != nil {
		return nil, err
	}
	return ParseScriptWith(saved, text).Valid, nil
}

// NormalizeSaved 规范化待写入配置。
func NormalizeSaved(cfg RulesConfig) RulesConfig {
	cfg.Blacklist = normalizeUpperList(cfg.Blacklist)
	seen := map[string]struct{}{}
	var cmds []CmdOverride
	for _, c := range cfg.Commands {
		n := strings.ToUpper(strings.TrimSpace(c.Name))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		c.Name = n
		if c.Enabled == nil {
			c.Enabled = boolPtr(true)
		}
		if c.MinArgs != nil && *c.MinArgs < 0 {
			c.MinArgs = intPtr(0)
		}
		cmds = append(cmds, c)
	}
	cfg.Commands = cmds
	return cfg
}

func normalizeUpperList(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, x := range in {
		x = strings.ToUpper(strings.TrimSpace(x))
		if x == "" {
			continue
		}
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	sortStrings(out)
	return out
}

func boolPtr(v bool) *bool { return &v }
func intPtr(v int) *int    { return &v }

func sortStrings(a []string) {
	for i := 0; i < len(a); i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

// FormatMinArgs 便于规则页展示。
func FormatMinArgs(n int) string {
	return strconv.Itoa(n)
}
