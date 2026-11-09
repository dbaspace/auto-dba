package sqlcheck

import (
	"regexp"
	"sort"
	"strings"
)

var (
	reCreateFunctionStmt = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\b`)
	reCreateTriggerStmt  = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:CONSTRAINT\s+)?TRIGGER\b`)
	reExecuteFunction    = regexp.MustCompile(`(?is)\bEXECUTE\s+(?:FUNCTION|PROCEDURE)\s+`)
)

// OrderFunctionsBeforeTriggers 把触发器引用、且首次定义写在该触发器之后的 CREATE FUNCTION
// 挪到第一个引用它的触发器之前。函数已在前面、或脚本里没有该函数时保持原顺序。
// public.update_modify_time 与 update_modify_time 视为同一个函数。
func OrderFunctionsBeforeTriggers(stmts []string) []string {
	if len(stmts) < 2 {
		return stmts
	}
	defs := map[string][]int{}
	firstUse := map[string]int{}
	for i, stmt := range stmts {
		if name, ok := createFunctionName(stmt); ok {
			defs[name] = append(defs[name], i)
			continue
		}
		for _, name := range triggerFunctionNames(stmt) {
			if _, seen := firstUse[name]; !seen {
				firstUse[name] = i
			}
		}
	}
	if len(firstUse) == 0 || len(defs) == 0 {
		return stmts
	}
	skip := map[int]bool{}
	insertBefore := map[int][]int{}
	for name, useAt := range firstUse {
		move := functionDefToAdvance(defs[name], useAt)
		if move < 0 {
			continue
		}
		skip[move] = true
		insertBefore[useAt] = append(insertBefore[useAt], move)
	}
	if len(skip) == 0 {
		return stmts
	}
	for at := range insertBefore {
		sort.Ints(insertBefore[at])
	}
	out := make([]string, 0, len(stmts))
	for i, stmt := range stmts {
		for _, j := range insertBefore[i] {
			out = append(out, stmts[j])
		}
		if !skip[i] {
			out = append(out, stmt)
		}
	}
	return out
}

// functionDefToAdvance 返回应提前的定义下标。前面已有定义，或脚本里没有定义时返回 -1。
func functionDefToAdvance(idxs []int, useAt int) int {
	move := -1
	for _, idx := range idxs {
		if idx < useAt {
			return -1
		}
		if idx > useAt && move < 0 {
			move = idx
		}
	}
	return move
}

func createFunctionName(stmt string) (string, bool) {
	s := stripSQLComments(stmt)
	loc := reCreateFunctionStmt.FindStringIndex(s)
	if loc == nil {
		return "", false
	}
	return funcNameAt(s[loc[1]:])
}

func triggerFunctionNames(stmt string) []string {
	s := stripSQLComments(stmt)
	if reCreateTriggerStmt.FindStringIndex(s) == nil {
		return nil
	}
	var names []string
	rest := s
	seen := map[string]bool{}
	for {
		loc := reExecuteFunction.FindStringIndex(rest)
		if loc == nil {
			break
		}
		name, ok := funcNameAt(rest[loc[1]:])
		rest = rest[loc[1]:]
		if !ok || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// funcNameAt 取函数名的最后一段并转成小写。schema.fn 与 fn 得到同一个键。
func funcNameAt(s string) (string, bool) {
	s = strings.TrimSpace(s)
	first, rest, ok := parseSQLIdent(s)
	if !ok || first == "" {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, ".") {
		second, _, ok2 := parseSQLIdent(rest[1:])
		if !ok2 || second == "" {
			return "", false
		}
		return second, true
	}
	return first, true
}

func parseSQLIdent(s string) (ident, rest string, ok bool) {
	i := 0
	for i < len(s) && isSQLSpace(s[i]) {
		i++
	}
	if i >= len(s) {
		return "", s, false
	}
	if s[i] == '"' {
		i++
		var b strings.Builder
		for i < len(s) {
			if s[i] == '"' {
				if i+1 < len(s) && s[i+1] == '"' {
					b.WriteByte('"')
					i += 2
					continue
				}
				i++
				return strings.ToLower(b.String()), s[i:], true
			}
			b.WriteByte(s[i])
			i++
		}
		return "", s, false
	}
	if !isIdentStart(s[i]) {
		return "", s, false
	}
	start := i
	i++
	for i < len(s) && isIdentCont(s[i]) {
		i++
	}
	return strings.ToLower(s[start:i]), s[i:], true
}

func isSQLSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\t' || c == '\r' || c == '\f'
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '$'
}
