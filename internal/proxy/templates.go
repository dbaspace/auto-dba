package proxy

import (
	"fmt"
	"strings"
)

// Backend 后端节点（模板用）。
type Backend struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Weight int    `json:"weight,omitempty"`
	Backup bool   `json:"backup,omitempty"`
}

// RenderOptions 生成默认配置。
type RenderOptions struct {
	Name       string      `json:"name"`
	VIP        string      `json:"vip"`
	ListenPort int         `json:"listen_port"`
	Mode       string      `json:"mode"` // tcp | http（haproxy/nginx）
	Backends   []Backend   `json:"backends"`
	LVS        *LVSOptions `json:"lvs,omitempty"` // LVS/Keepalived 专用
}

func DefaultConfPath(kind string) string {
	switch strings.ToLower(kind) {
	case "haproxy":
		return "/etc/haproxy/haproxy.cfg"
	case "nginx":
		return "/etc/nginx/nginx.conf"
	case "lvs":
		return "/etc/keepalived/keepalived.conf"
	default:
		return ""
	}
}

func DefaultUnit(kind string) string {
	switch strings.ToLower(kind) {
	case "haproxy":
		return "haproxy"
	case "nginx":
		return "nginx"
	case "lvs":
		return "keepalived"
	default:
		return ""
	}
}

func DefaultListenPort(kind string) int {
	switch strings.ToLower(kind) {
	case "haproxy", "nginx", "lvs":
		return 3306
	default:
		return 0
	}
}

// RenderDefaultConf 生成可编辑的起步模板。
func RenderDefaultConf(kind string, opt RenderOptions) string {
	name := strings.TrimSpace(opt.Name)
	if name == "" {
		name = "auto-dba"
	}
	port := opt.ListenPort
	if port <= 0 {
		port = DefaultListenPort(kind)
	}
	mode := strings.ToLower(strings.TrimSpace(opt.Mode))
	if mode == "" {
		mode = "tcp"
	}
	backends := opt.Backends
	if len(backends) == 0 {
		backends = []Backend{{Name: "db1", Host: "10.0.0.11", Port: port, Weight: 100}}
	}
	switch strings.ToLower(kind) {
	case "haproxy":
		return renderHAProxy(name, port, mode, backends)
	case "nginx":
		return renderNginx(name, port, mode, backends)
	case "lvs":
		vip := strings.TrimSpace(opt.VIP)
		if vip == "" {
			vip = "10.0.0.100"
		}
		lvsOpt := DefaultLVSOptions(name)
		if opt.LVS != nil {
			lvsOpt = *opt.LVS
		}
		s, err := RenderKeepalivedConf(name, vip, port, backends, lvsOpt)
		if err != nil {
			return "# LVS 模板生成失败: " + err.Error() + "\n"
		}
		return s
	default:
		return "# unsupported kind\n"
	}
}

func renderHAProxy(name string, port int, mode string, backends []Backend) string {
	var b strings.Builder
	b.WriteString("# managed by auto-dba — review before push\n")
	b.WriteString("global\n  log /dev/log local0\n  maxconn 4096\n  daemon\n\n")
	b.WriteString("defaults\n  log global\n  option dontlognull\n  timeout connect 5s\n  timeout client  50s\n  timeout server  50s\n\n")
	if mode == "http" {
		b.WriteString(fmt.Sprintf("frontend fe_%s\n  bind *:%d\n  mode http\n  default_backend be_%s\n\n", name, port, name))
		b.WriteString(fmt.Sprintf("backend be_%s\n  mode http\n  balance roundrobin\n  option httpchk GET /\n", name))
	} else {
		b.WriteString(fmt.Sprintf("frontend fe_%s\n  bind *:%d\n  mode tcp\n  default_backend be_%s\n\n", name, port, name))
		b.WriteString(fmt.Sprintf("backend be_%s\n  mode tcp\n  balance roundrobin\n  option tcp-check\n", name))
	}
	for i, x := range backends {
		n := strings.TrimSpace(x.Name)
		if n == "" {
			n = fmt.Sprintf("s%d", i+1)
		}
		w := x.Weight
		if w <= 0 {
			w = 100
		}
		line := fmt.Sprintf("  server %s %s:%d check weight %d", n, x.Host, x.Port, w)
		if x.Backup {
			line += " backup"
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nlisten stats\n  bind *:8404\n  mode http\n  stats enable\n  stats uri /stats\n")
	return b.String()
}

func renderNginx(name string, port int, mode string, backends []Backend) string {
	var b strings.Builder
	b.WriteString("# managed by auto-dba — review before push\n")
	b.WriteString("worker_processes auto;\nevents { worker_connections 4096; }\n\n")
	if mode == "http" {
		b.WriteString("http {\n  upstream " + name + " {\n")
		for _, x := range backends {
			w := x.Weight
			if w <= 0 {
				w = 1
			}
			b.WriteString(fmt.Sprintf("    server %s:%d weight=%d;\n", x.Host, x.Port, w))
		}
		b.WriteString("  }\n  server {\n    listen " + fmt.Sprintf("%d", port) + ";\n")
		b.WriteString("    location / {\n      proxy_pass http://" + name + ";\n    }\n  }\n}\n")
		return b.String()
	}
	b.WriteString("stream {\n  upstream " + name + " {\n")
	for _, x := range backends {
		w := x.Weight
		if w <= 0 {
			w = 1
		}
		b.WriteString(fmt.Sprintf("    server %s:%d weight=%d;\n", x.Host, x.Port, w))
	}
	b.WriteString("  }\n  server {\n    listen " + fmt.Sprintf("%d", port) + ";\n")
	b.WriteString("    proxy_pass " + name + ";\n  }\n}\n")
	return b.String()
}

// ValidateBin 返回 Agent 侧校验命令名。
func ValidateBin(kind string) (bin string, args []string) {
	switch strings.ToLower(kind) {
	case "haproxy":
		return "haproxy", []string{"-c", "-f"}
	case "nginx":
		return "nginx", []string{"-t", "-c"}
	case "lvs":
		return "keepalived", []string{"-t", "-f"}
	default:
		return "", nil
	}
}
