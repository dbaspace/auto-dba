package pgdiag

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	pgNormBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	pgNormLineComment  = regexp.MustCompile(`--[^\n]*`)
	pgNormStringLit    = regexp.MustCompile(`'(?:[^']|'')*'`)
	pgNormNumberLit    = regexp.MustCompile(`\b\d+(\.\d+)?\b`)
	pgNormInList       = regexp.MustCompile(`(?i)\bin\s*\(\s*\?(?:\s*,\s*\?)*\s*\)`)
	pgNormWhitespace   = regexp.MustCompile(`\s+`)
)

// NormalizeQuery 对齐 Hub：去注释 → 字面量替换为 ? → 折叠 IN → 空白/小写 → sha256。
func NormalizeQuery(info string) (normalized string, hash string) {
	s := strings.TrimSpace(info)
	if s == "" {
		return "", ""
	}
	s = pgNormBlockComment.ReplaceAllString(s, " ")
	s = pgNormLineComment.ReplaceAllString(s, " ")
	s = pgNormStringLit.ReplaceAllString(s, "?")
	s = pgNormNumberLit.ReplaceAllString(s, "?")
	s = pgNormInList.ReplaceAllString(s, "in (?)")
	s = pgNormWhitespace.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, ";")
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	if s == "" {
		return "", ""
	}
	sum := sha256.Sum256([]byte(s))
	return s, hex.EncodeToString(sum[:])
}
