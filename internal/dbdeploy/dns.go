package dbdeploy

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/dns"
	"auto-dba/internal/store"
)

const (
	dnsBinaryLocal  = "deploy/coredns/bin/coredns"
	dnsBinaryRemote = "/usr/local/auto-dba/dns/coredns"
	dnsCorefilePath = "/etc/auto-dba/dns/Corefile"
	dnsUnitName     = "auto-dba-coredns.service"
)

func (e *Engine) buildDNSSteps(nodes []store.DbDeployNode) []store.DbDeployChangeStep {
	var steps []store.DbDeployChangeStep
	seq := 0
	add := func(key, title, nodeID string) {
		seq++
		steps = append(steps, store.DbDeployChangeStep{Seq: seq, StepKey: key, Title: title, NodeID: nodeID})
	}
	add("agents_health", "探活全部 Db Agent", "")
	for _, n := range nodes {
		add("precheck_"+n.ID, "预检 "+n.Host, n.ID)
		add("install_"+n.ID, "安装 CoreDNS "+n.Host, n.ID)
		add("put_conf_"+n.ID, "写入 Corefile "+n.Host, n.ID)
		add("systemd_"+n.ID, "安装 systemd "+n.Host, n.ID)
		add("start_"+n.ID, "启动服务 "+n.Host, n.ID)
		add("heartbeat_"+n.ID, "确认心跳 "+n.Host, n.ID)
	}
	add("finalize", "完成", "")
	return steps
}

func (e *Engine) stepDNSPrecheck(ctx context.Context, cl *store.DbDeployCluster, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	_, port, err := splitPort(cl)
	if err != nil {
		return err
	}
	resp, err := e.Agent.Exec(ctx, auth, map[string]any{
		"command": "dns_precheck",
		"payload": map[string]any{"port": port},
		"dry_run": e.Cfg.DryRun,
	})
	if err != nil {
		return err
	}
	if resp != nil && !resp.OK {
		msg := resp.Message
		if msg == "" {
			msg = "预检未通过"
		}
		return fmt.Errorf("%s", msg)
	}
	step.LogText = "precheck ok"
	if resp != nil && resp.Message != "" {
		step.LogText = resp.Message
	}
	return nil
}

func (e *Engine) stepDNSInstall(ctx context.Context, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	raw, err := os.ReadFile(dnsBinaryLocal)
	if err != nil {
		return fmt.Errorf("本机没有 %s，请先执行 scripts/build_coredns.sh", dnsBinaryLocal)
	}
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	_, err = e.Agent.FilePut(ctx, auth, map[string]any{
		"path": dnsBinaryRemote, "content_base64": base64.StdEncoding.EncodeToString(raw), "mode": "0755",
		"change_id": step.ChangeID, "step_id": step.ID,
	})
	if err != nil {
		return err
	}
	step.LogText = dnsBinaryRemote
	return nil
}

func (e *Engine) stepDNSCorefile(ctx context.Context, cl *store.DbDeployCluster, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	opt, err := dns.ParseDeployOptions(cl.HaOptionsJSON)
	if err != nil {
		return err
	}
	body, err := dns.RenderCorefile(opt, e.Cfg.DNSNodeDB, e.Cfg.DNSNodeUser, e.Cfg.DNSNodePassword, n.Host)
	if err != nil {
		return err
	}
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	_, err = e.Agent.FilePut(ctx, auth, map[string]any{
		"path": dnsCorefilePath, "content": body, "mode": "0600",
		"change_id": step.ChangeID, "step_id": step.ID,
	})
	if err != nil {
		return err
	}
	step.LogText = dnsCorefilePath
	return nil
}

func (e *Engine) stepDNSSystemd(ctx context.Context, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	content := `[Unit]
Description=auto-dba CoreDNS
After=network.target

[Service]
ExecStart=` + dnsBinaryRemote + ` -conf ` + dnsCorefilePath + `
Restart=on-failure
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`
	_, err = e.Agent.SystemdInstall(ctx, auth, map[string]any{
		"unit_name": dnsUnitName, "content": content,
		"change_id": step.ChangeID, "step_id": step.ID, "dry_run": e.Cfg.DryRun,
	})
	if err != nil {
		return err
	}
	n.SystemdUnit = dnsUnitName
	_ = e.Store.UpsertDbDeployNode(n)
	step.LogText = dnsUnitName
	return nil
}

func (e *Engine) stepDNSHeartbeat(ctx context.Context, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	if strings.TrimSpace(e.Cfg.DNSDSN) == "" {
		return fmt.Errorf("未配置 dns.dsn，无法确认心跳")
	}
	st, err := dns.Open(e.Cfg.DNSDSN, e.Cfg.DNSNodeUser, e.Cfg.DNSNodePassword, e.Cfg.DNSNodeDB)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.WaitNode(ctx, n.Host, 30*time.Second); err != nil {
		return err
	}
	step.LogText = "coredns 表已出现 " + n.Host
	return nil
}

// DNSNode 由集群部署安装的 DNS 节点。
type DNSNode struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	MetricsPort int    `json:"metrics_port"`
	AgentID     string `json:"-"`
	ClusterID   string `json:"cluster_id"`
	ClusterName string `json:"cluster_name"`
}

// ListDNSNodes 列出全部 DNS 集群的节点。
func (e *Engine) ListDNSNodes() ([]DNSNode, error) {
	cls, err := e.Store.ListDbDeployClusters(store.DbDeployEngineDNS)
	if err != nil {
		return nil, err
	}
	var out []DNSNode
	for i := range cls {
		cl := &cls[i]
		opt, err := dns.ParseDeployOptions(cl.HaOptionsJSON)
		if err != nil {
			continue
		}
		_, port, err := splitPort(cl)
		if err != nil {
			continue
		}
		nodes, err := e.Store.ListDbDeployNodes(cl.ID)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			out = append(out, DNSNode{Host: n.Host, Port: port, MetricsPort: opt.MetricsPort, AgentID: n.AgentID, ClusterID: cl.ID, ClusterName: cl.Name})
		}
	}
	return out, nil
}

// DNSQuery 经节点上的 db-agent 向该节点自身内网 IP 查询，来源 IP 即节点地址。返回 agent 给出的 JSON。
func (e *Engine) DNSQuery(ctx context.Context, host, name, qtype string) (string, error) {
	nodes, err := e.ListDNSNodes()
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		if !strings.EqualFold(n.Host, strings.TrimSpace(host)) {
			continue
		}
		if strings.TrimSpace(n.AgentID) == "" {
			return "", fmt.Errorf("节点 %s 未绑定 Db Agent", n.Host)
		}
		auth, err := e.agentAuthWithPassword(n.AgentID)
		if err != nil {
			return "", err
		}
		resp, err := e.Agent.Exec(ctx, auth, map[string]any{
			"command": "dns_query",
			"payload": map[string]any{"server": net.JoinHostPort(n.Host, strconv.Itoa(n.Port)), "name": name, "type": qtype},
		})
		if err != nil {
			return "", err
		}
		if resp == nil || !resp.OK {
			msg := "节点查询失败"
			if resp != nil && resp.Message != "" {
				msg = resp.Message
			}
			return "", fmt.Errorf("%s", msg)
		}
		return resp.Message, nil
	}
	return "", fmt.Errorf("%s 不是已部署的 DNS 节点", host)
}

func splitPort(cl *store.DbDeployCluster) (string, int, error) {
	opt, err := dns.ParseDeployOptions(cl.HaOptionsJSON)
	if err != nil {
		return "", 0, err
	}
	listen := strings.TrimSpace(opt.Listen)
	i := strings.LastIndex(listen, ":")
	if i <= 0 {
		return "", 0, fmt.Errorf("监听地址须为 host:port")
	}
	var port int
	if _, err := fmt.Sscanf(listen[i+1:], "%d", &port); err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("监听端口无效")
	}
	return listen[:i], port, nil
}
