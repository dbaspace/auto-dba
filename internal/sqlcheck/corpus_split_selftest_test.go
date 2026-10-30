package sqlcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type corpusLabeled struct {
	DBType       string `json:"db_type"`
	IssueType    string `json:"issue_type"`
	RawStatement string `json:"raw_statement"`
}

type corpusPortRow struct {
	Msg  string `json:"msg"`
	Port int    `json:"port"`
}

func repoDist(name string) string {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	return filepath.Join(root, "dist", name)
}

func looksCreateDBWith(sql string) bool {
	u := strings.ToUpper(sql)
	return strings.Contains(u, "CREATE DATABASE") && strings.Contains(u, "WITH")
}

func looksAlterDropColNewline(sql string) bool {
	// ALTER TABLE ... \n ... DROP COLUMN（生产误拆形态）
	if !strings.Contains(strings.ToUpper(sql), "ALTER TABLE") {
		return false
	}
	if !strings.Contains(strings.ToUpper(sql), "DROP COLUMN") {
		return false
	}
	return strings.Contains(sql, "\n")
}

func assertNoFalseSplit(t *testing.T, tag, sql string) {
	t.Helper()
	parts := SplitSQL(sql)
	miss := findMissingSemicolonSplits(sql)
	if looksCreateDBWith(sql) || looksAlterDropColNewline(sql) {
		if len(miss) > 0 {
			t.Errorf("%s still missSemi=%v sql=%q", tag, miss, truncRunes(sql, 120))
		}
		// CREATE DATABASE WITH / 单条 ALTER DROP 应保持 1 段；多语句工单可能 >1，但不应在 WITH/DROP COLUMN 边界 missSemi
		if looksCreateDBWith(sql) && !strings.Contains(sql, ";") {
			// 无分号的单句建库
			if len(parts) != 1 {
				t.Errorf("%s CREATE DATABASE WITH split into %d: %#v", tag, len(parts), parts)
			}
		}
	}
}

func truncRunes(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func TestCorpusJSON_SplitFalsePositivesFixed(t *testing.T) {
	labeledPath := repoDist("dql_Ta5f202f071bc_20261030_114205.json")
	portPath := repoDist("dql_T6e8f62c26844_20261030_113209.json")
	if _, err := os.Stat(labeledPath); err != nil {
		t.Skip("labeled corpus missing:", labeledPath)
	}
	if _, err := os.Stat(portPath); err != nil {
		t.Skip("port corpus missing:", portPath)
	}

	var labeled []corpusLabeled
	b, err := os.ReadFile(labeledPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &labeled); err != nil {
		t.Fatal(err)
	}

	var createDBWith, alterDropNL, missOnCreateDBWith, missOnAlterDrop int
	var semiNoiseCreateDB, ticketNoiseCreateDB int
	rulesSemi := []Rule{
		{Name: "SQLRequireSemicolon", Value: "true"},
		{Name: "DDLForbidCreateDatabase", Value: `{"allow":true}`},
		{Name: "DDLDatabaseNamePrefix", Value: `{"prefix":""}`},
		{Name: "DDLAllowDropColumn", Value: `{"allow":true}`},
	}

	for i, r := range labeled {
		dt := strings.ToLower(strings.TrimSpace(r.DBType))
		if dt != "postgres" && dt != "starrocks" && dt != "pg" && dt != "postgresql" && dt != "sr" {
			continue
		}
		sql := r.RawStatement
		tag := "Ta5f#" + itoa(i)
		if looksCreateDBWith(sql) {
			createDBWith++
			miss := findMissingSemicolonSplits(sql)
			if len(miss) > 0 {
				missOnCreateDBWith++
				t.Errorf("%s CREATE DATABASE WITH missSemi=%v", tag, miss)
			}
			issue := strings.ToLower(strings.TrimSpace(r.IssueType))
			if issue == "" {
				issue = "ddl"
			}
			res := CheckTicketWithRules(sql, issue, normalizeEngine(dt), rulesSemi)
			for _, row := range res.Statements {
				if row.RuleID == "SQLRequireSemicolon" {
					semiNoiseCreateDB++
					t.Errorf("%s SQLRequireSemicolon on CREATE DATABASE WITH", tag)
				}
				if row.RuleID == "TicketType" && strings.Contains(strings.ToUpper(row.SQL), "WITH ENCODING") {
					ticketNoiseCreateDB++
					t.Errorf("%s TicketType on WITH ENCODING fragment", tag)
				}
			}
		}
		if looksAlterDropColNewline(sql) {
			alterDropNL++
			// 仅当整段是「单条 ALTER」（按分号拆后每段）检查 missSemi 边界
			for _, part := range SplitSQL(sql) {
				if !looksAlterDropColNewline(part) && !strings.Contains(strings.ToUpper(part), "DROP COLUMN") {
					continue
				}
				// 用原文找 miss：若 miss 摘要含 DROP COLUMN 且左侧是 ALTER TABLE 名，则仍是误判
			}
			miss := findMissingSemicolonSplits(sql)
			for _, m := range miss {
				if strings.Contains(m, "DROP COLUMN") && strings.Contains(m, "ALTER TABLE") {
					missOnAlterDrop++
					t.Errorf("%s ALTER/DROP COLUMN missSemi=%s", tag, m)
				}
			}
		}
		assertNoFalseSplit(t, tag, sql)
	}

	t.Logf("Ta5f pg/sr: createDBWith=%d missOnCreateDBWith=%d alterDropNL=%d missOnAlterDropBoundary=%d semiNoise=%d ticketNoise=%d",
		createDBWith, missOnCreateDBWith, alterDropNL, missOnAlterDrop, semiNoiseCreateDB, ticketNoiseCreateDB)
	if missOnCreateDBWith > 0 || missOnAlterDrop > 0 || semiNoiseCreateDB > 0 || ticketNoiseCreateDB > 0 {
		t.Fatalf("labeled corpus still has split false positives")
	}

	// 大语料：抽样全量扫描 port 5432/9030，统计误判形态
	var big []corpusPortRow
	b2, err := os.ReadFile(portPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b2, &big); err != nil {
		t.Fatal(err)
	}
	var n, cdb, cdbMiss, adrop, adropMiss int
	for _, r := range big {
		if r.Port != 5432 && r.Port != 9030 {
			continue
		}
		n++
		sql := r.Msg
		if looksCreateDBWith(sql) {
			cdb++
			if len(findMissingSemicolonSplits(sql)) > 0 {
				// 仅统计「CREATE DATABASE … ←缺少;→ WITH」形态
				for _, m := range findMissingSemicolonSplits(sql) {
					if strings.Contains(m, "CREATE DATABASE") && strings.Contains(m, "WITH") {
						cdbMiss++
						if cdbMiss <= 3 {
							t.Errorf("T6e8 CREATE DATABASE WITH missSemi=%s", m)
						}
						break
					}
				}
			}
		}
		if looksAlterDropColNewline(sql) {
			adrop++
			for _, m := range findMissingSemicolonSplits(sql) {
				if strings.Contains(m, "ALTER TABLE") && strings.Contains(m, "DROP COLUMN") {
					adropMiss++
					if adropMiss <= 3 {
						t.Errorf("T6e8 ALTER/DROP COLUMN missSemi=%s", m)
					}
					break
				}
			}
		}
	}
	t.Logf("T6e8 pg/sr rows=%d createDBWith=%d miss=%d alterDropNL=%d miss=%d", n, cdb, cdbMiss, adrop, adropMiss)
	if cdbMiss > 0 || adropMiss > 0 {
		t.Fatalf("port corpus still has split false positives: createDBWithMiss=%d alterDropMiss=%d", cdbMiss, adropMiss)
	}
}

func normalizeEngine(dt string) string {
	switch strings.ToLower(dt) {
	case "postgres", "postgresql", "pg":
		return "postgres"
	case "starrocks", "sr":
		return "starrocks"
	default:
		return dt
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [16]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}
