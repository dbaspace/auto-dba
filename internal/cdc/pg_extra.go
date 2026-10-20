package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func SetReplicaIdentityFull(ctx context.Context, db *sql.DB, schema string, tables []string) error {
	if db == nil {
		return fmt.Errorf("db nil")
	}
	for _, t := range tables {
		sch, tb := SplitSchemaTable(schema, t)
		if tb == "" {
			continue
		}
		stmt := fmt.Sprintf(`ALTER TABLE %s REPLICA IDENTITY FULL`, quotePGIdent(sch+"."+tb))
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("REPLICA IDENTITY FULL %s.%s: %w", sch, tb, err)
		}
	}
	return nil
}

func PGWalLevel(ctx context.Context, db *sql.DB) (string, error) {
	var level string
	if err := db.QueryRowContext(ctx, `SHOW wal_level`).Scan(&level); err != nil {
		return "", err
	}
	return strings.TrimSpace(level), nil
}

// MissingPGTables 校验 tables 是否存在：按 schema 各 ListPGTables 一次后做内存差集（避免逐表 COUNT）。
func MissingPGTables(ctx context.Context, db *sql.DB, schema string, tables []string) []string {
	if db == nil || len(tables) == 0 {
		return nil
	}
	bySchema := map[string][]string{}
	var schemaOrder []string
	for _, t := range tables {
		sch, tb := SplitSchemaTable(schema, t)
		if tb == "" {
			continue
		}
		if _, ok := bySchema[sch]; !ok {
			schemaOrder = append(schemaOrder, sch)
		}
		bySchema[sch] = append(bySchema[sch], tb)
	}
	var missing []string
	for _, sch := range schemaOrder {
		want := bySchema[sch]
		listed, err := ListPGTables(ctx, db, sch)
		if err != nil {
			for _, tb := range want {
				missing = append(missing, sch+"."+tb)
			}
			continue
		}
		have := make(map[string]struct{}, len(listed))
		for _, n := range listed {
			have[strings.ToLower(strings.TrimSpace(n))] = struct{}{}
		}
		missing = append(missing, MissingFromTableSet(have, sch, want)...)
	}
	return missing
}

// MissingFromTableSet 在已列出的表名集合中找缺失项；have 的 key 应为小写表名。
func MissingFromTableSet(have map[string]struct{}, schema string, wantTables []string) []string {
	if len(wantTables) == 0 {
		return nil
	}
	schema = strings.TrimSpace(schema)
	if schema == "" {
		schema = "public"
	}
	var missing []string
	seen := map[string]struct{}{}
	for _, tb := range wantTables {
		tb = strings.TrimSpace(tb)
		if tb == "" {
			continue
		}
		key := strings.ToLower(tb)
		if _, ok := have[key]; ok {
			continue
		}
		label := schema + "." + tb
		lk := strings.ToLower(label)
		if _, dup := seen[lk]; dup {
			continue
		}
		seen[lk] = struct{}{}
		missing = append(missing, label)
	}
	return missing
}

func ListPublicationTables(ctx context.Context, db *sql.DB, pub string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT schemaname, tablename FROM pg_publication_tables WHERE pubname=$1 ORDER BY schemaname, tablename`, pub)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sch, tb string
		if err := rows.Scan(&sch, &tb); err != nil {
			return nil, err
		}
		if sch != "" && sch != "public" {
			out = append(out, sch+"."+tb)
		} else {
			out = append(out, tb)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, rows.Err()
}

func DropPublication(ctx context.Context, db *sql.DB, pub string) error {
	pub = strings.TrimSpace(pub)
	if pub == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `DROP PUBLICATION IF EXISTS `+quotePGIdent(pub))
	return err
}

func DropReplicationSlot(ctx context.Context, db *sql.DB, slot string) error {
	slot = strings.TrimSpace(slot)
	if slot == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `SELECT pg_drop_replication_slot($1)`, slot)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "does not exist") {
		return nil
	}
	return err
}

func BuildRoutineLoadSQL(db, table string, keys []string, conc int, brokers string, topics []string, mode string) string {
	rl := RoutineLoadNameFor("", "", "", table)
	return buildRoutineLoadSQL(db, table, rl, keys, conc, brokers, topics, mode, "OFFSET_BEGINNING", "")
}

func BuildAlterMigratePlan(targetDB, targetTable, brokers, topic, mode string, existingCols []string, addCols []SchemaDiffColumn, conc int) map[string]string {
	return BuildAlterMigratePlanOpts(targetDB, targetTable, brokers, topic, mode, existingCols, addCols, conc, "OFFSET_BEGINNING", "", "", "", "")
}

func BuildAlterMigratePlanOpts(targetDB, targetTable, brokers, topic, mode string, existingCols []string, addCols []SchemaDiffColumn, conc int, kafkaOffset, schemaMode, env, sourceDB, topicPrefix string) map[string]string {
	rl := RoutineLoadNameFor(env, sourceDB, topicPrefix, targetTable)
	if conc <= 0 {
		conc = 3
	}
	cols := selectedAlterColumns(addCols)
	alter := BuildAlterAddColumnSQL(targetDB, targetTable, cols)
	pause := fmt.Sprintf("PAUSE ROUTINE LOAD FOR `%s`.`%s`;", targetDB, rl)
	stop := fmt.Sprintf("STOP ROUTINE LOAD FOR `%s`.`%s`;", targetDB, rl)
	all := append([]string{}, existingCols...)
	seen := map[string]struct{}{}
	for _, c := range all {
		seen[strings.ToLower(c)] = struct{}{}
	}
	for _, c := range cols {
		if _, ok := seen[strings.ToLower(c.Name)]; ok {
			continue
		}
		all = append(all, c.Name)
	}
	var keys []string
	for _, c := range all {
		if IsSRMetaColumn(c) {
			continue
		}
		keys = append(keys, c)
	}
	create := ""
	if len(keys) > 0 && strings.TrimSpace(topic) != "" {
		create = buildRoutineLoadSQL(targetDB, targetTable, rl, keys, conc, brokers, []string{topic}, mode, NormalizeKafkaOffset(kafkaOffset), schemaMode)
	}
	return map[string]string{
		"pause_sql":  pause,
		"stop_sql":   stop,
		"alter_sql":  alter,
		"create_sql": create,
	}
}
