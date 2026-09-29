package srcluster

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestBuiltinBeConfMaterialize(t *testing.T) {
	cl := &store.SrCluster{
		ID:              "c1",
		SoftwareBase:    "/home/dorisdb_base",
		DataRoot:        "/data/dorisdb",
		BeHeartbeatPort: 9050,
	}
	n := &store.SrNode{
		ID:           "n1",
		Component:    "be",
		Host:         "172.23.16.42",
		SoftwarePath: "/home/dorisdb_base/be",
		AgentID:      "a1",
	}
	path, body, source, err := MaterializeConf(nil, cl, n)
	if err != nil {
		t.Fatal(err)
	}
	if source != "builtin" {
		t.Fatalf("source=%s", source)
	}
	if !strings.Contains(path, "/be/conf/be.conf") {
		t.Fatalf("path=%s", path)
	}
	want := []string{
		"be_port = 9060",
		"be_http_port = 8040",
		"heartbeat_service_port = 9050",
		"brpc_port = 8060",
		"starlet_port = 9070",
		"priority_networks = 172.23.16.42",
		"storage_root_path = /data/dorisdb/be/storage",
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Fatalf("missing %q in:\n%s", w, body)
		}
	}
	if strings.Contains(body, "webserver_port") {
		t.Fatal("should use be_http_port not webserver_port")
	}
}

func TestBuiltinFeConfMaterialize(t *testing.T) {
	cl := &store.SrCluster{
		ID:           "c1",
		SoftwareBase: "/home/dorisdb_base",
		DataRoot:     "/data",
		FeHTTPPort:   8030,
		FeEditPort:   9010,
		FeQueryPort:  9030,
	}
	n := &store.SrNode{
		ID:           "n1",
		Component:    "fe",
		Host:         "172.23.16.21",
		SoftwarePath: "/home/dorisdb_base/fe",
		IsPrimaryFE:  true,
		AgentID:      "a1",
	}
	_, body, source, err := MaterializeConf(nil, cl, n)
	if err != nil {
		t.Fatal(err)
	}
	if source != "builtin" {
		t.Fatalf("source=%s", source)
	}
	want := []string{
		`LOG_DIR = ${STARROCKS_HOME}/log`,
		"JAVA_OPTS=",
		"JAVA_OPTS_FOR_JDK_11=",
		"sys_log_level = INFO",
		"http_port = 8030",
		"rpc_port = 9020",
		"query_port = 9030",
		"edit_log_port = 9010",
		"mysql_service_nio_enabled = true",
		"max_routine_load_task_num_per_be = 500",
		"frontend_address = 172.23.16.21",
		"empty_load_as_error = false",
		"meta_dir = /home/dorisdb_base/fe/meta",
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Fatalf("missing %q in:\n%s", w, body)
		}
	}
	if strings.Count(body, "empty_load_as_error") != 1 {
		t.Fatalf("empty_load_as_error should appear once:\n%s", body)
	}
}
