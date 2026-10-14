// Package agentport 约束 Agent SSH 下发监听端口，避免误杀已运行业务服务。
package agentport

import "fmt"

// 约定 Agent 端口带：平台 8090 / Node 8091 / DB 8092；仅该带允许 fuser -k。
const (
	FuserMin = 8090
	FuserMax = 8199
)

// 常见业务/数据库/中间件端口：禁止用作 Agent 监听，防止停旧 Agent 时误杀。
var blockedPorts = map[int]string{
	22:    "SSH",
	80:    "HTTP",
	443:   "HTTPS",
	1433:  "SQL Server",
	1521:  "Oracle",
	2181:  "ZooKeeper",
	2379:  "etcd",
	2380:  "etcd-peer",
	2881:  "OceanBase",
	3306:  "MySQL",
	4000:  "TiDB",
	5432:  "PostgreSQL",
	6379:  "Redis",
	6443:  "kube-apiserver",
	8030:  "StarRocks FE HTTP",
	8040:  "StarRocks BE HTTP",
	8060:  "StarRocks FE RPC",
	8080:  "常见 HTTP 应用",
	8123:  "ClickHouse",
	9000:  "常见 HTTP/MinIO",
	9010:  "StarRocks BE thrift",
	9020:  "StarRocks FE edit log",
	9030:  "StarRocks FE query",
	9042:  "Cassandra",
	9050:  "StarRocks BE heartbeat",
	9092:  "Kafka",
	9200:  "Elasticsearch",
	10250: "kubelet",
	26257: "CockroachDB",
	27017: "MongoDB",
}

// ValidateListenPort 校验 Agent 监听端口：须为合法高位端口，且不得占用已知业务端口。
func ValidateListenPort(port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("Agent 监听端口须在 1024–65535（当前 %d）", port)
	}
	if name, ok := blockedPorts[port]; ok {
		return fmt.Errorf("端口 %d 为 %s，禁止用作 Agent 监听（避免下发时误杀已运行服务）", port, name)
	}
	return nil
}

// AllowFuserKill 仅对约定 Agent 端口带允许 fuser -k，防止误配端口时杀 FE/BE/DB。
func AllowFuserKill(port int) bool {
	return port >= FuserMin && port <= FuserMax && ValidateListenPort(port) == nil
}

// FuserKillSnippet 返回可嵌入远端 shell 的 fuser 片段；不允许时返回说明性 no-op。
func FuserKillSnippet(port int) string {
	if !AllowFuserKill(port) {
		return `echo "skip fuser (port outside agent band ` + fmt.Sprintf("%d-%d", FuserMin, FuserMax) + `)"`
	}
	return `if [ "$PORT" -gt 0 ] 2>/dev/null && command -v fuser >/dev/null 2>&1; then
  fuser -k "${PORT}/tcp" 2>/dev/null || true
  sleep 0.5
fi`
}
