package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dns"
	"auto-dba/internal/promclient"
)

type resolveBody struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	SourceIP string `json:"source_ip"`
	Node     string `json:"node"`
}

// Simulate POST /dns/resolve/simulate 按 hdns 规则推算应答。
func (h *DNSHandler) Simulate(c *gin.Context) {
	var body resolveBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	snap, err := st.Snapshot(c.Request.Context(), true)
	if err != nil {
		h.fail(c, err)
		return
	}
	res, err := dns.Simulate(snap, body.Name, body.Type, body.SourceIP)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// nodeServer 节点的 DNS 服务地址 ip:port。
func (h *DNSHandler) nodeServer(node string) (string, error) {
	node = strings.TrimSpace(node)
	if node == "" {
		return "", fmt.Errorf("请选择节点")
	}
	if _, _, err := net.SplitHostPort(node); err == nil {
		return node, nil
	}
	if h.Engine != nil {
		if ns, err := h.Engine.ListDNSNodes(); err == nil {
			for _, n := range ns {
				if strings.EqualFold(n.Host, node) {
					return net.JoinHostPort(n.Host, strconv.Itoa(n.Port)), nil
				}
			}
		}
	}
	return net.JoinHostPort(node, "53"), nil
}

// Probe POST /dns/resolve/probe 平台直接向节点查询，来源 IP 为平台本机。
func (h *DNSHandler) Probe(c *gin.Context) {
	var body resolveBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	server, err := h.nodeServer(body.Node)
	if err != nil {
		h.fail(c, err)
		return
	}
	res := dns.Probe(c.Request.Context(), server, body.Name, body.Type)
	c.JSON(http.StatusOK, gin.H{"data": res, "source_ip": localIPFor(server)})
}

// NodeQuery POST /dns/resolve/node 经节点 db-agent 在节点本机查询，来源 IP 为节点地址。
func (h *DNSHandler) NodeQuery(c *gin.Context) {
	var body resolveBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	if h.Engine == nil {
		h.fail(c, fmt.Errorf("集群部署未启用"))
		return
	}
	host := strings.TrimSpace(body.Node)
	if hh, _, err := net.SplitHostPort(host); err == nil {
		host = hh
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	raw, err := h.Engine.DNSQuery(ctx, host, body.Name, body.Type)
	if err != nil {
		h.fail(c, err)
		return
	}
	var res dns.ProbeResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		h.fail(c, fmt.Errorf("节点返回无法解析：%s", raw))
		return
	}
	if res.Answers == nil {
		res.Answers = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"data": res, "source_ip": host})
}

// localIPFor 平台访问 server 时使用的本机地址（UDP 不实际发包）。
func localIPFor(server string) string {
	conn, err := net.DialTimeout("udp", server, time.Second)
	if err != nil {
		return ""
	}
	defer conn.Close()
	if a, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return ""
}

const (
	dnsMetricsSelectorArg  = "dns_metrics_selector"
	dnsServfailRatioArg    = "dns_servfail_ratio"
	dnsForwardFailRatioArg = "dns_forward_fail_ratio"
	dnsMonitorJobName      = "auto-dba-coredns"
)

func (h *DNSHandler) promClient() *promclient.Client {
	if h.Store == nil {
		return nil
	}
	base, _ := h.Store.GetGlobalArg("awr_prometheus_base_url")
	if strings.TrimSpace(base) == "" {
		return nil
	}
	token, _ := h.Store.GetGlobalArg("awr_prometheus_auth_token")
	return promclient.New(promclient.Config{BaseURL: base, AuthToken: token, Timeout: promclient.DefaultTimeout})
}

// metricsSelector CoreDNS 指标的标签选择器，缺省按监控任务名匹配 job。
func (h *DNSHandler) metricsSelector() string {
	if h.Store != nil {
		if v, _ := h.Store.GetGlobalArg(dnsMetricsSelectorArg); strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return `job="` + dnsMonitorJobName + `"`
}

type metricSeries struct {
	Label  string             `json:"label"`
	Points []promclient.Point `json:"points"`
}

// Metrics GET /dns/metrics?minutes=60 节点页的 QPS、P99、应答码与上游转发。
func (h *DNSHandler) Metrics(c *gin.Context) {
	cli := h.promClient()
	if cli == nil {
		c.JSON(http.StatusOK, gin.H{"configured": false, "message": "未配置全局参数 awr_prometheus_base_url"})
		return
	}
	minutes, _ := strconv.Atoi(c.DefaultQuery("minutes", "60"))
	if minutes <= 0 || minutes > 7*24*60 {
		minutes = 60
	}
	end := time.Now()
	start := end.Add(-time.Duration(minutes) * time.Minute)
	step := time.Duration(minutes) * time.Minute / 120
	if step < 15*time.Second {
		step = 15 * time.Second
	}
	sel := h.metricsSelector()
	queries := map[string]struct {
		q     string
		label string
	}{
		"qps":     {`sum by (instance) (rate(coredns_dns_requests_total{` + sel + `}[5m]))`, "instance"},
		"p99":     {`histogram_quantile(0.99, sum by (instance, le) (rate(coredns_dns_request_duration_seconds_bucket{` + sel + `}[5m])))`, "instance"},
		"rcode":   {`sum by (rcode) (rate(coredns_dns_responses_total{` + sel + `}[5m]))`, "rcode"},
		"forward": {`sum by (to) (rate(coredns_forward_requests_total{` + sel + `}[5m]))`, "to"},
	}
	out := gin.H{"configured": true, "selector": sel}
	errs := map[string]string{}
	for key, q := range queries {
		samples, err := cli.QueryRange(c.Request.Context(), q.q, start, end, step)
		if err != nil {
			errs[key] = err.Error()
			out[key] = []metricSeries{}
			continue
		}
		series := make([]metricSeries, 0, len(samples))
		for _, s := range samples {
			series = append(series, metricSeries{Label: s.Metric[q.label], Points: promclient.SampleToPoints(s)})
		}
		out[key] = series
	}
	out["errors"] = errs
	c.JSON(http.StatusOK, out)
}
