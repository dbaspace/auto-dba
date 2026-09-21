package flashback

import (
	"fmt"
	"strings"
)

const flashbackBulkInsertWarnRows = 1000

func flashbackPublicError(err error) string {
	if err == nil {
		return ""
	}
	return flashbackPublicErrorMessage(err.Error())
}

func flashbackPublicErrorMessage(msg string) string {
	raw := strings.TrimSpace(msg)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "写入平台任务记录失败：") {
		return raw
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "invalid byte sequence for encoding") {
		return "写入平台任务记录失败：生成的闪回 SQL 含非法 UTF-8 字节，不是目标实例报错"
	}
	if flashbackMySQLDumpTransientMsg(lower) {
		return flashbackMySQLDumpBrokenHint
	}
	if i := strings.Index(raw, "pq:"); i >= 0 {
		detail := strings.TrimSpace(raw[i+3:])
		if detail == "" {
			return "写入平台任务记录失败"
		}
		return "写入平台任务记录失败：" + detail
	}
	return raw
}

func flashbackAddWalMinerLimits(items *[]FlashbackCheckItem, dict *flashbackDictionary, sqlType string) {
	if flashbackWantDDL(sqlType) {
		flashbackAddCheck(items, "scope_dml", "DML 与有限 DDL", flashbackCheckWarning,
			"可解析用户表 INSERT/UPDATE/DELETE，以及能从目录元组反推的 CREATE/DROP TABLE、RENAME、ADD/DROP COLUMN。")
	} else {
		flashbackAddCheck(items, "scope_dml", "仅 DML", flashbackCheckWarning,
			"本次 sql_type 未包含 ddl，只解析 INSERT/UPDATE/DELETE。")
	}
	flashbackAddCheck(items, "dict_snapshot", "数据字典快照", flashbackCheckWarning,
		"任务创建时冻结 schema 到 dict.json，解析只用该快照。")
	if dict == nil {
		return
	}
	var rewritten, dropped, noUnique []string
	for _, rel := range dict.Wanted {
		if rel == nil {
			continue
		}
		q := rel.Schema + "." + rel.Name
		if rel.OID != 0 && rel.RelNode != 0 && rel.OID != rel.RelNode {
			rewritten = append(rewritten, fmt.Sprintf("%s (oid=%d relfilenode=%d)", q, rel.OID, rel.RelNode))
		}
		for _, c := range rel.Columns {
			if c.Dropped {
				dropped = append(dropped, q+"."+c.Name)
			}
		}
		if len(rel.PKCols) == 0 && rel.ReplIdent != "f" && rel.ReplIdent != "i" {
			noUnique = append(noUnique, q)
		}
	}
	if len(rewritten) > 0 {
		flashbackAddCheck(items, "rewrite", "表 rewrite", flashbackCheckWarning, strings.Join(rewritten, "；"))
	}
	if len(dropped) > 0 {
		flashbackAddCheck(items, "dropped_cols", "已删除列", flashbackCheckWarning, strings.Join(dropped, ", "))
	}
	if len(noUnique) > 0 {
		flashbackAddCheck(items, "no_unique", "无唯一键", flashbackCheckWarning, strings.Join(noUnique, ", "))
	}
}
