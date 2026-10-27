package clientaccess

import "testing"

func TestWhitelistOK(t *testing.T) {
	cases := []struct {
		method, path string
		ok           bool
	}{
		{"POST", "/api/v1/query", true},
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
	}
	if !WhitelistOKWithOpts("GET", "/api/v1/db-accounts/a1", WhitelistOpts{AllowCredential: true}) {
		t.Fatal("credential whitelist")
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
