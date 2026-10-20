package cdc

import (
	"fmt"
	"strings"
)

type TargetSQLParams struct {
	TargetDB           string
	KeyModel           string
	Buckets            int
	ReplicationNum     int
	RoutineLoadConc    int
	KafkaBrokers       string
	TopicPrefix        string
	Env                string
	SourceDB           string
	SourceSchema       string
	DebeziumMode       string
	MessageSchemaMode  string
	AutoCreate         bool
	DynamicPartitionOn bool
	TaskPartitionCol   string
	TaskPartitionProps map[string]string
	GlobalPartition    map[string]string
	Mappings           []TableMapping
	FallbackTables     []string
	TaskType           string
	SQLOverrides       []SQLOverride
	KafkaDefaultOffset string
}

type TargetSQLItem struct {
	Source         string `json:"source"`
	Table          string `json:"table"`
	DDL            string `json:"ddl"`
	Load           string `json:"load"`
	Skipped        bool   `json:"skipped"`
	Reason         string `json:"reason,omitempty"`
	DDLCustomized  bool   `json:"ddl_customized,omitempty"`
	LoadCustomized bool   `json:"load_customized,omitempty"`
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// NormalizeKafkaBootstrap 去掉 host:port:port 这种重复端口（实例 host 已带端口后又拼了 port）。
func NormalizeKafkaBootstrap(s string) string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "[") {
			out = append(out, p)
			continue
		}
		if i := strings.LastIndex(p, ":"); i > 0 {
			last := p[i+1:]
			rest := p[:i]
			if j := strings.LastIndex(rest, ":"); j > 0 {
				mid := rest[j+1:]
				if mid == last && isDigits(mid) {
					p = rest
				}
			}
		}
		out = append(out, p)
	}
	return strings.Join(out, ",")
}

func ResolveKafkaBootstrap(override, connectorHost, cfgBrokers string) string {
	if s := NormalizeKafkaBootstrap(strings.TrimSpace(override)); s != "" {
		return s
	}
	if s := NormalizeKafkaBootstrap(strings.TrimSpace(connectorHost)); s != "" {
		return s
	}
	return NormalizeKafkaBootstrap(strings.TrimSpace(cfgBrokers))
}

func BuildTargetSQLItems(p TargetSQLParams, colsByTable map[string][]pgCol, pksByTable map[string][]string) []TargetSQLItem {
	maps := p.Mappings
	if len(maps) == 0 {
		for _, t := range p.FallbackTables {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			maps = append(maps, TableMapping{SourceTables: []string{t}, TargetTable: t})
		}
	}
	seen := map[string]bool{}
	out := make([]TargetSQLItem, 0, len(maps))
	schema := defaultStr(p.SourceSchema, "public")
	prefix := defaultStr(p.TopicPrefix, "cdc")
	buckets := p.Buckets
	if buckets <= 0 {
		buckets = 10
	}
	repl := p.ReplicationNum
	if repl <= 0 {
		repl = 1
	}
	conc := p.RoutineLoadConc
	if conc <= 0 {
		conc = 3
	}
	keyClause := SRKeyClause(p.KeyModel)
	db := defaultStr(p.TargetDB, "target_db")
	for _, m := range maps {
		if strings.EqualFold(strings.TrimSpace(m.Status), "deleted") {
			continue
		}
		src := cleanStringSlice(m.SourceTables)
		tgt := strings.TrimSpace(m.TargetTable)
		if tgt == "" && len(src) > 0 {
			tgt = src[0]
		}
		if tgt == "" || seen[tgt] {
			continue
		}
		seen[tgt] = true
		srcName := src[0]
		if srcName == "" {
			srcName = tgt
		}
		item := TargetSQLItem{Source: strings.Join(src, ", "), Table: tgt}
		if !p.AutoCreate {
			item.Skipped = true
			item.Reason = "手动 DDL：跳过自动建表"
			out = append(out, item)
			continue
		}
		cols := colsByTable[srcName]
		pks := pksByTable[srcName]
		keys := cleanStringSlice(m.KeyColumns)
		if len(keys) == 0 {
			keys = pks
		}
		if len(keys) == 0 && len(cols) > 0 {
			keys = []string{cols[0].Name}
		}
		shard := cleanStringSlice(m.ShardKeys)
		if len(shard) == 0 {
			shard = keys
		}
		partCol := ""
		var dp map[string]string
		if p.DynamicPartitionOn {
			partCol = EffectivePartitionColumn(p.TaskPartitionCol, m.PartitionColumn)
			if partCol != "" {
				dp = MergeStringMaps(DefaultSRDynamicPartitionProps, p.GlobalPartition, p.TaskPartitionProps, m.SrDynamicPartition)
			}
		}
		item.DDL = buildCreateTableSQL(db, tgt, cols, keys, shard, keyClause, buckets, repl, partCol, dp)
		topics := CollectMappingKafkaTopics(prefix, schema, m)
		if len(topics) == 0 {
			topics = []string{prefix + "." + schema + "." + tgt}
		}
		offset := KafkaOffsetForMapping(p.TaskType, m)
		if strings.TrimSpace(p.KafkaDefaultOffset) != "" {
			offset = NormalizeKafkaOffset(p.KafkaDefaultOffset)
		}
		rlName := RoutineLoadNameFor(p.Env, p.SourceDB, prefix, tgt)
		item.Load = buildRoutineLoadSQL(db, tgt, rlName, keys, conc, p.KafkaBrokers, topics, p.DebeziumMode, offset, p.MessageSchemaMode)
		out = append(out, item)
	}
	return ApplySQLOverrides(out, p.SQLOverrides)
}

func JoinTargetSQL(items []TargetSQLItem) string {
	var b strings.Builder
	for _, it := range items {
		if it.Skipped {
			continue
		}
		if it.DDL != "" {
			b.WriteString(it.DDL)
			if !strings.HasSuffix(strings.TrimSpace(it.DDL), ";") {
				b.WriteString(";")
			}
			b.WriteString("\n")
		}
		if it.Load != "" {
			b.WriteString(it.Load)
			if !strings.HasSuffix(strings.TrimSpace(it.Load), ";") {
				b.WriteString(";")
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func buildCreateTableSQL(db, table string, cols []pgCol, keys, shard []string, keyClause string, buckets, repl int, partCol string, dp map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS `%s`.`%s` (\n", db, table)
	if len(cols) == 0 {
		for i, k := range keys {
			if i > 0 {
				b.WriteString(",\n")
			}
			fmt.Fprintf(&b, "  `%s` BIGINT NOT NULL", k)
		}
		if len(keys) == 0 {
			b.WriteString("  `id` BIGINT NOT NULL")
			keys = []string{"id"}
		}
	} else {
		for i, c := range cols {
			if i > 0 {
				b.WriteString(",\n")
			}
			fmt.Fprintf(&b, "  `%s` %s", c.Name, mapSRType(c.UDT))
		}
	}
	if len(keys) > 0 {
		fmt.Fprintf(&b, ",\n  %s(`%s`)", keyClause, strings.Join(keys, "`,`"))
	}
	b.WriteString("\n) ENGINE=OLAP")
	if partCol != "" {
		fmt.Fprintf(&b, "\nPARTITION BY RANGE(`%s`) ()", partCol)
	}
	dist := shard
	if len(dist) == 0 {
		dist = keys
	}
	if len(dist) > 0 {
		fmt.Fprintf(&b, "\nDISTRIBUTED BY HASH(`%s`) BUCKETS %d", strings.Join(dist, "`,`"), buckets)
	}
	fmt.Fprintf(&b, "\nPROPERTIES (\n%s\n);", FormatSRTableProperties(repl, dp))
	return b.String()
}

func buildRoutineLoadSQL(db, table, rlName string, keys []string, conc int, brokers string, topics []string, mode, kafkaOffset, schemaMode string) string {
	cols := append(append([]string{}, keys...), "__op")
	broker := strings.TrimSpace(brokers)
	if broker == "" {
		broker = "kafka:9092"
	}
	offset := defaultStr(NormalizeKafkaOffset(kafkaOffset), "OFFSET_BEGINNING")
	rlName = strings.TrimSpace(rlName)
	if rlName == "" {
		rlName = RoutineLoadNameFor("", "", "", table)
	}
	props := []string{`"format" = "json"`, `"strip_outer_array" = "false"`, fmt.Sprintf(`"desired_concurrent_number" = "%d"`, conc)}
	if !IsWithoutSchema(schemaMode) || strings.TrimSpace(mode) == ModeBeforeAfter {
		paths := make([]string, 0, len(cols))
		for _, c := range cols {
			if c == "__op" {
				if strings.TrimSpace(mode) == ModeBeforeAfter {
					paths = append(paths, `"$.op"`)
				} else {
					paths = append(paths, `"$.__op"`)
				}
				continue
			}
			if strings.TrimSpace(mode) == ModeBeforeAfter {
				paths = append(paths, fmt.Sprintf(`"$.after.%s"`, c))
			} else {
				paths = append(paths, fmt.Sprintf(`"$.%s"`, c))
			}
		}
		props = append([]string{props[0], fmt.Sprintf(`"jsonpaths" = "[%s]"`, strings.Join(paths, ",")), props[1], props[2]})
	}
	return fmt.Sprintf(`CREATE ROUTINE LOAD `+"`%s`.`%s`"+` ON `+"`%s`"+`
COLUMNS(%s)
PROPERTIES (
  %s
)
FROM KAFKA (
  "kafka_broker_list" = "%s",
  "kafka_topic" = "%s",
  "property.group.id" = "%s",
  "property.kafka_default_offsets" = "%s"
);`, db, rlName, table, strings.Join(cols, ", "), strings.Join(props, ",\n  "), broker, strings.Join(topics, ","), rlName, offset)
}

func NormalizeMessageSchemaMode(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), "with_schema") {
		return "with_schema"
	}
	return "without_schema"
}

func IsWithoutSchema(s string) bool {
	return NormalizeMessageSchemaMode(s) == "without_schema"
}

func NormalizeKafkaOffset(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "OFFSET_END", "END":
		return "OFFSET_END"
	default:
		return "OFFSET_BEGINNING"
	}
}
