package dbdeploy

import (
	"fmt"
	"path/filepath"
	"strings"

	"auto-dba/internal/store"
)

func renderClickHouseConf(cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, soft, data string, port int) (string, string) {
	httpPort := port + 1200 // 9000 -> 10200 typical alternate; keep 8123 default for http
	if port == 9000 {
		httpPort = 8123
	}
	replica := ""
	if n.Role == "replica" && primary != nil {
		replica = fmt.Sprintf("\n<!-- remote replica of %s:%d -->\n", primary.Host, nodePort(cl, primary))
	}
	body := fmt.Sprintf(`<?xml version="1.0"?>
<clickhouse>
  <logger><level>information</level><log>%s/log/clickhouse.log</log></logger>
  <path>%s</path>
  <tcp_port>%d</tcp_port>
  <http_port>%d</http_port>
  <listen_host>0.0.0.0</listen_host>%s
</clickhouse>
`, data, data, port, httpPort, replica)
	return filepath.ToSlash(filepath.Join(soft, "config.xml")), body
}

func renderMongoConf(cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, soft, data string, port int) (string, string) {
	repl := ""
	if cl.Topology == TopoReplset {
		repl = "replication:\n  replSetName: rs0\n"
	}
	_ = primary
	_ = n
	body := fmt.Sprintf(`storage:
  dbPath: %s
net:
  port: %d
  bindIp: 0.0.0.0
%s`, data, port, repl)
	return filepath.ToSlash(filepath.Join(soft, "mongod.conf")), body
}

func renderCassandraConf(cl *store.DbDeployCluster, n *store.DbDeployNode, soft, data string, port int) (string, string) {
	seeds := n.Host
	if cl.Topology == TopoMultiNode {
		// seed 角色优先；否则本机
		// 完整 seed 列表由部署方在扩容时调整；首节点写自身
	}
	body := fmt.Sprintf("cluster_name: '%s'\nlisten_address: %s\nrpc_address: 0.0.0.0\nnative_transport_port: %d\ndata_file_directories:\n    - %s\nseed_provider:\n    - class_name: org.apache.cassandra.locator.SimpleSeedProvider\n      parameters:\n          - seeds: \"%s\"\n",
		cl.Name, n.Host, port, data, seeds)
	return filepath.ToSlash(filepath.Join(soft, "cassandra.yaml")), body
}

func renderESConf(cl *store.DbDeployCluster, n *store.DbDeployNode, all []store.DbDeployNode, soft, data string, port int) (string, string) {
	roles := "node.roles: [ master, data ]\n"
	switch n.Role {
	case "master":
		roles = "node.roles: [ master ]\n"
	case "data":
		roles = "node.roles: [ data ]\n"
	}
	seeds := []string{}
	for _, x := range all {
		if x.Role == "master" || x.Role == "single" {
			seeds = append(seeds, fmt.Sprintf("\"%s\"", x.Host))
		}
	}
	if len(seeds) == 0 {
		seeds = append(seeds, fmt.Sprintf("\"%s\"", n.Host))
	}
	body := fmt.Sprintf("cluster.name: %s\nnode.name: %s\npath.data: %s\nnetwork.host: 0.0.0.0\nhttp.port: %d\n%sdiscovery.seed_hosts: [%s]\ncluster.initial_master_nodes: [%s]\n",
		cl.Name, n.Host, data, port, roles, strings.Join(seeds, ", "), strings.Join(seeds, ", "))
	return filepath.ToSlash(filepath.Join(soft, "elasticsearch.yml")), body
}

func renderKafkaConf(cl *store.DbDeployCluster, n *store.DbDeployNode, all []store.DbDeployNode, soft, data string, port int) (string, string) {
	id := serverID(n.Host, port) % 1000
	if id < 1 {
		id = 1
	}
	voters := []string{}
	for i, x := range all {
		vid := serverID(x.Host, nodePort(cl, &all[i])) % 1000
		if vid < 1 {
			vid = i + 1
		}
		voters = append(voters, fmt.Sprintf("%d@%s:%d", vid, x.Host, nodePort(cl, &all[i])+1))
	}
	if len(voters) == 0 {
		voters = append(voters, fmt.Sprintf("%d@%s:%d", id, n.Host, port+1))
	}
	body := fmt.Sprintf(`process.roles=broker,controller
node.id=%d
controller.quorum.voters=%s
listeners=PLAINTEXT://0.0.0.0:%d,CONTROLLER://0.0.0.0:%d
advertised.listeners=PLAINTEXT://%s:%d
controller.listener.names=CONTROLLER
inter.broker.listener.name=PLAINTEXT
log.dirs=%s
`, id, strings.Join(voters, ","), port, port+1, n.Host, port, data)
	_ = soft
	return filepath.ToSlash(filepath.Join(soft, "server.properties")), body
}
