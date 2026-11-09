package clientaccess

import "testing"

func TestWhitelistOK(t *testing.T) {
	cases := []struct {
		method, path string
		ok           bool
	}{
		{"POST", "/api/v1/query", true},
		{"GET", "/api/v1/query/catalog", true},
		{"POST", "/api/v1/query/table-info", true},
		{"GET", "/api/v1/databases/abc/tables", true},
		{"GET", "/api/v1/databases/abc/extra/tables", false},
		{"GET", "/api/v1/db-accounts/1", false},
		{"POST", "/api/v1/tickets/1/execute", false},
		{"GET", "/api/v1/query/favorites", true},
		{"GET", "/api/v1/query/history", true},
		{"POST", "/api/v1/instances/i1/redis/execute", true},
		{"GET", "/api/v1/instances/i1/kafka/topics", true},
		{"GET", "/api/v1/instances/i1/kafka/topics/t1/messages", true},
		{"GET", "/api/v1/db-accounts/a1", false},
		{"GET", "/api/v1/users", false},
		{"GET", "/api/v1/users/u1", false},
		{"PUT", "/api/v1/users/u1/password", false},
		{"GET", "/api/v1/roles/r1/users", false},
		{"GET", "/api/v1/auth/me", false},
		{"GET", "/api/v1/instances", false},
		{"GET", "/api/v1/instances/i1", false},
		{"POST", "/api/v1/instances/i1/open-terminal", false},
		{"GET", "/api/v1/dcl-grants/accounts", false},
		{"GET", "/api/v1/nacosadmin/credential", false},
		{"GET", "/api/v1/nacos-console/prod", false},
		{"GET", "/api/v1/nacos-proxy/prod/v1/cs/configs", false},
		{"GET", "/api/v1/settings/ldap", false},
		{"GET", "/api/v1/global-args", false},
		{"GET", "/api/v1/feadmin/x", false},
		{"GET", "/api/v1/dsadmin/credential", false},
		{"GET", "/api/v1/k8s/clusters", false},
		{"GET", "/api/v1/oss-settings", false},
		{"POST", "/api/v1/oss-uploads", false},
		{"GET", "/api/v1/backup/agents", false},
		{"GET", "/api/v1/platform/agents", false},
		{"POST", "/api/v1/tickets", false},
		{"POST", "/api/v1/create-db", false},
		{"POST", "/api/v1/redis-rdb/upload", false},
		{"POST", "/api/v1/sr-cluster/releases/1/artifacts/upload", false},
	}
	if WhitelistOKWithOpts("GET", "/api/v1/db-accounts/a1", WhitelistOpts{AllowCredential: true}) {
		t.Fatal("PAT 不能走看密接口")
	}
	for _, tc := range cases {
		if got := WhitelistOK(tc.method, tc.path); got != tc.ok {
			t.Fatalf("%s %s: got %v want %v", tc.method, tc.path, got, tc.ok)
		}
	}
}

func TestIsExemptPath(t *testing.T) {
	if !IsExemptPath("/api/v1/feadmin/resolve-credential") {
		t.Fatal("feadmin should be exempt")
	}
	if !IsExemptPath("/api/v1/copilot/chat") {
		t.Fatal("copilot should be exempt")
	}
	if IsExemptPath("/api/v1/query") {
		t.Fatal("query should not be exempt")
	}
}

func TestIsMachineClientType(t *testing.T) {
	if !IsMachineClientType("mcp") || !IsMachineClientType("CLI") {
		t.Fatal("expected machine types")
	}
	if IsMachineClientType("browser") {
		t.Fatal("browser is not machine")
	}
}

func TestScriptFileName(t *testing.T) {
	if !ScriptFileName(`form-data; name="file"; filename="a.SQL"`) {
		t.Fatal("sql upload")
	}
	if !ScriptFileName(`attachment; filename="run.js"`) {
		t.Fatal("js upload")
	}
	if ScriptFileName(`attachment; filename="notes.json"`) {
		t.Fatal("json is not a script")
	}
	if ScriptFileName("") {
		t.Fatal("empty")
	}
}
