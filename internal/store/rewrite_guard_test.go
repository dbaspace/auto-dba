package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoRawDBCallsWithQuestionPlaceholders 防止再出现 d.db.Query/Exec 直接带 ?
// 而未走 rewrite 的写法（Postgres 会报 syntax error near ORDER 等）。
func TestNoRawDBCallsWithQuestionPlaceholders(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var offenders []string
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Base(path) == "store.go" {
			return nil // rewrite 包装本身会调 d.db.*
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// d.db.Query / d.db.Exec / d.db.QueryRow / *Context
			xsel, ok := sel.X.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			xident, ok := xsel.X.(*ast.Ident)
			if !ok || xident.Name != "d" {
				return true
			}
			if xsel.Sel.Name != "db" {
				return true
			}
			name := sel.Sel.Name
			switch name {
			case "Query", "QueryRow", "Exec", "QueryContext", "QueryRowContext", "ExecContext":
			default:
				return true
			}
			// 调用参数文本里是否有占位 ?（且不是 rewrite(...)）
			start := fset.Position(call.Pos()).Offset
			end := fset.Position(call.End()).Offset
			if end > len(src) || start >= end {
				return true
			}
			snippet := string(src[start:end])
			if strings.Contains(snippet, "d.rewrite") {
				return true
			}
			if !strings.Contains(snippet, "?") {
				return true
			}
			// DDL / information_schema 探测通常无绑定占位
			upper := strings.ToUpper(snippet)
			if strings.Contains(upper, "INFORMATION_SCHEMA") {
				return true
			}
			// 真正危险：WHERE/VALUES/SET/LIMIT 旁的 ?
			dangerous := strings.Contains(snippet, "=?") ||
				strings.Contains(snippet, "= ?") ||
				strings.Contains(snippet, "(?") ||
				strings.Contains(snippet, ",?") ||
				strings.Contains(snippet, ", ?") ||
				strings.Contains(snippet, "LIMIT ?") ||
				strings.Contains(snippet, "OFFSET ?")
			if !dangerous {
				return true
			}
			rel, _ := filepath.Rel(dir, path)
			pos := fset.Position(call.Pos())
			offenders = append(offenders, rel+":"+itoa(pos.Line)+" "+name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("found d.db.* calls with ? placeholders (use d.Query/Exec or d.rewrite):\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
