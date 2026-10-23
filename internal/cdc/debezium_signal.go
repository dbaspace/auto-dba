package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	DefaultDebeziumSignalTable  = "debezium_signal"
	DefaultDebeziumSignalSchema = "public"
)

func ParseQualifiedSignalTable(signalTable string) (schema, table string) {
	signalTable = strings.TrimSpace(signalTable)
	if signalTable == "" {
		return DefaultDebeziumSignalSchema, DefaultDebeziumSignalTable
	}
	if idx := strings.Index(signalTable, "."); idx >= 0 {
		s := strings.TrimSpace(signalTable[:idx])
		t := strings.TrimSpace(signalTable[idx+1:])
		if s != "" && t != "" {
			return s, t
		}
	}
	return DefaultDebeziumSignalSchema, signalTable
}

func IsProtectedPublicationTable(name string) bool {
	_, table := SplitSchemaTable("", name)
	return strings.EqualFold(table, DefaultDebeziumSignalTable)
}

func FilterProtectedPublicationTables(tables []string) []string {
	var out []string
	for _, t := range tables {
		if IsProtectedPublicationTable(t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// PartitionMissingSourceTables 把缺失表拆成业务表和信号表。初始化预检只拦截业务表。
func PartitionMissingSourceTables(missing []string) (business, signal []string) {
	for _, name := range missing {
		if IsProtectedPublicationTable(name) {
			signal = append(signal, name)
			continue
		}
		business = append(business, name)
	}
	return business, signal
}

func SignalTableExists(ctx context.Context, db *sql.DB, signalTable string) (bool, error) {
	schema, table := ParseQualifiedSignalTable(signalTable)
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM information_schema.tables WHERE table_schema=$1 AND table_name=$2`, schema, table).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func EnsureDebeziumSignalTableInPublication(ctx context.Context, db *sql.DB, pubName, signalTable string) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("db nil")
	}
	schema, table := ParseQualifiedSignalTable(signalTable)
	pubName = strings.TrimSpace(pubName)
	if pubName == "" {
		return false, fmt.Errorf("publication name empty for signal table")
	}
	exists, err := SignalTableExists(ctx, db, schema+"."+table)
	if err != nil {
		return false, fmt.Errorf("check debezium_signal table: %w", err)
	}
	created := false
	if !exists {
		ddl := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (id VARCHAR(64) PRIMARY KEY, type VARCHAR(32) NOT NULL, data VARCHAR(2048))`,
			quotePGIdent(schema+"."+table),
		)
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return false, fmt.Errorf("create signal table %s.%s: %w", schema, table, err)
		}
		created = true
	}
	current, err := ListPublicationTables(ctx, db, pubName)
	if err != nil {
		return created, fmt.Errorf("list publication tables: %w", err)
	}
	for _, n := range current {
		_, tb := SplitSchemaTable(schema, n)
		if strings.EqualFold(tb, table) {
			return created, nil
		}
	}
	stmt := fmt.Sprintf(`ALTER PUBLICATION %s ADD TABLE %s`, quotePGIdent(pubName), quotePGIdent(schema+"."+table))
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "already") || strings.Contains(low, "duplicate") {
			return created, nil
		}
		return created, fmt.Errorf("alter publication add signal table: %w", err)
	}
	return true, nil
}

func ResolveSignalTable(extra map[string]string) string {
	if extra != nil {
		if v := strings.TrimSpace(extra["signal_data_collection"]); v != "" {
			return v
		}
		if raw := strings.TrimSpace(extra["debezium_config"]); raw != "" {
			cfg := ParseJSONStringMap(raw)
			if v := strings.TrimSpace(cfg["signal.data.collection"]); v != "" {
				return v
			}
		}
	}
	return DefaultDebeziumSignalSchema + "." + DefaultDebeziumSignalTable
}

func InsertDebeziumSnapshotSignal(ctx context.Context, db *sql.DB, signalTable, signalID, dataJSON string) error {
	if db == nil {
		return fmt.Errorf("db nil")
	}
	signalID = strings.TrimSpace(signalID)
	if signalID == "" {
		return fmt.Errorf("snapshot signal id empty")
	}
	schema, table := ParseQualifiedSignalTable(signalTable)
	stmt := fmt.Sprintf(
		`INSERT INTO %s (id, type, data) VALUES ($1, 'execute-snapshot', $2) ON CONFLICT (id) DO NOTHING`,
		quotePGIdent(schema+"."+table),
	)
	_, err := db.ExecContext(ctx, stmt, signalID, dataJSON)
	return err
}
