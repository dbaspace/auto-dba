package dbdeploy

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestRenderCommercialConf(t *testing.T) {
	ob := &store.DbDeployCluster{Engine: "oceanbase", SoftwareBase: "/opt/ob", DataRoot: "/data/ob"}
	nOB := &store.DbDeployNode{Role: "single", Host: "10.0.0.1", Port: 2881}
	path, body := renderConf(ob, nOB, nil, nil)
	if !strings.Contains(path, "oceanbase.env") || !strings.Contains(body, "OB_MYSQL_PORT=2881") {
		t.Fatalf("oceanbase conf: %s %s", path, body)
	}
	or := &store.DbDeployCluster{Engine: "oracle", SoftwareBase: "/u01/dbhome", DataRoot: "/u01/oradata"}
	nOr := &store.DbDeployNode{Role: "single", Host: "10.0.0.1", Port: 1521}
	path, body = renderConf(or, nOr, nil, nil)
	if !strings.Contains(path, "oracle.env") || !strings.Contains(body, "ORACLE_HOME=") {
		t.Fatalf("oracle conf: %s %s", path, body)
	}
	ss := &store.DbDeployCluster{Engine: "sqlserver", SoftwareBase: "/opt/mssql", DataRoot: "/var/opt/mssql"}
	nSS := &store.DbDeployNode{Role: "single", Host: "10.0.0.1", Port: 1433}
	path, body = renderConf(ss, nSS, nil, nil)
	if !strings.Contains(path, "mssql.conf") || !strings.Contains(body, "tcpport = 1433") {
		t.Fatalf("sqlserver conf: %s %s", path, body)
	}
	unit, content := renderUnit(ss, nSS)
	if unit != "mssql-server.service" || !strings.Contains(content, "sqlservr") {
		t.Fatalf("sqlserver unit: %s %s", unit, content)
	}
}
