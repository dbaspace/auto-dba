package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"auto-dba/internal/rediscmd"
)

const (
	GlobalArgRedisCommandRules = "ticket_redis_command_rules"
	descRedisCommandRules      = "Redis 工单命令规则：JSON {blacklist:[], commands:[{name,enabled,min_args}]}"
)

// GetRedisCommandRules 读取已保存的 Redis 命令规则（可能为部分覆盖）；校验用请再 MergeRules。
func (d *DB) GetRedisCommandRules() (*rediscmd.RulesConfig, error) {
	if err := d.EnsureRedisCommandRules(); err != nil {
		return nil, err
	}
	raw, err := d.GetGlobalArg(GlobalArgRedisCommandRules)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		def := rediscmd.DefaultRulesConfig()
		return &def, nil
	}
	var cfg rediscmd.RulesConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("解析 Redis 命令规则失败: %w", err)
	}
	return &cfg, nil
}

// GetRedisCommandRulesMerged 返回与内置目录合并后的完整规则。
func (d *DB) GetRedisCommandRulesMerged() (rediscmd.RulesConfig, error) {
	saved, err := d.GetRedisCommandRules()
	if err != nil {
		return rediscmd.RulesConfig{}, err
	}
	return rediscmd.MergeRules(saved), nil
}

// SaveRedisCommandRules 保存 Redis 命令规则（规范化后整存）。
func (d *DB) SaveRedisCommandRules(cfg rediscmd.RulesConfig) error {
	cfg = rediscmd.NormalizeSaved(cfg)
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return d.upsertGlobalArgValue(GlobalArgRedisCommandRules, string(b), descRedisCommandRules)
}

// EnsureRedisCommandRules 保证 GlobalArg 存在，并补齐内置目录中新增的命令项。
func (d *DB) EnsureRedisCommandRules() error {
	if err := d.ensureGlobalArgJSON(GlobalArgRedisCommandRules, descRedisCommandRules, func() (string, error) {
		b, err := json.Marshal(rediscmd.DefaultRulesConfig())
		return string(b), err
	}); err != nil {
		return err
	}
	return d.mergeMissingRedisCommandRules()
}

func (d *DB) mergeMissingRedisCommandRules() error {
	raw, err := d.GetGlobalArg(GlobalArgRedisCommandRules)
	if err != nil {
		return err
	}
	var saved rediscmd.RulesConfig
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &saved); err != nil {
			return nil // 坏数据留给 Get 报错；不阻断启动
		}
	}
	merged := rediscmd.MergeRules(&saved)
	// 若 commands 数量少于内置，写回 merged（保留用户 enabled/min_args）
	def := rediscmd.DefaultRulesConfig()
	if len(saved.Commands) >= len(def.Commands) && saved.Blacklist != nil {
		return nil
	}
	b, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	return d.upsertGlobalArgValue(GlobalArgRedisCommandRules, string(b), descRedisCommandRules)
}
