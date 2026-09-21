package dbprobe

import "strings"

func normType(dbType string) string {
	t := strings.ToLower(strings.TrimSpace(dbType))
	switch t {
	case "postgresql", "pg", "pgsql":
		return "postgres"
	case "cockroach", "cockroadchdb":
		return "cockroachdb"
	case "ob":
		return "oceanbase"
	case "mssql", "sql-server", "sql_server":
		return "sqlserver"
	case "es", "elastic":
		return "elasticsearch"
	case "mongo":
		return "mongodb"
	default:
		return t
	}
}

func family(dbType string) string {
	switch normType(dbType) {
	case "postgres", "cockroachdb":
		return "postgres"
	case "mysql", "tidb", "starrocks", "oceanbase":
		return "mysql"
	case "clickhouse":
		return "clickhouse"
	case "sqlserver":
		return "sqlserver"
	case "oracle":
		return "oracle"
	case "mongodb":
		return "mongodb"
	case "elasticsearch":
		return "elasticsearch"
	case "cassandra":
		return "cassandra"
	case "kafka":
		return "kafka"
	case "redis":
		return "redis"
	default:
		return normType(dbType)
	}
}

func pgConnectDB(dbType string) string {
	if normType(dbType) == "cockroachdb" {
		return "defaultdb"
	}
	return "postgres"
}
