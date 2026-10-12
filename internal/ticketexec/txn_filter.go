package ticketexec

import (
	"strings"
)

// isDMLTxnControlStatement 是否为事务控制语句（平台已 BeginTx，脚本内勿再执行）。
// 去前导注释/空白后匹配，大小写不敏感，允许尾部分号。
func isDMLTxnControlStatement(stmt string) bool {
	s := stripLeadingComments(stmt)
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ";")
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	u := strings.ToUpper(s)
	switch u {
	case "BEGIN", "BEGIN TRANSACTION", "START TRANSACTION",
		"COMMIT", "COMMIT TRANSACTION",
		"ROLLBACK", "ROLLBACK TRANSACTION", "ABORT":
		return true
	}
	// BEGIN WORK / COMMIT WORK 等
	if strings.HasPrefix(u, "BEGIN ") || strings.HasPrefix(u, "START TRANSACTION") {
		return true
	}
	if strings.HasPrefix(u, "COMMIT ") || strings.HasPrefix(u, "ROLLBACK ") {
		return true
	}
	return false
}

// filterDMLTxnControlStatements 去掉事务控制语句；不执行、不记成功明细。
func filterDMLTxnControlStatements(stmts []string) []string {
	out := make([]string, 0, len(stmts))
	for _, s := range stmts {
		if isDMLTxnControlStatement(s) {
			continue
		}
		if strings.TrimSpace(stripLeadingComments(s)) == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
