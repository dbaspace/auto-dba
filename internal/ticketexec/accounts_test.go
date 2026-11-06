package ticketexec

import (
	"strings"
	"testing"
)

func TestListAccountSQL(t *testing.T) {
	if !strings.Contains(sqlListPostgresRoles, "pg_roles") || !strings.Contains(sqlListPostgresRoles, "rolname !~ '^pg_'") {
		t.Fatalf("postgres roles sql = %s", sqlListPostgresRoles)
	}
	if !strings.Contains(sqlListMySQLUsers, "mysql.user") || !strings.Contains(sqlListMySQLUsers, "User <> ''") {
		t.Fatalf("mysql users sql = %s", sqlListMySQLUsers)
	}
}

func TestUniqueStarRocksUsers(t *testing.T) {
	got := uniqueStarRocksUsers([]string{"'test'@'%'", "'test'@'10.0.0.1'", "'data_front_user'@'%'", "''@'%'"})
	if len(got) != 2 || got[0] != "test" || got[1] != "data_front_user" {
		t.Fatalf("users = %#v", got)
	}
}
