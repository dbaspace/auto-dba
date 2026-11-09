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

// WhitelistOKWithOpts 一期 query + 二期 Redis/Kafka。敏感路径先拒绝，白名单写宽也不放行。
func WhitelistOKWithOpts(method, path string, opts WhitelistOpts) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if PATDenied(path) {
		return false
	}
	switch {
	case method == "POST" && path == "/api/v1/query":
		return true
	case method == "GET" && path == "/api/v1/query/catalog":
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
	default:
		_ = opts
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

// PATDenied 这些路径对 PAT 恒拒绝，先于白名单判断。
func PATDenied(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if strings.Contains(path, "/upload") || strings.HasSuffix(path, "/open-terminal") {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/roles/") && strings.HasSuffix(path, "/users") {
		return true
	}
	if patInstanceAccountPath(path) {
		return true
	}
	prefixes := []string{
		"/api/v1/users",
		"/api/v1/auth/me",
		"/api/v1/db-accounts",
		"/api/v1/dcl-grants",
		"/api/v1/nacosadmin",
		"/api/v1/nacos-console",
		"/api/v1/nacos-proxy",
		"/api/v1/settings",
		"/api/v1/global-args",
		"/api/v1/feadmin",
		"/api/v1/dsadmin",
		"/api/v1/k8s",
		"/api/v1/oss-settings",
		"/api/v1/oss-uploads",
		"/api/v1/backup",
		"/api/v1/platform/agents",
		"/api/v1/tickets",
		"/api/v1/create-db",
		"/api/v1/redis-rdb",
	}
	for _, p := range prefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func patInstanceAccountPath(path string) bool {
	const p = "/api/v1/instances"
	if path == p {
		return true
	}
	if !strings.HasPrefix(path, p+"/") {
		return false
	}
	rest := strings.TrimPrefix(path, p+"/")
	return rest != "" && !strings.Contains(rest, "/")
}

// ScriptFileName 判断 Content-Disposition 里的文件名是不是脚本。
func ScriptFileName(disposition string) bool {
	lower := strings.ToLower(strings.TrimSpace(disposition))
	i := strings.Index(lower, "filename=")
	if i < 0 {
		return false
	}
	name := lower[i+len("filename="):]
	if semi := strings.IndexByte(name, ';'); semi >= 0 {
		name = name[:semi]
	}
	name = strings.Trim(name, ` "'`)
	for _, ext := range []string{".mjs", ".js", ".sql", ".txt", ".sh"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
