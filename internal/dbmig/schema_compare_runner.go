package dbmig

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// RunSchemaCompareTask 后台执行结构对比并写回任务结果。
func RunSchemaCompareTask(st *store.DB, secretKey, taskID string) {
	t, err := st.GetSchemaCompareTask(taskID, false)
	if err != nil || t == nil {
		return
	}
	_ = st.MarkSchemaCompareRunning(taskID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	leftInst, err := st.GetInstance(t.LeftInstanceID)
	if err != nil || leftInst == nil {
		_ = st.FailSchemaCompareTask(taskID, "左侧实例不存在")
		return
	}
	rightInst, err := st.GetInstance(t.RightInstanceID)
	if err != nil || rightInst == nil {
		_ = st.FailSchemaCompareTask(taskID, "右侧实例不存在")
		return
	}
	family, ok := SameEngineFamily(leftInst.DBType, rightInst.DBType)
	if !ok {
		_ = st.FailSchemaCompareTask(taskID, "两侧数据库类型须同属一类")
		return
	}
	engine := "mysql"
	if family == "postgres" {
		engine = "postgres"
	}

	_ = st.UpdateSchemaCompareProgress(taskID, 10, "连接源端实例")
	leftDB, err := OpenInstance(ctx, leftInst, secretKey, t.LeftDB)
	if err != nil {
		_ = st.FailSchemaCompareTask(taskID, "连接源端失败: "+err.Error())
		return
	}
	defer leftDB.Close()

	_ = st.UpdateSchemaCompareProgress(taskID, 20, "连接目标端实例")
	rightDB, err := OpenInstance(ctx, rightInst, secretKey, t.RightDB)
	if err != nil {
		_ = st.FailSchemaCompareTask(taskID, "连接目标端失败: "+err.Error())
		return
	}
	defer rightDB.Close()

	_ = st.UpdateSchemaCompareProgress(taskID, 40, "比对表结构")
	items, err := CompareSchemas(ctx, leftDB, rightDB, SchemaCompareSpec{
		Engine:      engine,
		LeftSchema:  t.LeftSchema,
		RightSchema: t.RightSchema,
		Tables:      t.Tables,
	})
	if err != nil {
		_ = st.FailSchemaCompareTask(taskID, err.Error())
		return
	}

	_ = st.UpdateSchemaCompareProgress(taskID, 70, "生成修复 SQL")
	fixSQL, err := BuildSchemaFixSQL(ctx, leftDB, engine, t.LeftSchema, t.RightSchema, items)
	if err != nil {
		// 对比结果仍保存，修复 SQL 失败不阻断
		fixSQL = "-- 生成修复 SQL 失败: " + err.Error() + "\n"
	}

	match, differ, leftOnly, rightOnly := 0, 0, 0, 0
	fixTables := 0
	for _, it := range items {
		switch it.Status {
		case "match":
			match++
		case "differ":
			differ++
		case "left_only":
			leftOnly++
		case "right_only":
			rightOnly++
		}
		if strings.TrimSpace(it.FixSQL) != "" && !strings.HasPrefix(strings.TrimSpace(it.FixSQL), "-- 仅目标") {
			if strings.Contains(it.FixSQL, "CREATE ") || strings.Contains(it.FixSQL, "ALTER ") {
				fixTables++
			}
		}
	}
	hasFix := strings.Contains(fixSQL, "CREATE ") || strings.Contains(fixSQL, "ALTER ")
	summary := map[string]any{
		"total":           len(items),
		"match":           match,
		"differ":          differ,
		"left_only":       leftOnly,
		"right_only":      rightOnly,
		"engine":          family,
		"has_fix_sql":     hasFix,
		"fix_table_count": fixTables,
	}
	raw, err := json.Marshal(items)
	if err != nil {
		_ = st.FailSchemaCompareTask(taskID, "序列化结果失败: "+err.Error())
		return
	}
	_ = st.UpdateSchemaCompareProgress(taskID, 90, fmt.Sprintf("写入结果（%d 表）", len(items)))
	if err := st.SaveSchemaCompareResult(taskID, summary, string(raw), fixSQL); err != nil {
		_ = st.FailSchemaCompareTask(taskID, "保存结果失败: "+err.Error())
	}
}

// SchemaCompareExportCSV 将任务结果导出为 CSV（含 BOM）。
func SchemaCompareExportCSV(items []TableSchemaDiff) []byte {
	var b strings.Builder
	b.WriteString("\uFEFF")
	b.WriteString("table,status,message,diff_kind,diff_name,left,right,diff_message,fix_sql\n")
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `"`, `""`)
		if strings.ContainsAny(s, ",\"\n\r") {
			return `"` + s + `"`
		}
		return s
	}
	for _, it := range items {
		if len(it.Diffs) == 0 {
			b.WriteString(strings.Join([]string{
				esc(it.Table), esc(it.Status), esc(it.Message), "", "", "", "", "", esc(it.FixSQL),
			}, ","))
			b.WriteByte('\n')
			continue
		}
		for i, d := range it.Diffs {
			fix := ""
			if i == 0 {
				fix = it.FixSQL
			}
			b.WriteString(strings.Join([]string{
				esc(it.Table), esc(it.Status), esc(it.Message),
				esc(d.Kind), esc(d.Name), esc(d.Left), esc(d.Right), esc(d.Message), esc(fix),
			}, ","))
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}
