package clientaccess

import (
	"strings"
)

// ExemptPrefixes 项目间/内置助手等路径：JWT 不做机器通道强制。
var ExemptPrefixes = []string{
	"/api/v1/feadmin/",
	"/api/v1/dsadmin/",
	"/api/v1/copilot/",
	"/api/v1/k8s/",
	"/api/v1/auth/",
	"/api/v1/settings/",
}

// MachineClientTypes 视为机器通道的 X-Client-Type / PAT.client_type。
var MachineClientTypes = map[string]bool{
	"mcp":      true,
	"cli":      true,
	"local_ai": true,
	"machine":  true,
}

// WhitelistOpts 白名单扩展选项（二期）。
type WhitelistOpts struct {
	AllowCredential bool // PAT 含 credential scope 时允许看密接口
}

// IsExemptPath 是否豁免机器通道门禁（仅对非 PAT 的 JWT）。
func IsExemptPath(path string) bool {
	path = strings.TrimSpace(path)
	for _, p := range ExemptPrefixes {
		if path == strings.TrimSuffix(p, "/") || strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// IsMachineClientType 是否机器客户端类型。
func IsMachineClientType(t string) bool {
	return MachineClientTypes[strings.ToLower(strings.TrimSpace(t))]
}

// WhitelistOK 默认白名单（无 credential）。
func WhitelistOK(method, path string) bool {
	return WhitelistOKWithOpts(method, path, WhitelistOpts{})
}

// WhitelistOKWithOpts 一期 query + 二期 Redis/Kafka；可选 credential。
func WhitelistOKWithOpts(method, path string, opts WhitelistOpts) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	switch {
	case method == "POST" && path == "/api/v1/query":
		return true
	case method == "POST" && path == "/api/v1/query/table-info":
		return true
	case method == "GET" && path == "/api/v1/query/favorites":
		return true
	case method == "GET" && path == "/api/v1/query/history":
		return true
	case method == "GET" && strings.HasPrefix(path, "/api/v1/databases/") && strings.HasSuffix(path, "/tables"):
		rest := strings.TrimPrefix(path, "/api/v1/databases/")
		rest = strings.TrimSuffix(rest, "/tables")
		return rest != "" && !strings.Contains(rest, "/")
	// 二期：Redis 命令查询
	case method == "POST" && strings.HasPrefix(path, "/api/v1/instances/") && strings.HasSuffix(path, "/redis/execute"):
		return instanceSubPath(path, "/redis/execute")
	// 二期：Kafka 拉消息（只读查询）
	case method == "GET" && strings.HasPrefix(path, "/api/v1/instances/") && strings.Contains(path, "/kafka/topics/") && strings.HasSuffix(path, "/messages"):
		return true
	case method == "GET" && strings.HasPrefix(path, "/api/v1/instances/") && strings.HasSuffix(path, "/kafka/topics"):
		return instanceSubPath(path, "/kafka/topics")
	// 可选：看密（须 PAT credential scope）
	case opts.AllowCredential && method == "GET" && strings.HasPrefix(path, "/api/v1/db-accounts/"):
		rest := strings.TrimPrefix(path, "/api/v1/db-accounts/")
		return rest != "" && !strings.Contains(rest, "/")
	default:
		return false
	}
}

func instanceSubPath(path, suffix string) bool {
	if !strings.HasSuffix(path, suffix) {
		return false
	}
	mid := strings.TrimPrefix(path, "/api/v1/instances/")
	mid = strings.TrimSuffix(mid, suffix)
	return mid != "" && !strings.Contains(mid, "/")
}
