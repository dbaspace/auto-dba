package ticketexec

import (
	"strings"
	"testing"
)

func TestPGMetaSQLShape(t *testing.T) {
	if !strings.Contains(sqlLookupPGDatabaseOwner, "pg_database") || !strings.Contains(sqlLookupPGDatabaseOwner, "datdba") {
		t.Fatalf("owner sql: %s", sqlLookupPGDatabaseOwner)
	}
	if !strings.Contains(sqlPostgresUserExists, "pg_user") || !strings.Contains(sqlPostgresUserExists, "usename") {
		t.Fatalf("user sql: %s", sqlPostgresUserExists)
	}
}
