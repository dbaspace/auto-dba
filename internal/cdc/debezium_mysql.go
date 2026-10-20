package cdc

import (
	"fmt"
	"strings"
)

const (
	SourceEnginePostgres = "postgres"
	SourceEngineMySQL    = "mysql"
)

// NormalizeCDCSourceEngine 归一化 CDC 源引擎。
func NormalizeCDCSourceEngine(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "mysql", "tidb", "oceanbase", "mariadb":
		return SourceEngineMySQL
	case "postgres", "postgresql", "pg", "":
		return SourceEnginePostgres
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// BuildConfigMySQLAfterOnly Debezium MySQLConnector（after_only / unwrap）。
func BuildConfigMySQLAfterOnly(p DebeziumBuildParams) map[string]string {
	partitions := p.TopicPartitions
	if partitions <= 0 {
		partitions = 3
	}
	replicationFactor := p.TopicReplicationFactor
	if replicationFactor <= 0 {
		replicationFactor = 1
	}
	serverID := p.MySQLServerID
	if serverID <= 0 {
		serverID = 5400
	}
	dbName := strings.TrimSpace(p.PGDbName)
	include := buildMySQLTableIncludeList(dbName, p.Tables, p.ShardGroups)
	cfg := map[string]string{
		"connector.class":                           "io.debezium.connector.mysql.MySqlConnector",
		"database.hostname":                         p.PGHost,
		"database.port":                             fmt.Sprintf("%d", p.PGPort),
		"database.user":                             p.PGUser,
		"database.password":                         p.PGPassword,
		"database.server.id":                        fmt.Sprintf("%d", serverID),
		"topic.prefix":                              p.TopicPrefix,
		"table.include.list":                        include,
		"snapshot.mode":                             defaultStr(p.SnapshotMode, SnapshotNever),
		"topic.creation.default.partitions":         fmt.Sprintf("%d", partitions),
		"topic.creation.default.replication.factor": fmt.Sprintf("%d", replicationFactor),
		"schema.history.internal.kafka.topic":       p.TopicPrefix + ".schemahistory",
		"include.schema.changes":                    "false",
		"time.precision.mode":                       "connect",
		"decimal.handling.mode":                     "string",
		"transforms.unwrap.type":                    "io.debezium.transforms.ExtractNewRecordState",
		"transforms.unwrap.delete.handling.mode":    "rewrite",
		"transforms.unwrap.add.fields":              "op,source.db,source.table,source.ts_ms",
	}
	if dbName != "" {
		cfg["database.include.list"] = dbName
	}
	applyShardRouterTransforms(cfg, p.ShardGroups)
	return cfg
}

// BuildConfigMySQLBeforeAfter Debezium MySQLConnector（完整 envelope）。
func BuildConfigMySQLBeforeAfter(p DebeziumBuildParams) map[string]string {
	cfg := BuildConfigMySQLAfterOnly(p)
	delete(cfg, "transforms.unwrap.type")
	delete(cfg, "transforms.unwrap.delete.handling.mode")
	delete(cfg, "transforms.unwrap.add.fields")
	// 清理 unwrap 后可能留下的 transforms 键由 applyShardRouterTransforms 管理
	if p.FilterDeleteEvents {
		cfg["transforms"] = joinTransform(cfg["transforms"], "filter")
		cfg["transforms.filter.type"] = "io.debezium.transforms.Filter"
		cfg["transforms.filter.language"] = "jsr223.groovy"
		cfg["transforms.filter.condition"] = "value.op != 'd'"
	}
	return cfg
}

func buildMySQLTableIncludeList(db string, plainTables []string, shards []ShardGroup) string {
	db = strings.TrimSpace(db)
	var parts []string
	for _, t := range plainTables {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if strings.Contains(t, ".") {
			parts = append(parts, t)
			continue
		}
		if db != "" {
			parts = append(parts, db+"."+t)
		} else {
			parts = append(parts, t)
		}
	}
	for _, g := range shards {
		regex := strings.TrimSpace(g.Regex)
		if regex == "" {
			continue
		}
		if db != "" && !strings.Contains(regex, ".") {
			parts = append(parts, db+"."+regex)
		} else {
			parts = append(parts, regex)
		}
	}
	return strings.Join(parts, ",")
}

func joinTransform(existing, add string) string {
	existing = strings.TrimSpace(existing)
	add = strings.TrimSpace(add)
	if existing == "" {
		return add
	}
	if add == "" {
		return existing
	}
	for _, p := range strings.Split(existing, ",") {
		if strings.TrimSpace(p) == add {
			return existing
		}
	}
	return existing + "," + add
}
