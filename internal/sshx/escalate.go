package sshx

import (
	"strings"
)

// 提权模式（对齐现场 sudo su -，非交互）。
const (
	EscalateNone   = ""
	EscalateSudoSu = "sudo_su"
)

// NormalizeEscalate 规范化提权模式。
func NormalizeEscalate(mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	switch m {
	case "", "none", "off", "false", "0":
		return EscalateNone
	case EscalateSudoSu, "sudo-su", "sudo", "su", "root":
		return EscalateSudoSu
	default:
		return EscalateNone
	}
}

// ShellQuote 单引号包装，供远端 sh -c / su -c 使用。
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// WrapEscalate 将命令包装为提权执行；mode 空则原样返回。
// sudo_su 等价非交互：sudo -n su - root -c '<cmdline>'
func WrapEscalate(cmdline, mode string) string {
	cmdline = strings.TrimSpace(cmdline)
	if cmdline == "" {
		return cmdline
	}
	switch NormalizeEscalate(mode) {
	case EscalateSudoSu:
		return "sudo -n su - root -c " + ShellQuote(cmdline)
	default:
		return cmdline
	}
}

// EscalateHint 提权失败时的用户提示。
func EscalateHint(mode string) string {
	if NormalizeEscalate(mode) == EscalateSudoSu {
		return "；请为登录用户配置免密 sudo（NOPASSWD），或关闭「提权执行」"
	}
	return ""
}
