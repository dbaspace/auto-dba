package cdc

import (
	"fmt"
	"strings"
)

// JoinSample 拼接列表，超过 limit 时只保留前若干项并附总数（默认 20）。
func JoinSample(items []string, limit int) string {
	if limit <= 0 {
		limit = 20
	}
	n := len(items)
	if n == 0 {
		return ""
	}
	if n <= limit {
		return strings.Join(items, ",")
	}
	return fmt.Sprintf("%s 等共 %d 项", strings.Join(items[:limit], ","), n)
}
