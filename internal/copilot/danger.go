package copilot

import (
	"regexp"
	"strings"

	"auto-dba/internal/aiops"
)

var dangerPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(drop|delete|update|insert|truncate|alter|kill|grant|revoke|copy|call)\b`),
	regexp.MustCompile(`(?i)pg_read_file|lo_export|into\s+outfile|dblink`),
}

// DetectDanger 命中写操作或危险语句时返回原因；只读分析返回空。
func DetectDanger(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, re := range dangerPatterns {
		if m := re.FindString(text); m != "" {
			return "命中危险操作: " + m
		}
	}
	if u := aiops.ParseUtterance(text); u.Write {
		return "命中危险操作: " + u.Meaning()
	}
	return ""
}
