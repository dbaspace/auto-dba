package handler

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbprobe"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type ticketExportReq struct {
	FileFormat string `json:"file_format"`
}

var reFromTable = regexp.MustCompile(`(?i)\bfrom\s+(.+?)(?:\s+(?:as|where|limit|order|group|having|join|left|right|inner|outer|on|union)|;|$)`)

func (h *TicketHandler) Export(c *gin.Context) {
	var req ticketExportReq
	_ = c.ShouldBindJSON(&req)
	format := strings.ToLower(strings.TrimSpace(req.FileFormat))
	if format == "" {
		format = strings.ToLower(strings.TrimSpace(c.Query("file_format")))
	}
	switch format {
	case "csv", "json", "sql":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "导出格式仅支持 csv / json / sql"})
		return
	}

	it, err := h.Store.GetIssue(c.Param("id"))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "工单不存在"})
		return
	}
	if it.IssueType != store.IssueTypeDQL {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅数据导出工单支持下载"})
		return
	}
	if it.Status != store.StatusApproved && it.Status != store.StatusExecuted {
		c.JSON(http.StatusBadRequest, gin.H{"message": "工单通过后才可下载"})
		return
	}
	user := middleware.Username(c)
	if !strings.EqualFold(strings.TrimSpace(it.Submitter), user) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅限工单发起人可下载"})
		return
	}

	var rec *store.Database
	if strings.TrimSpace(it.DatabaseID) != "" {
		rec, err = h.Store.GetDatabase(it.DatabaseID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	if rec == nil && it.InstanceID != "" && it.DBName != "" {
		rec, err = h.Store.FindDatabase(it.InstanceID, it.DBName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	if rec == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "库未登记，无法导出"})
		return
	}
	inst, err := h.Store.GetInstance(rec.InstanceID)
	if err != nil || inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "所属实例不存在"})
		return
	}
	password, err := secret.Decrypt(h.Secret, inst.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例密码解密失败"})
		return
	}

	limit := dbprobe.DefaultExportLimit
	if extra := parseDQLExtra(it.NeedRollback); extra != nil && extra.RowCount > 0 {
		limit = extra.RowCount + 100
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	res, err := dbprobe.ExecExport(ctx, inst, password, rec.DBName, it.SQLContent, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	body, ext, ctype := encodeExport(res, format, exportTableName(it.SQLContent), strings.ToLower(inst.DBType))
	name := fmt.Sprintf("dql_%s_%s.%s", sanitizeFilePart(it.TicketNo), time.Now().Format("20060102_150405"), ext)
	if it.Status == store.StatusApproved {
		_ = h.Store.UpdateIssueStatus(it.ID, store.StatusExecuted, "dql export "+format)
	}
	c.Header("Content-Disposition", `attachment; filename="`+name+`"; filename*=UTF-8''`+url.QueryEscape(name))
	c.Data(http.StatusOK, ctype, body)
}

type dqlExtra struct {
	RowCount int `json:"row_count"`
}

func parseDQLExtra(raw string) *dqlExtra {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw[0] != '{' {
		return nil
	}
	var extra dqlExtra
	if json.Unmarshal([]byte(raw), &extra) != nil {
		return nil
	}
	return &extra
}

func encodeExport(res *dbprobe.QueryResult, format, table, dbType string) ([]byte, string, string) {
	if res == nil {
		res = &dbprobe.QueryResult{}
	}
	switch format {
	case "json":
		return encodeExportJSON(res), "json", "application/json; charset=utf-8"
	case "sql":
		return encodeExportSQL(res, table, dbType), "sql", "application/sql; charset=utf-8"
	default:
		return encodeExportCSV(res), "csv", "text/csv; charset=utf-8"
	}
}

func encodeExportCSV(res *dbprobe.QueryResult) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	_ = w.Write(res.Columns)
	for _, row := range res.Rows {
		rec := make([]string, len(res.Columns))
		for i := range res.Columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			rec[i] = cellString(v)
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return buf.Bytes()
}

func encodeExportJSON(res *dbprobe.QueryResult) []byte {
	out := make([]map[string]any, 0, len(res.Rows))
	for _, row := range res.Rows {
		m := make(map[string]any, len(res.Columns))
		for i, col := range res.Columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			m[col] = v
		}
		out = append(out, m)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return b
}

func encodeExportSQL(res *dbprobe.QueryResult, table, dbType string) []byte {
	if table == "" {
		table = "export_result"
	}
	var b strings.Builder
	b.WriteString("-- auto-dba dql export\n")
	if len(res.Columns) == 0 {
		return []byte(b.String())
	}
	cols := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		cols[i] = quoteIdent(c, dbType)
	}
	ident := quoteIdent(table, dbType)
	if len(res.Rows) == 0 {
		b.WriteString(fmt.Sprintf("-- no rows\n-- INSERT INTO %s (%s) VALUES ();\n", ident, strings.Join(cols, ", ")))
		return []byte(b.String())
	}
	for _, row := range res.Rows {
		vals := make([]string, len(res.Columns))
		for i := range res.Columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			vals[i] = sqlLiteral(v)
		}
		b.WriteString("INSERT INTO ")
		b.WriteString(ident)
		b.WriteString(" (")
		b.WriteString(strings.Join(cols, ", "))
		b.WriteString(") VALUES (")
		b.WriteString(strings.Join(vals, ", "))
		b.WriteString(");\n")
	}
	return []byte(b.String())
}

func exportTableName(sqlText string) string {
	m := reFromTable.FindStringSubmatch(sqlText)
	if len(m) < 2 {
		return "export_result"
	}
	parts := strings.Split(m[1], ".")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), "`\"'[]")
	}
	name := strings.Join(parts, ".")
	if name == "" || strings.ContainsAny(name, " \t\n()=") {
		return "export_result"
	}
	return name
}

func quoteIdent(name, dbType string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "`", "")
	if dbType == "postgres" || dbType == "postgresql" {
		return `"` + strings.ReplaceAll(name, `.`, `"."`) + `"`
	}
	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		for i, p := range parts {
			parts[i] = "`" + p + "`"
		}
		return strings.Join(parts, ".")
	}
	return "`" + name + "`"
}

func sqlLiteral(v any) string {
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case bool:
		if t {
			return "TRUE"
		}
		return "FALSE"
	case int, int32, int64, float32, float64, json.Number:
		return fmt.Sprint(t)
	default:
		s := cellString(v)
		s = strings.ReplaceAll(s, `'`, `''`)
		return "'" + s + "'"
	}
}

func cellString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprint(t)
	}
}

func sanitizeFilePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "export"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
