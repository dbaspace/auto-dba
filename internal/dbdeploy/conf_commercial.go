package dbdeploy

import (
	"fmt"
	"path/filepath"
	"strings"

	"auto-dba/internal/store"
)

func renderOceanBaseConf(cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, soft, data string, port int) (string, string) {
	rpc := port + 1
	if port == 2881 {
		rpc = 2882
	}
	roleNote := "# role=" + n.Role
	if n.Role == "replica" && primary != nil {
		roleNote = fmt.Sprintf("# replica of %s:%d (MySQL 协议面；OB 日志流由制品脚本完成)", primary.Host, nodePort(cl, primary))
	}
	body := fmt.Sprintf(`# auto-dba oceanbase env（制品 start.sh / observer 读取）
OB_HOME=%s
OB_DATA=%s
OB_MYSQL_PORT=%d
OB_RPC_PORT=%d
OB_ZONE=zone1
%s
`, soft, data, port, rpc, roleNote)
	return filepath.ToSlash(filepath.Join(data, "oceanbase.env")), body
}

func renderOracleConf(cl *store.DbDeployCluster, n *store.DbDeployNode, soft, data string, port int) (string, string) {
	_ = cl
	_ = n
	base := soft
	if i := strings.LastIndex(strings.TrimRight(soft, "/"), "/"); i > 0 {
		parent := soft[:i]
		if j := strings.LastIndex(strings.TrimRight(parent, "/"), "/"); j > 0 {
			base = parent[:j]
		} else {
			base = parent
		}
	}
	body := fmt.Sprintf(`# auto-dba oracle env
ORACLE_HOME=%s
ORACLE_BASE=%s
ORACLE_SID=ORCL
ORACLE_DATA=%s
LISTENER_PORT=%d
`, soft, base, data, port)
	return filepath.ToSlash(filepath.Join(data, "oracle.env")), body
}

func renderSQLServerConf(cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, soft, data string, port int) (string, string) {
	note := ""
	if n.Role == "replica" && primary != nil {
		note = fmt.Sprintf("\n# replica of %s:%d — Always On / 日志传送由制品脚本完成\n", primary.Host, nodePort(cl, primary))
	}
	_ = soft
	_ = cl
	body := fmt.Sprintf(`[network]
tcpport = %d
ipaddress = 0.0.0.0

[filelocation]
defaultdatadir = %s/data
defaultlogdir = %s/log
%saccepteula = Y
`, port, data, data, note)
	return filepath.ToSlash(filepath.Join(data, "mssql.conf")), body
}
