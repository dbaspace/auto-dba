package rediscmd

import (
	"fmt"
	"strings"
)

// SplitArgs 按 redis-cli / sdssplitargs 规则拆分一行命令：
// 空白分隔；支持 "双引号" / '单引号'；双引号内 \n \r \t \b \a \\ \" \xHH 等转义；
// 单引号内 \' \\ 转义，其余字面量（便于 JSON、反斜杠路径、含空格值）。
// 引号未闭合、或闭合后紧跟非空白（如 "foo"bar）返回错误。
func SplitArgs(line string) ([]string, error) {
	p := 0
	n := len(line)
	var out []string

	for {
		for p < n && isRedisSpace(line[p]) {
			p++
		}
		if p >= n {
			return out, nil
		}

		var b strings.Builder
		inq, insq := false, false
		done := false

		for !done {
			if p >= n {
				if inq || insq {
					return nil, fmt.Errorf("引号未闭合")
				}
				done = true
				break
			}
			c := line[p]

			if inq {
				if c == '\\' && p+1 < n {
					if line[p+1] == 'x' && p+3 < n && isHex(line[p+2]) && isHex(line[p+3]) {
						b.WriteByte(hexByte(line[p+2], line[p+3]))
						p += 4
						continue
					}
					p++
					esc := line[p]
					switch esc {
					case 'n':
						b.WriteByte('\n')
					case 'r':
						b.WriteByte('\r')
					case 't':
						b.WriteByte('\t')
					case 'b':
						b.WriteByte('\b')
					case 'a':
						b.WriteByte('\a')
					default:
						b.WriteByte(esc) // 含 \" \\ 及其他
					}
					p++
					continue
				}
				if c == '"' {
					if p+1 < n && !isRedisSpace(line[p+1]) {
						return nil, fmt.Errorf("闭合引号后不能紧跟非空白字符")
					}
					done = true
					p++
					continue
				}
				b.WriteByte(c)
				p++
				continue
			}

			if insq {
				if c == '\\' && p+1 < n && (line[p+1] == '\'' || line[p+1] == '\\') {
					p++
					b.WriteByte(line[p])
					p++
					continue
				}
				if c == '\'' {
					if p+1 < n && !isRedisSpace(line[p+1]) {
						return nil, fmt.Errorf("闭合引号后不能紧跟非空白字符")
					}
					done = true
					p++
					continue
				}
				b.WriteByte(c)
				p++
				continue
			}

			// 非引号
			switch c {
			case ' ', '\n', '\r', '\t':
				done = true
			case '"':
				inq = true
				p++
			case '\'':
				insq = true
				p++
			default:
				b.WriteByte(c)
				p++
			}
		}

		out = append(out, b.String())
	}
}

// ParseLine 拆分命令行；解析失败时返回 nil（兼容旧调用方）。
func ParseLine(line string) []string {
	parts, err := SplitArgs(line)
	if err != nil {
		return nil
	}
	return parts
}

func isRedisSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexByte(a, b byte) byte {
	return byte(hexVal(a)<<4 | hexVal(b))
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return 0
	}
}
