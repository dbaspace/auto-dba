package copilot

import (
	"strings"

	"auto-dba/internal/dbprobe"
)

const copilotSQLMaxStatements = 1

// CheckCopilotSQL 只允许单条只读语句，并拒绝文件导出与跨库函数。
func CheckCopilotSQL(raw string) (string, error) {
	stmts, err := dbprobe.SplitReadOnlyStatements(raw, copilotSQLMaxStatements)
	if err != nil {
		return "", err
	}
	sqlText := strings.TrimSpace(stmts[0])
	low := strings.ToLower(sqlText)
	for _, bad := range []string{"pg_read_file", "lo_export", "into outfile", "dblink", "copy ", " pg_read_binary_file"} {
		if strings.Contains(low, bad) {
			return "", errDangerSQL(bad)
		}
	}
	return sqlText, nil
}

type dangerSQLError string

func (e dangerSQLError) Error() string { return "Copilot 禁止 " + string(e) }

func errDangerSQL(s string) error { return dangerSQLError(strings.TrimSpace(s)) }
