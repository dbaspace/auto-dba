package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type schemaEngineMySQL struct{}

func (schemaEngineMySQL) FetchAndDiff(ctx context.Context, left, right *sql.DB, _, _, table string) (TableSchemaDiff, error) {
	ls, err := fetchMySQLTableInfo(ctx, left, table)
	if err != nil {
		return TableSchemaDiff{}, fmt.Errorf("读左侧结构: %w", err)
	}
	rs, err := fetchMySQLTableInfo(ctx, right, table)
	if err != nil {
		return TableSchemaDiff{}, fmt.Errorf("读右侧结构: %w", err)
	}
	diffs := diffMySQLTableSchema(ls, rs)
	if len(diffs) == 0 {
		return TableSchemaDiff{Table: table, Status: "match", Message: "结构一致"}, nil
	}
	return TableSchemaDiff{Table: table, Status: "differ", Message: fmt.Sprintf("%d 处差异", len(diffs)), Diffs: diffs}, nil
}

func (schemaEngineMySQL) DiffRoutines(ctx context.Context, left, right *sql.DB, _, _ string) []TableSchemaDiff {
	return compareMySQLRoutines(ctx, left, right)
}

func (schemaEngineMySQL) BuildCreateSQL(ctx context.Context, src *sql.DB, _, _, table string) (string, error) {
	return buildMySQLCreateTableSQL(ctx, src, table)
}

func (schemaEngineMySQL) BuildAlterSQL(ctx context.Context, src *sql.DB, _, _ string, item *TableSchemaDiff) (string, error) {
	return buildMySQLAlterFixSQLFull(ctx, src, item)
}

func (schemaEngineMySQL) BuildRoutineFixSQL(ctx context.Context, src *sql.DB, _, _ string, item *TableSchemaDiff) (string, error) {
	return buildMySQLRoutineFixSQL(ctx, src, item)
}

func (schemaEngineMySQL) IsRoutineVirtualTable(name string) bool {
	return name == schemaRoutinesVirtualTable
}

func buildMySQLAlterFixSQLFull(ctx context.Context, src *sql.DB, item *TableSchemaDiff) (string, error) {
	info, err := fetchMySQLTableInfo(ctx, src, item.Table)
	if err != nil {
		return "", err
	}
	colMap := map[string]mysqlColInfo{}
	for _, c := range info.Columns {
		colMap[strings.ToLower(c.Name)] = c
	}
	conMap := map[string]mysqlConstraintInfo{}
	for _, c := range info.Constraints {
		conMap[strings.ToLower(c.Name)] = c
	}
	idxMap := map[string]mysqlIndexInfo{}
	for _, idx := range info.Indexes {
		idxMap[strings.ToLower(idx.Name)] = idx
	}
	fkMap := map[string]mysqlFKInfo{}
	for _, fk := range info.ForeignKeys {
		fkMap[strings.ToLower(fk.Name)] = fk
	}
	trigMap := map[string]mysqlTriggerInfo{}
	for _, t := range info.Triggers {
		trigMap[strings.ToLower(t.Name)] = t
	}

	ref := quoteMySQLIdent(item.Table)
	var b strings.Builder

	for _, d := range item.Diffs {
		msg := d.Message
		srcOnly := strings.Contains(msg, "仅源")
		differ := strings.Contains(msg, "不一致")
		switch d.Kind {
		case "column":
			if srcOnly {
				c, ok := colMap[strings.ToLower(d.Name)]
				if !ok {
					b.WriteString(fmt.Sprintf("-- 无法解析列 %s\n", d.Name))
					continue
				}
				b.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;\n", ref, mysqlColDDLPart(c)))
			} else if strings.Contains(msg, "列定义不一致") || differ {
				c, ok := colMap[strings.ToLower(d.Name)]
				if !ok {
					b.WriteString(fmt.Sprintf("-- TODO: 列 %s 无法解析源定义，需人工处理 源=%s 目标=%s\n", d.Name, d.Left, d.Right))
					continue
				}
				// 类型/默认值/可空等不一致统一 MODIFY（与改注释路径一致）
				b.WriteString(fmt.Sprintf("-- 对齐列 %s：源=%s 目标=%s\n", d.Name, d.Left, d.Right))
				b.WriteString(fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s;\n", ref, mysqlColDDLPart(c)))
			}
		case "comment":
			if srcOnly || differ {
				if d.Name == "" {
					b.WriteString(fmt.Sprintf("ALTER TABLE %s COMMENT=%s;\n", ref, quoteMySQLString(d.Left)))
				} else {
					c, ok := colMap[strings.ToLower(d.Name)]
					if !ok {
						b.WriteString(fmt.Sprintf("-- 无法解析列注释 %s\n", d.Name))
						continue
					}
					// MySQL 改列注释需 MODIFY 整列定义
					b.WriteString(fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s;\n", ref, mysqlColDDLPart(c)))
				}
			}
		case "constraint":
			if srcOnly {
				c, ok := conMap[strings.ToLower(d.Name)]
				if !ok {
					b.WriteString(fmt.Sprintf("-- 无法解析约束 %s\n", d.Name))
					continue
				}
				b.WriteString(buildMySQLAddConstraint(ref, c))
			} else if differ {
				b.WriteString(fmt.Sprintf("-- TODO: 约束 %s 定义不一致，需人工处理\n", d.Name))
			}
		case "index":
			if srcOnly {
				idx, ok := idxMap[strings.ToLower(d.Name)]
				if !ok {
					b.WriteString(fmt.Sprintf("-- 无法解析索引 %s\n", d.Name))
					continue
				}
				b.WriteString(buildMySQLAddIndex(ref, idx))
			} else if differ {
				idx, ok := idxMap[strings.ToLower(d.Name)]
				if !ok {
					b.WriteString(fmt.Sprintf("-- TODO: 索引 %s 定义不一致且无法解析源定义\n", d.Name))
					continue
				}
				b.WriteString(fmt.Sprintf("ALTER TABLE %s DROP INDEX %s;\n", ref, quoteMySQLIdent(d.Name)))
				b.WriteString(buildMySQLAddIndex(ref, idx))
			}
		case "foreign_key":
			if srcOnly {
				fk, ok := fkMap[strings.ToLower(d.Name)]
				if !ok {
					b.WriteString(fmt.Sprintf("-- 无法解析外键 %s\n", d.Name))
					continue
				}
				b.WriteString(buildMySQLAddFK(ref, fk))
			} else if differ {
				b.WriteString(fmt.Sprintf("-- TODO: 外键 %s 定义不一致，需人工处理\n", d.Name))
			}
		case "trigger":
			if srcOnly {
				t, ok := trigMap[strings.ToLower(d.Name)]
				if !ok {
					b.WriteString(fmt.Sprintf("-- 无法解析触发器 %s\n", d.Name))
					continue
				}
				sqlText := strings.TrimSpace(t.CreateSQL)
				if !strings.HasSuffix(sqlText, ";") {
					sqlText += ";"
				}
				b.WriteString(sqlText + "\n")
			} else if differ {
				b.WriteString(fmt.Sprintf("-- TODO: 触发器 %s 定义不一致，需人工处理\n", d.Name))
			}
		}
	}
	return b.String(), nil
}

func buildMySQLAddConstraint(ref string, c mysqlConstraintInfo) string {
	cols := splitMySQLCols(c.Def)
	switch strings.ToUpper(c.Type) {
	case "PRIMARY KEY":
		return fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s);\n", ref, cols)
	case "UNIQUE":
		return fmt.Sprintf("ALTER TABLE %s ADD UNIQUE %s (%s);\n", ref, quoteMySQLIdent(c.Name), cols)
	case "CHECK":
		return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s);\n", ref, quoteMySQLIdent(c.Name), c.Def)
	default:
		return fmt.Sprintf("-- TODO: 未知约束类型 %s %s\n", c.Type, c.Name)
	}
}

func buildMySQLAddIndex(ref string, idx mysqlIndexInfo) string {
	cols := splitMySQLCols(idx.Columns)
	if idx.Unique {
		return fmt.Sprintf("ALTER TABLE %s ADD UNIQUE INDEX %s (%s);\n", ref, quoteMySQLIdent(idx.Name), cols)
	}
	return fmt.Sprintf("ALTER TABLE %s ADD INDEX %s (%s);\n", ref, quoteMySQLIdent(idx.Name), cols)
}

func buildMySQLAddFK(ref string, fk mysqlFKInfo) string {
	return fmt.Sprintf(
		"ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s) ON UPDATE %s ON DELETE %s;\n",
		ref, quoteMySQLIdent(fk.Name), splitMySQLCols(fk.Columns),
		quoteMySQLIdent(fk.RefTable), splitMySQLCols(fk.RefColumns),
		fk.OnUpdate, fk.OnDelete,
	)
}

func splitMySQLCols(csv string) string {
	parts := strings.Split(csv, ",")
	for i := range parts {
		parts[i] = quoteMySQLIdent(strings.TrimSpace(parts[i]))
	}
	return strings.Join(parts, ", ")
}

func buildMySQLRoutineFixSQL(ctx context.Context, src *sql.DB, item *TableSchemaDiff) (string, error) {
	routines, err := fetchMySQLRoutines(ctx, src)
	if err != nil {
		return "", err
	}
	byKey := map[string]mysqlRoutineInfo{}
	for _, r := range routines {
		byKey[strings.ToUpper(r.Type)+":"+strings.ToLower(r.Name)] = r
	}
	var b strings.Builder
	for _, d := range item.Diffs {
		if d.Kind != "function" {
			continue
		}
		if !(strings.Contains(d.Message, "仅源") || strings.Contains(d.Message, "不一致")) {
			continue
		}
		// Name: "FUNCTION foo" or "PROCEDURE bar"
		fields := strings.Fields(d.Name)
		rtype, name := "FUNCTION", d.Name
		if len(fields) >= 2 {
			rtype, name = strings.ToUpper(fields[0]), fields[1]
		}
		r, ok := byKey[rtype+":"+strings.ToLower(name)]
		if !ok {
			b.WriteString(fmt.Sprintf("-- 无法解析例程 %s\n", d.Name))
			continue
		}
		def := strings.TrimSpace(r.Definition)
		if def == "" {
			b.WriteString(fmt.Sprintf("-- 例程 %s 无定义\n", d.Name))
			continue
		}
		if !strings.HasSuffix(def, ";") {
			def += ";"
		}
		b.WriteString(def + "\n\n")
	}
	return b.String(), nil
}
