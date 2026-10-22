package dns

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// RenderCorefile 生成 hdns Corefile。不写 fallthrough，不写 parsing_range_ids。
func RenderCorefile(opt DeployOptions, dbName, dbUser, dbPassword, nodeName string) (string, error) {
	host, port, err := splitListen(opt.Listen)
	if err != nil {
		return "", err
	}
	up := strings.TrimSpace(opt.Upstream)
	if _, _, err := net.SplitHostPort(up); err != nil {
		return "", fmt.Errorf("上游 DNS 须为 host:port")
	}
	dbHost := strings.TrimSpace(opt.DBHost)
	if dbHost == "" {
		return "", fmt.Errorf("解析库地址为空")
	}
	dbPort := opt.DBPort
	if dbPort == 0 {
		dbPort = 5432
	}
	if strings.TrimSpace(dbName) == "" || strings.TrimSpace(dbUser) == "" || dbPassword == "" {
		return "", fmt.Errorf("平台未配置解析库账号")
	}
	name := strings.TrimSpace(nodeName)
	if name == "" {
		name = "dns"
	}
	var b strings.Builder
	fmt.Fprintf(&b, ".:%s {\n", port)
	fmt.Fprintf(&b, "  bind %s\n", host)
	b.WriteString("  hdns {\n")
	fmt.Fprintf(&b, "    host %s\n", quote(dbHost))
	fmt.Fprintf(&b, "    port %d\n", dbPort)
	fmt.Fprintf(&b, "    db %s\n", quote(dbName))
	fmt.Fprintf(&b, "    user %s\n", quote(dbUser))
	fmt.Fprintf(&b, "    password %s\n", quote(dbPassword))
	b.WriteString("    ssl_mode disable\n")
	b.WriteString("    check_intervial 5s\n")
	b.WriteString("    refresh 20m\n")
	fmt.Fprintf(&b, "    marsdns_name %s\n", quote(name))
	b.WriteString("  }\n")
	fmt.Fprintf(&b, "  forward . %s\n", up)
	mp := opt.MetricsPort
	if mp == 0 {
		mp = DefaultMetricsPort
	}
	if mp > 0 {
		fmt.Fprintf(&b, "  prometheus :%d\n", mp)
	}
	b.WriteString("  errors\n")
	b.WriteString("  log\n")
	b.WriteString("  health\n")
	b.WriteString("  ready\n")
	b.WriteString("}\n")
	return b.String(), nil
}

func splitListen(listen string) (host, port string, err error) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		listen = "0.0.0.0:53"
	}
	h, p, err := net.SplitHostPort(listen)
	if err != nil {
		return "", "", fmt.Errorf("监听地址须为 host:port")
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("监听端口无效")
	}
	if h == "" {
		h = "0.0.0.0"
	}
	return h, p, nil
}

func quote(s string) string {
	return strconv.Quote(s)
}

// ParseDeployOptions 读取集群上的部署参数。
func ParseDeployOptions(raw string) (DeployOptions, error) {
	var opt DeployOptions
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &opt); err != nil {
			return opt, fmt.Errorf("DNS 部署参数无效")
		}
	}
	if strings.TrimSpace(opt.Listen) == "" {
		opt.Listen = "0.0.0.0:53"
	}
	if opt.DBPort == 0 {
		opt.DBPort = 5432
	}
	if opt.MetricsPort == 0 {
		opt.MetricsPort = DefaultMetricsPort
	}
	if opt.MetricsPort < 0 || opt.MetricsPort > 65535 {
		return opt, fmt.Errorf("指标端口无效")
	}
	if _, _, err := splitListen(opt.Listen); err != nil {
		return opt, err
	}
	if strings.TrimSpace(opt.Upstream) == "" {
		return opt, fmt.Errorf("请填写上游 DNS")
	}
	if _, _, err := net.SplitHostPort(strings.TrimSpace(opt.Upstream)); err != nil {
		return opt, fmt.Errorf("上游 DNS 须为 host:port")
	}
	if strings.TrimSpace(opt.DBHost) == "" {
		return opt, fmt.Errorf("请填写解析库地址")
	}
	return opt, nil
}

// EncodeMX 按 hdns 的拆分方式保存为 邮件主机|优先级。
func EncodeMX(host string, pref int) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" || strings.Contains(host, "|") {
		return "", fmt.Errorf("邮件主机无效")
	}
	if pref < 0 || pref > 65535 {
		return "", fmt.Errorf("MX 优先级须在 0 到 65535")
	}
	return host + "|" + strconv.Itoa(pref), nil
}

// DecodeMX 把库内 data 拆回邮件主机和优先级。
func DecodeMX(data string) (host string, pref int, ok bool) {
	parts := strings.Split(data, "|")
	if len(parts) != 2 {
		return "", 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return "", 0, false
	}
	return strings.TrimSpace(parts[0]), n, true
}
