package copilot

import (
	"regexp"
	"strings"
)

var secretAssign = regexp.MustCompile(`(?i)(password|passwd|secret|api[_-]?key|access[_-]?key|token|kubeconfig|authorization)(\s*[:=]\s*["']?)([^\s"',;]{3,})`)

// Redact 去掉送入模型的凭据。提示词里的「只读」说明不能代替这里。
func Redact(s string) string {
	if s == "" {
		return s
	}
	out := secretAssign.ReplaceAllString(s, `${1}${2}***`)
	out = strings.ReplaceAll(out, "crsr_", "crsr_***")
	return out
}
