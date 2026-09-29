package dbmonitor

import "strings"

// NormalizeEngine 归一化引擎名。
func NormalizeEngine(t string) string {
	n := strings.ToLower(strings.TrimSpace(t))
	switch n {
	case "pg", "postgresql", "postgres":
		return "postgres"
	case "mysql", "mariadb":
		return "mysql"
	case "tidb":
		return "tidb"
	case "redis":
		return "redis"
	case "starrocks", "sr":
		return "starrocks"
	case "clickhouse", "ch":
		return "clickhouse"
	case "kafka":
		return "kafka"
	case "mongo", "mongodb":
		return "mongodb"
	case "oracle":
		return "oracle"
	case "es", "elastic", "elasticsearch":
		return "elasticsearch"
	case "mssql", "sqlserver", "sql_server":
		return "sqlserver"
	case "cockroach", "cockroachdb", "crdb":
		return "cockroachdb"
	case "cassandra":
		return "cassandra"
	case "oceanbase", "ob":
		return "oceanbase"
	case "host", "node", "server":
		return "host"
	case "debezium", "cdc":
		return "debezium"
	case "dolphin", "ds", "dolphinscheduler", "dolphin_scheduler":
		return "dolphinscheduler"
	default:
		return n
	}
}

// EnvQueryEngine 是否支持仅凭 idc/domain 查询（无需实例表）。
func EnvQueryEngine(engine string) bool {
	switch NormalizeEngine(engine) {
	case "debezium", "dolphinscheduler", "starrocks":
		return true
	default:
		return false
	}
}

// Catalog 返回指定引擎看板目录。
func Catalog(engine string) []SectionDef {
	return sectionsFor(NormalizeEngine(engine))
}

func sectionsFor(engine string) []SectionDef {
	switch engine {
	case "mysql":
		return mysqlSections
	case "redis":
		return redisSections
	case "starrocks":
		return starrocksSections
	case "clickhouse":
		return clickhouseSections
	case "kafka":
		return kafkaSections
	case "mongodb":
		return mongodbSections
	case "oracle":
		return oracleSections
	case "elasticsearch":
		return elasticsearchSections
	case "sqlserver":
		return sqlserverSections
	case "tidb":
		return tidbSections
	case "cockroachdb":
		return cockroachSections
	case "cassandra":
		return cassandraSections
	case "oceanbase":
		return oceanbaseSections
	case "host":
		return hostSections
	case "debezium":
		return debeziumSections
	case "dolphinscheduler":
		return dolphinSections
	default:
		return pgSections
	}
}

func findPanel(engine, key string) *PanelDef {
	secs := sectionsFor(engine)
	for i := range secs {
		for j := range secs[i].Panels {
			p := &secs[i].Panels[j]
			if p.Key == key {
				return p
			}
		}
	}
	return nil
}

func panelsBySection(engine, section string) []PanelDef {
	section = strings.TrimSpace(section)
	var out []PanelDef
	for _, s := range sectionsFor(engine) {
		if section != "" && s.Name != section {
			continue
		}
		out = append(out, s.Panels...)
	}
	return out
}
