package dbdeploy

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"

	"auto-dba/internal/store"
)

func softBase(cl *store.DbDeployCluster, n *store.DbDeployNode) string {
	base := strings.TrimSpace(cl.SoftwareBase)
	if base == "" {
		switch cl.Engine {
		case store.DbDeployEnginePostgres:
			base = "/usr/local/pgsql"
		case store.DbDeployEngineRedis, store.DbDeployEngineCodis:
			base = "/usr/local/redis"
		case "tidb":
			base = "/usr/local/tidb"
		case "cockroachdb":
			base = "/usr/local/cockroach"
		case "clickhouse":
			base = "/usr/local/clickhouse"
		case "mongodb":
			base = "/usr/local/mongodb"
		case "cassandra":
			base = "/usr/local/cassandra"
		case "elasticsearch":
			base = "/usr/local/elasticsearch"
		case "kafka":
			base = "/usr/local/kafka"
		case "oceanbase":
			base = "/usr/local/oceanbase"
		case "oracle":
			base = "/u01/app/oracle/product/19c/dbhome_1"
		case "sqlserver":
			base = "/opt/mssql"
		default:
			base = "/usr/local/mysql"
		}
	}
	_ = n
	return filepath.ToSlash(base)
}

func dataRoot(cl *store.DbDeployCluster) string {
	d := strings.TrimSpace(cl.DataRoot)
	if d != "" {
		return filepath.ToSlash(d)
	}
	switch cl.Engine {
	case store.DbDeployEnginePostgres:
		return "/data/pgsql"
	case store.DbDeployEngineRedis, store.DbDeployEngineCodis:
		return "/data/redis"
	case "tidb":
		return "/data/tidb"
	case "cockroachdb":
		return "/data/cockroach"
	case "clickhouse":
		return "/data/clickhouse"
	case "mongodb":
		return "/data/mongodb"
	case "cassandra":
		return "/data/cassandra"
	case "elasticsearch":
		return "/data/elasticsearch"
	case "kafka":
		return "/data/kafka"
	case "oceanbase":
		return "/data/oceanbase"
	case "oracle":
		return "/u01/oradata"
	case "sqlserver":
		return "/var/opt/mssql"
	default:
		return "/data/mysql"
	}
}

func nodePort(cl *store.DbDeployCluster, n *store.DbDeployNode) int {
	if n.Port > 0 {
		return n.Port
	}
	switch n.Role {
	case "sentinel":
		return 26379
	case "codis_dashboard":
		return 18080
	case "codis_proxy":
		return 19000
	case "pd":
		return 2379
	case "tikv":
		return 20160
	case "tidb":
		return 4000
	}
	switch cl.Engine {
	case store.DbDeployEnginePostgres:
		return 5432
	case store.DbDeployEngineRedis, store.DbDeployEngineCodis:
		return 6379
	case "tidb":
		return 4000
	case "cockroachdb":
		return 26257
	case "clickhouse":
		return 9000
	case "mongodb":
		return 27017
	case "cassandra":
		return 9042
	case "elasticsearch":
		return 9200
	case "kafka":
		return 9092
	case "oceanbase":
		return 2881
	case "oracle":
		return 1521
	case "sqlserver":
		return 1433
	default:
		return 3306
	}
}

func defaultUnit(cl *store.DbDeployCluster, n *store.DbDeployNode) string {
	if strings.TrimSpace(n.SystemdUnit) != "" {
		return n.SystemdUnit
	}
	eng := cl.Engine
	if eng == store.DbDeployEngineCodis {
		switch n.Role {
		case "codis_dashboard":
			return "codis-dashboard.service"
		case "codis_proxy":
			return "codis-proxy.service"
		default:
			return "codis-server.service"
		}
	}
	switch eng {
	case store.DbDeployEnginePostgres:
		return "postgresql.service"
	case store.DbDeployEngineRedis:
		if n.Role == "sentinel" {
			return "redis-sentinel.service"
		}
		return "redis.service"
	case "tidb":
		switch n.Role {
		case "pd":
			return "pd.service"
		case "tikv":
			return "tikv.service"
		default:
			return "tidb.service"
		}
	case "cockroachdb":
		return "cockroach.service"
	case "clickhouse":
		return "clickhouse-server.service"
	case "mongodb":
		return "mongod.service"
	case "cassandra":
		return "cassandra.service"
	case "elasticsearch":
		return "elasticsearch.service"
	case "kafka":
		return "kafka.service"
	case "oceanbase":
		return "oceanbase-observer.service"
	case "oracle":
		return "oracle-db.service"
	case "sqlserver":
		return "mssql-server.service"
	default:
		return "mysqld.service"
	}
}

func serverID(host string, port int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s:%d", host, port)))
	id := int(h.Sum32()%4000000000) + 1
	if id < 1 {
		id = 1
	}
	return id
}

func renderConf(cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, all []store.DbDeployNode) (confPath, content string) {
	soft := softBase(cl, n)
	data := dataRoot(cl)
	port := nodePort(cl, n)
	switch cl.Engine {
	case store.DbDeployEnginePostgres:
		return renderPGConf(cl, n, primary, soft, data, port)
	case store.DbDeployEngineRedis, store.DbDeployEngineCodis:
		return renderRedisConf(cl, n, primary, soft, data, port)
	case "tidb":
		return renderTiDBConf(cl, n, all, soft, data, port)
	case "cockroachdb":
		return renderCRDBConf(cl, n, primary, soft, data, port)
	case "clickhouse":
		return renderClickHouseConf(cl, n, primary, soft, data, port)
	case "mongodb":
		return renderMongoConf(cl, n, primary, soft, data, port)
	case "cassandra":
		return renderCassandraConf(cl, n, soft, data, port)
	case "elasticsearch":
		return renderESConf(cl, n, all, soft, data, port)
	case "kafka":
		return renderKafkaConf(cl, n, all, soft, data, port)
	case "oceanbase":
		return renderOceanBaseConf(cl, n, primary, soft, data, port)
	case "oracle":
		return renderOracleConf(cl, n, soft, data, port)
	case "sqlserver":
		return renderSQLServerConf(cl, n, primary, soft, data, port)
	default:
		return renderMySQLConf(cl, n, primary, soft, data, port)
	}
}

func renderMySQLConf(cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, soft, data string, port int) (string, string) {
	sid := serverID(n.Host, port)
	semi := ""
	if cl.Topology == store.DbDeployTopoMasterReplica {
		if n.Role == "primary" || n.Role == "single" {
			semi = "rpl_semi_sync_master_enabled=1\nrpl_semi_sync_master_timeout=10000\n"
		} else {
			semi = "rpl_semi_sync_slave_enabled=1\n"
			if primary != nil {
				semi += fmt.Sprintf("# replicate from %s:%d\n", primary.Host, nodePort(cl, primary))
			}
		}
	}
	readOnly := ""
	if n.Role == "replica" {
		readOnly = "read_only=1\nsuper_read_only=1\n"
	}
	body := fmt.Sprintf(`[mysqld]
basedir=%s
datadir=%s
port=%d
socket=%s/mysql.sock
server_id=%d
log_bin=mysql-bin
binlog_format=ROW
gtid_mode=ON
enforce_gtid_consistency=ON
log_slave_updates=ON
%sskip_name_resolve=ON
%s
`, soft, data, port, data, sid, semi, readOnly)
	return filepath.ToSlash(filepath.Join(soft, "my.cnf")), body
}

func renderPGConf(cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, soft, data string, port int) (string, string) {
	syncNames := ""
	if cl.Topology == store.DbDeployTopoMasterReplica && (n.Role == "primary" || n.Role == "single") {
		syncNames = "synchronous_standby_names = '*'\n"
	}
	standby := ""
	if n.Role == "replica" && primary != nil {
		standby = fmt.Sprintf(`primary_conninfo = 'host=%s port=%d user=replicator application_name=%s'
hot_standby = on
`, primary.Host, nodePort(cl, primary), n.Host)
	}
	body := fmt.Sprintf(`# auto-dba generated
listen_addresses = '*'
port = %d
data_directory = '%s'
max_wal_senders = 16
wal_level = replica
%s%s
`, port, data, syncNames, standby)
	_ = soft
	return filepath.ToSlash(filepath.Join(data, "postgresql.conf")), body
}

func renderRedisConf(cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, soft, data string, port int) (string, string) {
	if n.Role == "sentinel" {
		masterHost, masterPort := "127.0.0.1", 6379
		if primary != nil {
			masterHost = primary.Host
			masterPort = nodePort(cl, primary)
		}
		body := fmt.Sprintf(`port %d
dir %s
sentinel monitor mymaster %s %d 2
sentinel down-after-milliseconds mymaster 5000
sentinel failover-timeout mymaster 60000
`, port, data, masterHost, masterPort)
		return filepath.ToSlash(filepath.Join(soft, "sentinel.conf")), body
	}
	if n.Role == "codis_dashboard" {
		body := fmt.Sprintf("coordinator_name=filesystem\ncoordinator_addr=%s/codis\nproduct_name=%s\nadmin_addr=0.0.0.0:%d\n", data, cl.Name, port)
		return filepath.ToSlash(filepath.Join(soft, "dashboard.toml")), body
	}
	if n.Role == "codis_proxy" {
		body := fmt.Sprintf("product_name=%s\nadmin_addr=0.0.0.0:%d\nproxy_addr=0.0.0.0:%d\n", cl.Name, port+1, port)
		return filepath.ToSlash(filepath.Join(soft, "proxy.toml")), body
	}
	replicaof := ""
	if n.Role == "replica" && primary != nil {
		replicaof = fmt.Sprintf("replicaof %s %d\n", primary.Host, nodePort(cl, primary))
	}
	body := fmt.Sprintf(`port %d
dir %s
daemonize no
%sprotected-mode no
`, port, data, replicaof)
	return filepath.ToSlash(filepath.Join(soft, "redis.conf")), body
}

func renderTiDBConf(cl *store.DbDeployCluster, n *store.DbDeployNode, all []store.DbDeployNode, soft, data string, port int) (string, string) {
	switch n.Role {
	case "pd":
		peer := port + 1
		if peer == 2380 {
			// default client 2379 peer 2380
		} else if port == 2379 {
			peer = 2380
		}
		name := fmt.Sprintf("pd-%s", strings.ReplaceAll(n.Host, ".", "-"))
		body := fmt.Sprintf("name=%s\ndata-dir=%s/pd\nclient-urls=http://0.0.0.0:%d\npeer-urls=http://0.0.0.0:%d\nadvertise-client-urls=http://%s:%d\nadvertise-peer-urls=http://%s:%d\ninitial-cluster=%s=http://%s:%d\n",
			name, data, port, peer, n.Host, port, n.Host, peer, name, n.Host, peer)
		return filepath.ToSlash(filepath.Join(soft, "pd.toml")), body
	case "tikv":
		pd := pickRole(all, "pd")
		pdAddr := "127.0.0.1:2379"
		if pd != nil {
			pdAddr = fmt.Sprintf("%s:%d", pd.Host, nodePort(cl, pd))
		}
		body := fmt.Sprintf("[storage]\ndata-dir = \"%s/tikv\"\n\n[server]\naddr = \"0.0.0.0:%d\"\n\n[pd]\nendpoints = [\"%s\"]\n", data, port, pdAddr)
		return filepath.ToSlash(filepath.Join(soft, "tikv.toml")), body
	default: // tidb
		pd := pickRole(all, "pd")
		pdAddr := "127.0.0.1:2379"
		if pd != nil {
			pdAddr = fmt.Sprintf("%s:%d", pd.Host, nodePort(cl, pd))
		}
		body := fmt.Sprintf("host = \"0.0.0.0\"\nport = %d\nstore = \"tikv\"\npath = \"%s\"\n", port, pdAddr)
		_ = soft
		return filepath.ToSlash(filepath.Join(data, "tidb.toml")), body
	}
}

func renderCRDBConf(cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, soft, data string, port int) (string, string) {
	// cockroach 多用 flags；写一份 env 供 unit 引用
	join := ""
	if primary != nil && primary.ID != n.ID {
		join = fmt.Sprintf("%s:%d", primary.Host, nodePort(cl, primary))
	}
	body := fmt.Sprintf("COCKROACH_LISTEN_ADDR=0.0.0.0:%d\nCOCKROACH_HTTP_ADDR=0.0.0.0:%d\nCOCKROACH_STORE=%s\nCOCKROACH_JOIN=%s\n",
		port, port+1, data, join)
	_ = soft
	_ = cl
	return filepath.ToSlash(filepath.Join(data, "cockroach.env")), body
}

func renderUnit(cl *store.DbDeployCluster, n *store.DbDeployNode) (unitName, content string) {
	unit := defaultUnit(cl, n)
	soft := softBase(cl, n)
	data := dataRoot(cl)
	port := nodePort(cl, n)
	var execStart, execStop string
	switch cl.Engine {
	case store.DbDeployEnginePostgres:
		execStart = fmt.Sprintf("%s/bin/pg_ctl -D %s -l %s/logfile start", soft, data, data)
		execStop = fmt.Sprintf("%s/bin/pg_ctl -D %s stop -m fast", soft, data)
	case store.DbDeployEngineRedis, store.DbDeployEngineCodis:
		switch n.Role {
		case "sentinel":
			execStart = fmt.Sprintf("%s/bin/redis-sentinel %s/sentinel.conf", soft, soft)
		case "codis_dashboard":
			execStart = fmt.Sprintf("%s/bin/codis-dashboard -c %s/dashboard.toml", soft, soft)
		case "codis_proxy":
			execStart = fmt.Sprintf("%s/bin/codis-proxy -c %s/proxy.toml", soft, soft)
		default:
			execStart = fmt.Sprintf("%s/bin/redis-server %s/redis.conf", soft, soft)
		}
		execStop = "/bin/kill -TERM $MAINPID"
	case "tidb":
		switch n.Role {
		case "pd":
			execStart = fmt.Sprintf("%s/bin/pd-server --config=%s/pd.toml", soft, soft)
		case "tikv":
			execStart = fmt.Sprintf("%s/bin/tikv-server --config=%s/tikv.toml", soft, soft)
		default:
			execStart = fmt.Sprintf("%s/bin/tidb-server --config=%s/tidb.toml", soft, data)
		}
		execStop = "/bin/kill -TERM $MAINPID"
	case "cockroachdb":
		joinFlag := ""
		if n.Role == "node" {
			joinFlag = " --join=${COCKROACH_JOIN}"
		}
		execStart = fmt.Sprintf("/bin/bash -c 'set -a; source %s/cockroach.env; set +a; %s/bin/cockroach start --insecure --listen-addr=${COCKROACH_LISTEN_ADDR} --http-addr=${COCKROACH_HTTP_ADDR} --store=${COCKROACH_STORE}%s --background'", data, soft, joinFlag)
		if n.Role == "single" || (cl.Topology == store.DbDeployTopoStandalone) {
			execStart = fmt.Sprintf("/bin/bash -c 'set -a; source %s/cockroach.env; set +a; %s/bin/cockroach start-single-node --insecure --listen-addr=${COCKROACH_LISTEN_ADDR} --http-addr=${COCKROACH_HTTP_ADDR} --store=${COCKROACH_STORE} --background'", data, soft)
		}
		execStop = fmt.Sprintf("%s/bin/cockroach quit --insecure --host=localhost:%d", soft, port)
	case "clickhouse":
		execStart = fmt.Sprintf("%s/bin/clickhouse-server --config-file=%s/config.xml", soft, soft)
		execStop = "/bin/kill -TERM $MAINPID"
	case "mongodb":
		execStart = fmt.Sprintf("%s/bin/mongod --config %s/mongod.conf", soft, soft)
		execStop = "/bin/kill -TERM $MAINPID"
	case "cassandra":
		execStart = fmt.Sprintf("%s/bin/cassandra -f", soft)
		execStop = "/bin/kill -TERM $MAINPID"
	case "elasticsearch":
		execStart = fmt.Sprintf("%s/bin/elasticsearch -Epath.conf=%s", soft, soft)
		execStop = "/bin/kill -TERM $MAINPID"
	case "kafka":
		execStart = fmt.Sprintf("%s/bin/kafka-server-start.sh %s/server.properties", soft, soft)
		execStop = fmt.Sprintf("%s/bin/kafka-server-stop.sh", soft)
	case "oceanbase":
		execStart = fmt.Sprintf("/bin/bash -c 'set -a; source %s/oceanbase.env; set +a; if [ -x %s/bin/start.sh ]; then %s/bin/start.sh; elif [ -x %s/bin/observer ]; then cd %s && %s/bin/observer; else echo missing oceanbase start; exit 1; fi'", data, soft, soft, soft, soft, soft)
		execStop = "/bin/kill -TERM $MAINPID"
	case "oracle":
		execStart = fmt.Sprintf("/bin/bash -c 'set -a; source %s/oracle.env; set +a; if [ -x %s/bin/dbstart_wrapper.sh ]; then %s/bin/dbstart_wrapper.sh; else $ORACLE_HOME/bin/dbstart $ORACLE_HOME; fi'", data, soft, soft)
		execStop = fmt.Sprintf("/bin/bash -c 'set -a; source %s/oracle.env; set +a; $ORACLE_HOME/bin/dbshut $ORACLE_HOME'", data)
	case "sqlserver":
		execStart = fmt.Sprintf("/bin/bash -c 'export ACCEPT_EULA=Y; if [ -x %s/bin/sqlservr ]; then %s/bin/sqlservr; else /opt/mssql/bin/sqlservr; fi'", soft, soft)
		execStop = "/bin/kill -TERM $MAINPID"
	default:
		execStart = fmt.Sprintf("%s/bin/mysqld --defaults-file=%s/my.cnf", soft, soft)
		execStop = fmt.Sprintf("%s/bin/mysqladmin --defaults-file=%s/my.cnf shutdown", soft, soft)
	}
	content = fmt.Sprintf(`[Unit]
Description=auto-dba %s %s
After=network.target

[Service]
Type=%s
ExecStart=%s
ExecStop=%s
Restart=on-failure
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, cl.Engine, n.Role, serviceType(cl.Engine), execStart, execStop)
	_ = port
	return unit, content
}

func serviceType(engine string) string {
	switch engine {
	case "sqlserver", "oceanbase", "elasticsearch", "cassandra", "kafka":
		return "simple"
	default:
		return "forking"
	}
}

func needDirs(cl *store.DbDeployCluster, n *store.DbDeployNode) []string {
	soft := softBase(cl, n)
	data := dataRoot(cl)
	return []string{soft, data, filepath.ToSlash(filepath.Join(soft, "bin")), filepath.ToSlash(filepath.Join(data, "log"))}
}

func pickPrimary(nodes []store.DbDeployNode) *store.DbDeployNode {
	for i := range nodes {
		if nodes[i].Role == "primary" || nodes[i].Role == "single" {
			return &nodes[i]
		}
	}
	if len(nodes) > 0 {
		return &nodes[0]
	}
	return nil
}

func orderNodesForDeploy(cl *store.DbDeployCluster, nodes []store.DbDeployNode) []store.DbDeployNode {
	priority := func(role string) int {
		if p := orderPriorityForEngine(cl.Engine, role); p >= 0 {
			return p
		}
		switch role {
		case "primary", "single":
			return 0
		case "codis_dashboard":
			return 1
		case "codis_server":
			return 2
		case "codis_proxy":
			return 3
		case "replica":
			return 4
		case "sentinel":
			return 5
		default:
			return 9
		}
	}
	out := append([]store.DbDeployNode(nil), nodes...)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if priority(out[j].Role) < priority(out[i].Role) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
