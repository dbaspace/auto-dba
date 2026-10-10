package dbdeploy

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestMajorVersionAndValidate(t *testing.T) {
	cases := []struct {
		engine, ver, major string
		ok                 bool
	}{
		{store.DbDeployEngineMySQL, "8.0.36", "8.0", true},
		{store.DbDeployEngineMySQL, "8.4.3", "8.4", true},
		{store.DbDeployEngineMySQL, "5.7.44", "5.7", true},
		{store.DbDeployEngineMySQL, "8.0", "8.0", false},
		{store.DbDeployEngineMySQL, "6.0.1", "6.0", false},
		{store.DbDeployEnginePostgres, "16.4", "16", true},
		{store.DbDeployEnginePostgres, "17", "17", true},
		{store.DbDeployEnginePostgres, "12.20", "12", false},
		{store.DbDeployEnginePostgres, "16.4.1", "16", false},
	}
	for _, c := range cases {
		if got := MajorVersion(c.engine, c.ver); got != c.major {
			t.Errorf("MajorVersion(%s,%s)=%s want %s", c.engine, c.ver, got, c.major)
		}
		if err := ValidateVersion(c.engine, c.ver); (err == nil) != c.ok {
			t.Errorf("ValidateVersion(%s,%s) err=%v want ok=%v", c.engine, c.ver, err, c.ok)
		}
	}
}

func TestBuildReleaseObjectKey(t *testing.T) {
	got := BuildReleaseObjectKey("db-artifacts/mysql", "8.0.36", "", "/tmp/mysql-8.0.36-linux-glibc2.17-x86_64.tar.xz")
	want := "db-artifacts/mysql/8.0.36/x86_64/mysql-8.0.36-linux-glibc2.17-x86_64.tar.xz"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if !ReleaseReady(&store.DbDeployRelease{ObjectKey: got, Checksum: strings.Repeat("a", 64)}) {
		t.Fatal("expected ready")
	}
	if ReleaseReady(&store.DbDeployRelease{ObjectKey: "db-artifacts/mysql/8.0.36/x86_64/", Checksum: strings.Repeat("a", 64)}) {
		t.Fatal("dir key must not be ready")
	}
}

func TestBuiltinMajorFor(t *testing.T) {
	for in, want := range map[string]string{"5.7": "5.7", "8.0": "8.0", "8.3": "8.0", "8.4": "8.4", "9.1": "8.4"} {
		if got := builtinMajorFor(store.DbDeployEngineMySQL, in); got != want {
			t.Errorf("builtinMajorFor(%s)=%s want %s", in, got, want)
		}
	}
}

func TestValidateRejectsManagedKeys(t *testing.T) {
	if err := ValidateConfTemplate(store.DbDeployEngineMySQL, "[mysqld]\nserver-id = 3\n"); err == nil || !strings.Contains(err.Error(), "server_id") {
		t.Fatalf("expected managed key error, got %v", err)
	}
	if err := ValidateConfTemplate(store.DbDeployEngineMySQL, "[mysqld]\nloose-rpl_semi_sync_master_enabled=1\n"); err == nil {
		t.Fatal("loose_ prefix should still be managed")
	}
	if err := ValidateConfTemplate(store.DbDeployEngineMySQL, "[mysqld]\nlog_error = {{.Nope}}/x\n"); err == nil {
		t.Fatal("unknown var should fail")
	}
	if err := ValidateClusterConf(store.DbDeployEnginePostgres, "hot_standby = off"); err == nil {
		t.Fatal("pg managed key should fail")
	}
	if err := ValidateClusterConf(store.DbDeployEngineMySQL, "[client]\nfoo=1"); err == nil {
		t.Fatal("section header should fail")
	}
	if err := ValidateClusterConf(store.DbDeployEnginePostgres, "work_mem"); err == nil {
		t.Fatal("pg override without = should fail")
	}
	if err := ValidateClusterConf(store.DbDeployEngineMySQL, "max_connections = 3000\nskip-log-bin"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	for _, eng := range []string{store.DbDeployEngineMySQL, store.DbDeployEnginePostgres} {
		for _, m := range BuiltinConfMajors(eng) {
			body, _ := BuiltinConfTemplate(eng, m)
			if err := ValidateConfTemplate(eng, body); err != nil {
				t.Errorf("builtin %s %s invalid: %v", eng, m, err)
			}
		}
	}
}

func TestApplyClusterOverridesMySQL(t *testing.T) {
	base := "[client]\nmax_connections = 1\n\n[mysqld]\nmax_connections = 2000\ninnodb_buffer_pool_size = 1G\n\n[mysqldump]\nquick\n"
	out := applyClusterOverrides(store.DbDeployEngineMySQL, base, "max-connections = 3000\nlong_query_time = 2")
	if !strings.Contains(out, "[client]\nmax_connections = 1\n") {
		t.Fatalf("[client] must stay untouched:\n%s", out)
	}
	if !strings.Contains(out, "[mysqld]\nmax-connections = 3000\n") {
		t.Fatalf("expected replacement in [mysqld]:\n%s", out)
	}
	idx := strings.Index(out, "long_query_time = 2")
	if idx < 0 || idx > strings.Index(out, "[mysqldump]") {
		t.Fatalf("append must land in [mysqld] before [mysqldump]:\n%s", out)
	}
}

func mysqlCluster(version, topo string) *store.DbDeployCluster {
	return &store.DbDeployCluster{ID: "c1", Engine: store.DbDeployEngineMySQL, Topology: topo, Version: version,
		SoftwareBase: "/usr/local/mysql", DataRoot: "/data/mysql"}
}

func TestRenderTemplatedConfMySQL(t *testing.T) {
	primary := &store.DbDeployNode{ID: "n1", Role: "primary", Host: "10.0.0.1", Port: 3306}
	replica := &store.DbDeployNode{ID: "n2", Role: "replica", Host: "10.0.0.2", Port: 3306}
	ov := "innodb_buffer_pool_size = 8G\nmax_connections = 3000"

	cases := []struct {
		version, src string
		has, hasNot  []string
	}{
		{"5.7.44", "5.7", []string{"expire_logs_days", "semisync_master.so;semisync_slave.so", "rpl_semi_sync_master_enabled = 1"}, []string{"binlog_expire_logs_seconds"}},
		{"8.0.36", "8.0", []string{"binlog_expire_logs_seconds", "rpl_semi_sync_master_enabled = 1"}, []string{"expire_logs_days"}},
		{"8.4.3", "8.4", []string{"log_replica_updates", "semisync_source.so;semisync_replica.so", "rpl_semi_sync_source_enabled = 1"}, []string{"rpl_semi_sync_master", "log_slave_updates =", "binlog_format ="}},
	}
	for _, c := range cases {
		cl := mysqlCluster(c.version, store.DbDeployTopoMasterReplica)
		path, body, src, err := RenderTemplatedConf(nil, cl, primary, primary, &ov)
		if err != nil {
			t.Fatalf("%s: %v", c.version, err)
		}
		if path != "/usr/local/mysql/my.cnf" || src.Kind != "builtin" || src.Major != c.src {
			t.Fatalf("%s: path=%s src=%+v", c.version, path, src)
		}
		for _, s := range append([]string{
			"innodb_buffer_pool_size = 8G", "max_connections = 3000",
			"log_error = /data/mysql/log/mysqld.err", "basedir = /usr/local/mysql", "port = 3306",
		}, c.has...) {
			if !strings.Contains(body, s) {
				t.Errorf("%s: missing %q\n%s", c.version, s, body)
			}
		}
		for _, s := range c.hasNot {
			if strings.Contains(body, s) {
				t.Errorf("%s: unexpected %q\n%s", c.version, s, body)
			}
		}
		if strings.Contains(body, "read_only = ON") {
			t.Errorf("%s: primary must not be read_only", c.version)
		}
		if strings.Count(body, "innodb_buffer_pool_size") != 1 {
			t.Errorf("%s: override must replace, not duplicate", c.version)
		}

		_, rbody, _, err := RenderTemplatedConf(nil, cl, replica, primary, &ov)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rbody, "super_read_only = ON") {
			t.Errorf("%s: replica must be super_read_only", c.version)
		}
	}

	_, body, _, err := RenderTemplatedConf(nil, mysqlCluster("8.0.36", store.DbDeployTopoStandalone), &store.DbDeployNode{Role: "single", Host: "h", Port: 3307}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "semisync") {
		t.Errorf("standalone must not load semisync:\n%s", body)
	}
}

func TestRenderTemplatedConfPG(t *testing.T) {
	cl := &store.DbDeployCluster{ID: "c2", Engine: store.DbDeployEnginePostgres, Topology: store.DbDeployTopoMasterReplica, Version: "16.4", DataRoot: "/data/pg"}
	primary := &store.DbDeployNode{ID: "n1", Role: "primary", Host: "10.0.0.1", Port: 5432}
	replica := &store.DbDeployNode{ID: "n2", Role: "replica", Host: "10.0.0.2", Port: 5432}
	ov := "shared_buffers = 4GB"

	path, body, src, err := RenderTemplatedConf(nil, cl, primary, primary, &ov)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/data/pg/postgresql.conf" || src.Major != store.DbDeployConfTemplateAnyMajor {
		t.Fatalf("path=%s src=%+v", path, src)
	}
	for _, s := range []string{"shared_buffers = 4GB", "wal_keep_size", "port = 5432", "data_directory = '/data/pg'", "synchronous_standby_names = '*'"} {
		if !strings.Contains(body, s) {
			t.Errorf("missing %q\n%s", s, body)
		}
	}
	if strings.Count(body, "shared_buffers") != 1 {
		t.Errorf("override must replace:\n%s", body)
	}

	_, rbody, _, err := RenderTemplatedConf(nil, cl, replica, primary, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rbody, "primary_conninfo = 'host=10.0.0.1 port=5432") || !strings.Contains(rbody, "hot_standby = on") {
		t.Errorf("replica standby settings missing:\n%s", rbody)
	}
}
