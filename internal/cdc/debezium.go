package cdc

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// 对齐 hub：after_only / before_after 两套 Debezium PostgresConnector 配置模版。
//
// after_only：ExtractNewRecordState unwrap，add.fields=op,source.db,source.table,source.ts_ms。
// before_after：保留完整 envelope，无 unwrap；可选 Filter SMT（value.op != 'd'）。

const (
	ModeAfterOnly   = "after_only"
	ModeBeforeAfter = "before_after"
	SnapshotNever   = "never"
	MaskedPassword  = "***"

	debeziumTransformByLogicalTableRouter = "io.debezium.transforms.ByLogicalTableRouter"
)

// DebeziumBuildParams Debezium 配置构建入参。
//
// Tables 是非分表的单源 → 单 topic 表名；ShardGroups 是多源 → 单 logical topic 的分表组。
type DebeziumBuildParams struct {
	ConnectorName          string
	PGHost                 string
	PGPort                 int
	PGUser                 string
	PGPassword             string
	PGDbName               string
	Schema                 string
	Tables                 []string
	ShardGroups            []ShardGroup
	PublicationName        string
	SlotName               string
	TopicPrefix            string
	SnapshotMode           string
	TopicPartitions        int
	TopicReplicationFactor int
	FilterDeleteEvents     bool
}

// ShardGroup Debezium ByLogicalTableRouter：把 N 个源 topic 合并为 1 个 logical topic。
type ShardGroup struct {
	LogicalName string
	Regex       string
}

var sourceTableShardSuffixRE = regexp.MustCompile(`^(.+?)(_[0-9]+|[0-9]+)$`)

func BuildConfigAfterOnly(p DebeziumBuildParams) map[string]string {
	partitions := p.TopicPartitions
	if partitions <= 0 {
		partitions = 3
	}
	replicationFactor := p.TopicReplicationFactor
	if replicationFactor <= 0 {
		replicationFactor = 1
	}
	cfg := map[string]string{
		"connector.class":                           "io.debezium.connector.postgresql.PostgresConnector",
		"database.hostname":                         p.PGHost,
		"database.port":                             fmt.Sprintf("%d", p.PGPort),
		"database.user":                             p.PGUser,
		"database.password":                         p.PGPassword,
		"database.dbname":                           p.PGDbName,
		"table.include.list":                        buildTableIncludeListWithShards(p.Schema, p.Tables, p.ShardGroups),
		"topic.prefix":                              p.TopicPrefix,
		"snapshot.mode":                             defaultStr(p.SnapshotMode, SnapshotNever),
		"topic.creation.default.partitions":         fmt.Sprintf("%d", partitions),
		"topic.creation.default.replication.factor": fmt.Sprintf("%d", replicationFactor),
		"signal.data.collection":                    "public.debezium_signal",
		"plugin.name":                               "pgoutput",
		"publication.name":                          p.PublicationName,
		"publication.autocreate.mode":               "disabled",
		"slot.name":                                 p.SlotName,
		"time.precision.mode":                       "connect",
		"decimal.handling.mode":                     "string",
		"transforms.unwrap.type":                    "io.debezium.transforms.ExtractNewRecordState",
		"transforms.unwrap.delete.handling.mode":    "rewrite",
		"transforms.unwrap.add.fields":              "op,source.db,source.table,source.ts_ms",
	}
	applyShardRouterTransforms(cfg, p.ShardGroups)
	return cfg
}

func shardTableRegexPart(logicalTable string) string {
	base := strings.TrimSpace(logicalTable)
	if base == "" {
		return ""
	}
	return base + "(_[0-9]+|[0-9]+)"
}

func inferSourceTableStem(tableName string) string {
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		return ""
	}
	if m := sourceTableShardSuffixRE.FindStringSubmatch(tableName); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return tableName
}

func inferSourceRegexStem(sourceTables []string) string {
	tables := cleanStringSlice(sourceTables)
	if len(tables) == 0 {
		return ""
	}
	stems := make([]string, 0, len(tables))
	for _, t := range tables {
		stems = append(stems, inferSourceTableStem(t))
	}
	first := stems[0]
	allSame := true
	for _, s := range stems[1:] {
		if s != first {
			allSame = false
			break
		}
	}
	if allSame {
		return first
	}
	return longestCommonPrefix(stems)
}

func longestCommonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, v := range values[1:] {
		for len(prefix) > 0 && !strings.HasPrefix(v, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
		if prefix == "" {
			break
		}
	}
	return prefix
}

func debeziumShardRegexFromPattern(pattern string) string {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return ""
	}
	if strings.ContainsAny(p, `()[]{}|\+`) {
		return p
	}
	if t := strings.TrimRight(p, "*"); t != p && !strings.ContainsAny(t, "*?") {
		return shardTableRegexPart(strings.TrimRight(t, "_"))
	}
	if strings.ContainsAny(p, "*?") {
		var b strings.Builder
		for i := 0; i < len(p); i++ {
			switch p[i] {
			case '*':
				b.WriteString(".*")
			case '?':
				b.WriteByte('.')
			default:
				b.WriteString(regexp.QuoteMeta(string(p[i])))
			}
		}
		return b.String()
	}
	return shardTableRegexPart(p)
}

func deriveShardRegexPart(targetTable, fePattern string, sourceTables []string) string {
	if p := strings.TrimSpace(fePattern); p != "" {
		if regex := debeziumShardRegexFromPattern(p); regex != "" {
			if _, err := regexp.Compile(regex); err == nil {
				return regex
			}
		}
	}
	stem := inferSourceRegexStem(sourceTables)
	if stem == "" {
		stem = strings.TrimSpace(targetTable)
	}
	return shardTableRegexPart(stem)
}

func buildTableIncludeListWithShards(schema string, plainTables []string, shards []ShardGroup) string {
	s := strings.TrimSpace(schema)
	if s == "" {
		s = "public"
	}
	parts := make([]string, 0, len(plainTables)+len(shards))
	for _, t := range plainTables {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		parts = append(parts, s+"."+t)
	}
	for _, sg := range shards {
		r := strings.TrimSpace(sg.Regex)
		if r == "" {
			continue
		}
		parts = append(parts, s+"."+r)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func applyShardRouterTransforms(cfg map[string]string, shards []ShardGroup) {
	if cfg == nil {
		return
	}
	routerNames := make([]string, 0, len(shards))
	for _, sg := range shards {
		name := sanitizeIdent(sg.LogicalName)
		regex := strings.TrimSpace(sg.Regex)
		if name == "" || regex == "" {
			continue
		}
		routerNames = append(routerNames, name)
		cfg["transforms."+name+".type"] = debeziumTransformByLogicalTableRouter
		cfg["transforms."+name+".topic.regex"] = "(.*)" + regex
		cfg["transforms."+name+".topic.replacement"] = "$1" + sg.LogicalName
		cfg["transforms."+name+".key.field.name"] = "source_table"
	}
	hasUnwrap := strings.TrimSpace(cfg["transforms.unwrap.type"]) != ""
	cfg["transforms"] = finalizeDebeziumTransformsChain(
		strings.TrimSpace(cfg["transforms"]),
		routerNames,
		hasUnwrap,
	)
	pruneEmptyTransformsKey(cfg)
}

func finalizeDebeziumTransformsChain(prefix string, routers []string, hasUnwrap bool) string {
	parts := splitTransformsChain(prefix)
	filtered := make([]string, 0, len(parts))
	routerSet := make(map[string]struct{}, len(routers))
	for _, r := range routers {
		routerSet[r] = struct{}{}
	}
	for _, p := range parts {
		if p == "" || p == "unwrap" {
			continue
		}
		if _, isRouter := routerSet[p]; isRouter {
			continue
		}
		filtered = append(filtered, p)
	}
	out := append(filtered, routers...)
	if hasUnwrap {
		out = append(out, "unwrap")
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, ",")
}

func splitTransformsChain(chain string) []string {
	if chain == "" {
		return nil
	}
	raw := strings.Split(chain, ",")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func routerNamesFromConnectorConfig(cfg map[string]string) []string {
	if cfg == nil {
		return nil
	}
	const prefix = "transforms."
	const suffix = ".type"
	names := make([]string, 0)
	for k, v := range cfg {
		if !strings.HasPrefix(k, prefix) || !strings.HasSuffix(k, suffix) {
			continue
		}
		if strings.TrimSpace(v) != debeziumTransformByLogicalTableRouter {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(k, prefix), suffix)
		if name == "" || name == "filter" || name == "unwrap" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeConnectorTransformsChain(cfg map[string]string) {
	if cfg == nil {
		return
	}
	hasFilter := strings.TrimSpace(cfg["transforms.filter.type"]) != ""
	hasUnwrap := strings.TrimSpace(cfg["transforms.unwrap.type"]) != ""
	routers := routerNamesFromConnectorConfig(cfg)
	prefix := strings.TrimSpace(cfg["transforms"])
	if hasFilter {
		hasFilterInChain := false
		for _, p := range splitTransformsChain(prefix) {
			if p == "filter" {
				hasFilterInChain = true
				break
			}
		}
		if !hasFilterInChain {
			if prefix == "" {
				prefix = "filter"
			} else {
				prefix = "filter," + prefix
			}
		}
	}
	cfg["transforms"] = finalizeDebeziumTransformsChain(prefix, routers, hasUnwrap)
	pruneEmptyTransformsKey(cfg)
}

func validateConnectorTransformsOrder(cfg map[string]string) error {
	if cfg == nil {
		return nil
	}
	chain := splitTransformsChain(cfg["transforms"])
	filterIdx, unwrapIdx := -1, -1
	for i, name := range chain {
		switch name {
		case "filter":
			filterIdx = i
		case "unwrap":
			unwrapIdx = i
		}
	}
	if filterIdx < 0 || unwrapIdx < 0 {
		return nil
	}
	if filterIdx > unwrapIdx {
		return fmt.Errorf(
			"transforms order invalid: filter must be before unwrap (expected filter,{routers},unwrap, got %s)",
			cfg["transforms"],
		)
	}
	return nil
}

func validateConnectorTransformsDefined(cfg map[string]string) error {
	if cfg == nil {
		return nil
	}
	chain := splitTransformsChain(cfg["transforms"])
	if len(chain) == 0 {
		return nil
	}
	missing := make([]string, 0)
	for _, name := range chain {
		key := "transforms." + name + ".type"
		if strings.TrimSpace(cfg[key]) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if len(missing) == 1 {
		name := missing[0]
		return fmt.Errorf(
			`transform "%s" 缺少必填配置 transforms.%s.type（请在配置表中添加对应 type 及子项后再保存）`,
			name, name,
		)
	}
	return fmt.Errorf(
		"transforms 链中的 %s 缺少必填配置 transforms.{name}.type（请在配置表中添加对应 type 及子项后再保存）",
		strings.Join(missing, ", "),
	)
}

func ValidateAndNormalizeConnectorCfg(cfg map[string]string) error {
	return validateAndNormalizeConnectorCfg(cfg)
}

func validateAndNormalizeConnectorCfg(cfg map[string]string) error {
	normalizeConnectorTransformsChain(cfg)
	if err := validateConnectorTransformsOrder(cfg); err != nil {
		return err
	}
	return validateConnectorTransformsDefined(cfg)
}
