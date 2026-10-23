package cdc

import (
	"fmt"
	"strings"
)

const (
	srColPhysicalDeleted = "is_physical_deleted"
	srColSourceDBName    = "source_db_name"
	srColSourceTableName = "source_table_name"
)

func srMetaColumnLines(cols []pgCol) []string {
	have := map[string]struct{}{}
	for _, c := range cols {
		have[strings.ToLower(strings.TrimSpace(c.Name))] = struct{}{}
	}
	var lines []string
	if _, ok := have[srColPhysicalDeleted]; !ok {
		lines = append(lines, "  `"+srColPhysicalDeleted+"` BOOLEAN NOT NULL DEFAULT \"false\" COMMENT '物理删除'")
	}
	if _, ok := have[srColSourceDBName]; !ok {
		lines = append(lines, "  `"+srColSourceDBName+"` VARCHAR(100) NULL COMMENT '库名'")
	}
	if _, ok := have[srColSourceTableName]; !ok {
		lines = append(lines, "  `"+srColSourceTableName+"` VARCHAR(100) NULL COMMENT '表名'")
	}
	return lines
}

func routineLoadSchemaMode(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "with_schema") {
		return "with_schema"
	}
	return "without_schema"
}

func isSRTableMetaName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case srColPhysicalDeleted, srColSourceDBName, srColSourceTableName, "__op", "__deleted", "__source_db", "__source_table":
		return true
	default:
		return false
	}
}

func pgColKind(c pgCol) string {
	if pgColIsDateOnly(c) {
		return "date"
	}
	if pgColIsTimestampWithoutTZ(c) {
		return "timestamp"
	}
	if pgColHasTimeZone(c) {
		return "timestamptz"
	}
	udt := strings.ToLower(strings.TrimSpace(c.UDT))
	dt := strings.ToLower(strings.TrimSpace(c.DataType))
	switch {
	case udt == "json" || udt == "jsonb" || dt == "json" || dt == "jsonb":
		return "json"
	case udt == "bool" || udt == "boolean" || dt == "boolean":
		return "boolean"
	default:
		return ""
	}
}

func renderRoutineLoadSQL(db, table, rlName string, cols []pgCol, conc int, brokers string, topics []string, mode, kafkaOffset, schemaMode string) string {
	broker := strings.TrimSpace(brokers)
	if broker == "" {
		broker = "kafka:9092"
	}
	if conc <= 0 {
		conc = 3
	}
	offset := defaultStr(NormalizeKafkaOffset(kafkaOffset), "OFFSET_BEGINNING")
	rlName = strings.TrimSpace(rlName)
	if rlName == "" {
		rlName = RoutineLoadNameFor("", "", "", table)
	}
	mode = strings.TrimSpace(mode)
	schemaMode = routineLoadSchemaMode(schemaMode)
	var exprs []string
	var jsonpaths string
	switch {
	case schemaMode == "without_schema" && mode == ModeBeforeAfter:
		exprs, jsonpaths = routineLoadExprsBeforeAfter(cols, false)
	case schemaMode == "without_schema":
		exprs = routineLoadExprsUnwrap(cols)
	case mode == ModeBeforeAfter:
		exprs, jsonpaths = routineLoadExprsBeforeAfter(cols, true)
	default:
		exprs, jsonpaths = routineLoadExprsWithSchema(cols)
	}
	props := []string{
		`"format" = "json"`,
		`"strip_outer_array" = "false"`,
		fmt.Sprintf(`"desired_concurrent_number" = "%d"`, conc),
		`"max_batch_interval" = "20"`,
		`"max_batch_rows" = "300000"`,
		`"max_error_number" = "1000"`,
	}
	if jsonpaths != "" {
		props = []string{
			`"format" = "json"`,
			fmt.Sprintf(`"jsonpaths" = "[%s]"`, jsonpaths),
			`"strip_outer_array" = "false"`,
			fmt.Sprintf(`"desired_concurrent_number" = "%d"`, conc),
			`"max_batch_interval" = "20"`,
			`"max_batch_rows" = "300000"`,
			`"max_error_number" = "1000"`,
		}
	}
	return fmt.Sprintf("CREATE ROUTINE LOAD `%s`.`%s` ON `%s`\nCOLUMNS(%s)\nPROPERTIES (\n  %s\n)\nFROM KAFKA (\n  \"kafka_broker_list\" = \"%s\",\n  \"kafka_topic\" = \"%s\",\n  \"property.group.id\" = \"%s\",\n  \"property.kafka_default_offsets\" = \"%s\"\n);",
		db, rlName, table, strings.Join(exprs, ", "), strings.Join(props, ",\n  "), broker, strings.Join(topics, ","), rlName, offset)
}

func businessCols(cols []pgCol) []pgCol {
	var out []pgCol
	for _, c := range cols {
		if strings.TrimSpace(c.Name) == "" || isSRTableMetaName(c.Name) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func routineLoadExprsUnwrap(cols []pgCol) []string {
	biz := businessCols(cols)
	var exprs []string
	for _, c := range biz {
		exprs = append(exprs, c.Name)
	}
	exprs = append(exprs, "__source_db", "__source_table", "__deleted")
	for _, c := range biz {
		switch pgColKind(c) {
		case "date":
			exprs = append(exprs, fmt.Sprintf("`%s`=date_add('1970-01-01', INTERVAL `%s` DAY)", c.Name, c.Name))
		case "timestamp":
			exprs = append(exprs, fmt.Sprintf("`%s`=DATE_ADD(days_add('1970-01-01 00:00:00', FLOOR(`%s` / 86400000)), INTERVAL `%s`-FLOOR(`%s` / 86400000)*86400000 MILLISECOND)", c.Name, c.Name, c.Name, c.Name))
		case "json":
			exprs = append(exprs, fmt.Sprintf("`%s`=parse_json(`%s`)", c.Name, c.Name))
		case "boolean":
			exprs = append(exprs, fmt.Sprintf("`%s`=coalesce(`%s`,\"false\")", c.Name, c.Name))
		}
	}
	exprs = append(exprs,
		"`"+srColSourceDBName+"`=`__source_db`",
		"`"+srColSourceTableName+"`=`__source_table`",
		"`"+srColPhysicalDeleted+"`=`__deleted`",
	)
	return exprs
}

func routineLoadExprsWithSchema(cols []pgCol) ([]string, string) {
	exprs := []string{"payload"}
	for _, c := range businessCols(cols) {
		sub := "$." + c.Name
		exprs = append(exprs, payloadExpr(c, sub))
	}
	exprs = append(exprs,
		"`"+srColPhysicalDeleted+"`=get_json_string(payload,'$.__deleted')",
		"`"+srColSourceDBName+"`=get_json_string(payload,'$.__source_db')",
		"`"+srColSourceTableName+"`=get_json_string(payload,'$.__source_table')",
	)
	return exprs, `\"$.payload\"`
}

func routineLoadExprsBeforeAfter(cols []pgCol, withSchema bool) ([]string, string) {
	if withSchema {
		exprs := []string{"payload"}
		for _, c := range businessCols(cols) {
			exprs = append(exprs, payloadExpr(c, "$.after."+c.Name))
		}
		exprs = append(exprs,
			"`"+srColPhysicalDeleted+"`='false'",
			"`"+srColSourceDBName+"`=get_json_string(payload,'$.source.db')",
			"`"+srColSourceTableName+"`=get_json_string(payload,'$.source.table')",
		)
		return exprs, `\"$.payload\"`
	}
	exprs := []string{"payload", "source_data"}
	for _, c := range businessCols(cols) {
		exprs = append(exprs, payloadExpr(c, "$."+c.Name))
	}
	exprs = append(exprs,
		"`"+srColSourceDBName+"`=get_json_string(source_data,'$.db')",
		"`"+srColSourceTableName+"`=get_json_string(source_data,'$.table')",
		"`"+srColPhysicalDeleted+"`='false'",
	)
	return exprs, `\"$.after\", \"$.source\"`
}

func payloadJSON(sub string) string {
	return fmt.Sprintf("get_json_string(payload,'%s')", sub)
}

func payloadEmptyCond(raw string) string {
	return fmt.Sprintf("(%s IS NULL OR %s = '' OR lower(%s) = 'null')", raw, raw, raw)
}

func payloadEmptyOrZeroCond(raw string) string {
	return fmt.Sprintf("(%s OR %s = '0')", payloadEmptyCond(raw), raw)
}

func payloadExpr(c pgCol, sub string) string {
	raw := payloadJSON(sub)
	switch pgColKind(c) {
	case "date":
		inner := fmt.Sprintf("date_add('1970-01-01', INTERVAL %s DAY)", raw)
		if c.IsNullable {
			return fmt.Sprintf("`%s`=IF(%s, NULL, %s)", c.Name, payloadEmptyOrZeroCond(raw), inner)
		}
		return fmt.Sprintf("`%s`=%s", c.Name, inner)
	case "timestamp":
		inner := fmt.Sprintf("DATE_ADD(days_add('1970-01-01 00:00:00', FLOOR(%s / 86400000)), INTERVAL %s-FLOOR(%s / 86400000)*86400000 MILLISECOND)", raw, raw, raw)
		if c.IsNullable {
			return fmt.Sprintf("`%s`=IF(%s, NULL, %s)", c.Name, payloadEmptyOrZeroCond(raw), inner)
		}
		return fmt.Sprintf("`%s`=%s", c.Name, inner)
	case "timestamptz":
		if c.IsNullable {
			return fmt.Sprintf("`%s`=IF(%s, NULL, %s)", c.Name, payloadEmptyCond(raw), raw)
		}
		return fmt.Sprintf("`%s`=%s", c.Name, raw)
	default:
		return fmt.Sprintf("`%s`=%s", c.Name, raw)
	}
}
