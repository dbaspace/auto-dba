package k8s

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const DefaultTimeout = 15 * time.Second

// AccessMode 接入方式。
const (
	AccessDirect = "direct"
	AccessAgent  = "agent"
)

// AuthType 认证方式。
const (
	AuthKubeconfig = "kubeconfig"
	AuthToken      = "token"
)

// ClusterCred 解密后的集群凭据（内存中使用，不落库）。
type ClusterCred struct {
	AccessMode         string
	AuthType           string
	APIServer          string
	Credential         string // kubeconfig YAML 或 token
	CAData             string
	ImpersonateEnabled bool
	ImpersonateUser    string
	ImpersonateGroups  []string
	AgentID            string
}

// Client 单个集群的客户端集合。
type Client struct {
	ID        string
	Config    *rest.Config
	Clientset kubernetes.Interface
	Dynamic   dynamic.Interface
	Discovery discovery.DiscoveryInterface
}

// ClusterManager 按 clusterID 缓存客户端；凭据变更时 Invalidate。
type ClusterManager struct {
	mu       sync.RWMutex
	clients  map[string]*Client
	timeout  time.Duration
	dialers  map[string]http.RoundTripper // agent 隧道 transport
	buildCfg func(cred ClusterCred) (*rest.Config, error)
}

func NewClusterManager() *ClusterManager {
	m := &ClusterManager{
		clients: make(map[string]*Client),
		timeout: DefaultTimeout,
		dialers: make(map[string]http.RoundTripper),
	}
	m.buildCfg = m.defaultBuildConfig
	return m
}

func (m *ClusterManager) SetTimeout(d time.Duration) {
	if d > 0 {
		m.timeout = d
	}
}

// SetAgentTransport 为 agent 接入模式注册 RoundTripper。
func (m *ClusterManager) SetAgentTransport(agentID string, rt http.RoundTripper) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt == nil {
		delete(m.dialers, agentID)
	} else {
		m.dialers[agentID] = rt
	}
}

// Invalidate 清除缓存。
func (m *ClusterManager) Invalidate(clusterID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clients, clusterID)
}

func (m *ClusterManager) InvalidateAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients = make(map[string]*Client)
}

// Get 获取或构建客户端。
func (m *ClusterManager) Get(clusterID string, cred ClusterCred) (*Client, error) {
	m.mu.RLock()
	if c, ok := m.clients[clusterID]; ok && c != nil {
		m.mu.RUnlock()
		return c, nil
	}
	m.mu.RUnlock()

	cfg, err := m.buildCfg(cred)
	if err != nil {
		return nil, err
	}
	if m.timeout > 0 {
		cfg.Timeout = m.timeout
	}
	if cred.ImpersonateEnabled && cred.ImpersonateUser != "" {
		cfg.Impersonate = rest.ImpersonationConfig{
			UserName: cred.ImpersonateUser,
			Groups:   cred.ImpersonateGroups,
		}
	}
	if cred.AccessMode == AccessAgent && cred.AgentID != "" {
		m.mu.RLock()
		rt := m.dialers[cred.AgentID]
		m.mu.RUnlock()
		if rt != nil {
			cfg.Transport = rt
			cfg.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
		}
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("clientset: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic: %w", err)
	}
	cli := &Client{
		ID:        clusterID,
		Config:    cfg,
		Clientset: cs,
		Dynamic:   dyn,
		Discovery: cs.Discovery(),
	}
	m.mu.Lock()
	m.clients[clusterID] = cli
	m.mu.Unlock()
	return cli, nil
}

func (m *ClusterManager) defaultBuildConfig(cred ClusterCred) (*rest.Config, error) {
	switch strings.TrimSpace(cred.AuthType) {
	case AuthKubeconfig, "":
		if strings.TrimSpace(cred.Credential) == "" {
			return nil, fmt.Errorf("kubeconfig 为空")
		}
		cfg, err := clientcmd.BuildConfigFromKubeconfigGetter("", func() (*clientcmdapi.Config, error) {
			return clientcmd.Load([]byte(cred.Credential))
		})
		if err != nil {
			return nil, fmt.Errorf("解析 kubeconfig: %w", err)
		}
		if host := strings.TrimSpace(cred.APIServer); host != "" {
			cfg.Host = host
		}
		return cfg, nil
	case AuthToken:
		host := strings.TrimSpace(cred.APIServer)
		if host == "" {
			return nil, fmt.Errorf("token 模式需要 api_server")
		}
		if strings.TrimSpace(cred.Credential) == "" {
			return nil, fmt.Errorf("token 为空")
		}
		cfg := &rest.Config{
			Host:        host,
			BearerToken: cred.Credential,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: strings.TrimSpace(cred.CAData) == "",
			},
		}
		if ca := strings.TrimSpace(cred.CAData); ca != "" {
			cfg.TLSClientConfig.CAData = []byte(ca)
			cfg.TLSClientConfig.Insecure = false
		}
		return cfg, nil
	default:
		return nil, fmt.Errorf("不支持的 auth_type: %s", cred.AuthType)
	}
}

// ProbeResult 探测结果。
type ProbeResult struct {
	OK           bool     `json:"ok"`
	Version      string   `json:"version,omitempty"`
	NodeCount    int      `json:"node_count"`
	ReadyNodes   int      `json:"ready_nodes"`
	Error        string   `json:"error,omitempty"`
	CanListPods  bool     `json:"can_list_pods"`
	CanListNodes bool     `json:"can_list_nodes"`
	CheckedAt    string   `json:"checked_at"`
	Warnings     []string `json:"warnings,omitempty"`
}

// Probe 探测集群连通性与基本权限。
func (m *ClusterManager) Probe(ctx context.Context, clusterID string, cred ClusterCred) ProbeResult {
	res := ProbeResult{CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	cli, err := m.Get(clusterID, cred)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	info, err := cli.Discovery.ServerVersion()
	if err != nil {
		res.Error = fmt.Sprintf("ServerVersion: %v", err)
		m.Invalidate(clusterID)
		return res
	}
	res.Version = info.GitVersion
	res.OK = true

	nodes, err := cli.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 500})
	if err != nil {
		res.Warnings = append(res.Warnings, "list nodes: "+err.Error())
	} else {
		res.CanListNodes = true
		res.NodeCount = len(nodes.Items)
		for _, n := range nodes.Items {
			for _, c := range n.Status.Conditions {
				if c.Type == "Ready" && c.Status == "True" {
					res.ReadyNodes++
					break
				}
			}
		}
	}
	_, err = cli.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		res.Warnings = append(res.Warnings, "list pods: "+err.Error())
	} else {
		res.CanListPods = true
	}
	return res
}

