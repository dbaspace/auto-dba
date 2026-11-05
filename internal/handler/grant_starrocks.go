package handler

import (
	"fmt"
	"strings"
)

// StarRocks 专属权限代码；其它引擎提交时会被过滤。
const (
	permStarRocksDDL    = "ddl"
	permStarRocksExport = "export"
)

func starRocksOnlyPerm(p string) bool {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case permStarRocksDDL, permStarRocksExport:
		return true
	}
	return false
}

// normalizeAccountPerms 去空去重；非 StarRocks 引擎剔除 ddl/export。
func normalizeAccountPerms(engine string, perms []string) []string {
	out := make([]string, 0, len(perms))
	seen := map[string]bool{}
	for _, p := range perms {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		if engine != "starrocks" && starRocksOnlyPerm(p) {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func starRocksUserIdent(user string) string {
	return "'" + strings.ReplaceAll(strings.TrimSpace(user), "'", "''") + "'@'%'"
}

func buildStarRocksCreateUserSQL(user, password string) string {
	if strings.TrimSpace(user) == "" {
		return ""
	}
	return fmt.Sprintf("CREATE USER %s IDENTIFIED BY %s", starRocksUserIdent(user), sqlQuoteString(password))
}

type srGrantObject struct {
	clause string
	order  []string
}

// 各对象的权限排列顺序与运维给定的标准授权语句一致。
var srGrantObjects = []srGrantObject{
	{"ALL TABLES IN DATABASE %s", []string{"DELETE", "DROP", "INSERT", "SELECT", "ALTER", "EXPORT", "UPDATE"}},
	{"ALL VIEWS IN DATABASE %s", []string{"SELECT", "DROP"}},
	{"ALL MATERIALIZED VIEWS IN DATABASE %s", []string{"ALTER", "REFRESH", "DROP", "SELECT"}},
	{"DATABASE %s", []string{"CREATE TABLE", "DROP", "ALTER", "CREATE VIEW", "CREATE FUNCTION", "CREATE MATERIALIZED VIEW", "CREATE PIPE"}},
}

// srPermPrivileges 权限代码 -> 每类对象上授予的权限（下标对应 srGrantObjects）。
var srPermPrivileges = map[string][4][]string{
	"r":                 {{"SELECT"}, {"SELECT"}, {"SELECT"}, nil},
	"c":                 {{"INSERT"}, nil, nil, nil},
	"u":                 {{"UPDATE"}, nil, nil, nil},
	"d":                 {{"DELETE"}, nil, nil, nil},
	permStarRocksExport: {{"EXPORT"}, nil, nil, nil},
	permStarRocksDDL: {
		{"DROP", "ALTER"},
		{"DROP"},
		{"ALTER", "REFRESH", "DROP"},
		{"CREATE TABLE", "DROP", "ALTER", "CREATE VIEW", "CREATE FUNCTION", "CREATE MATERIALIZED VIEW", "CREATE PIPE"},
	},
}

// renderStarRocksGrantStatements 按表/视图/物化视图/库四类对象生成 GRANT，空类跳过。
func renderStarRocksGrantStatements(user, db string, perms []string) []string {
	user = strings.TrimSpace(user)
	db = strings.TrimSpace(db)
	if user == "" || db == "" {
		return nil
	}
	var want [4]map[string]bool
	for i := range want {
		want[i] = map[string]bool{}
	}
	for _, p := range perms {
		privs, ok := srPermPrivileges[strings.ToLower(strings.TrimSpace(p))]
		if !ok {
			continue
		}
		for i, list := range privs {
			for _, priv := range list {
				want[i][priv] = true
			}
		}
	}
	var out []string
	for i, obj := range srGrantObjects {
		var privs []string
		for _, priv := range obj.order {
			if want[i][priv] {
				privs = append(privs, priv)
			}
		}
		if len(privs) == 0 {
			continue
		}
		on := fmt.Sprintf(obj.clause, db)
		out = append(out, fmt.Sprintf("GRANT %s ON %s TO USER %s", strings.Join(privs, ", "), on, starRocksUserIdent(user)))
	}
	return out
}

func renderStarRocksDCLScript(databases []string, grantee string, perms []string) string {
	var b strings.Builder
	for _, db := range databases {
		for _, stmt := range renderStarRocksGrantStatements(grantee, db, perms) {
			b.WriteString(stmt)
			b.WriteString(";\n")
		}
	}
	return strings.TrimSpace(b.String())
}
