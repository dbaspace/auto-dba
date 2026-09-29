package sshx

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// 主机密钥策略。
const (
	HostKeyAcceptNew = "accept-new" // 未知主机记录指纹后放行；指纹变化拒绝
	HostKeyStrict    = "strict"     // 必须已在 known_hosts
	HostKeyInsecure  = "insecure"   // 不校验（仅开发）
)

// Config SSH 客户端配置（容器内不依赖系统 ssh/scp）。
type Config struct {
	User            string
	IdentityFile    string
	Timeout         time.Duration
	InsecureHostKey bool   // true：忽略 host key，优先于 HostKeyPolicy
	HostKeyPolicy   string // 空则用 SetDefaults 设置的默认值
	KnownHostsFile  string // 空则用默认文件；默认未设置时为 ~/.ssh/known_hosts
}

var (
	defaultsMu        sync.RWMutex
	defaultPolicy     = HostKeyAcceptNew
	defaultKnownHosts string

	knownHostsWriteMu sync.Mutex
)

// SetDefaults 设置进程级主机密钥策略与 known_hosts 路径（启动时调用一次）。
func SetDefaults(policy, knownHostsFile string) {
	defaultsMu.Lock()
	defer defaultsMu.Unlock()
	if p := normalizePolicy(policy); p != "" {
		defaultPolicy = p
	}
	defaultKnownHosts = strings.TrimSpace(knownHostsFile)
}

func normalizePolicy(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case HostKeyAcceptNew:
		return HostKeyAcceptNew
	case HostKeyStrict:
		return HostKeyStrict
	case HostKeyInsecure:
		return HostKeyInsecure
	default:
		return ""
	}
}

func resolveHostKeySettings(cfg Config) (policy, path string, err error) {
	defaultsMu.RLock()
	policy, path = defaultPolicy, defaultKnownHosts
	defaultsMu.RUnlock()
	if p := normalizePolicy(cfg.HostKeyPolicy); p != "" {
		policy = p
	}
	if cfg.InsecureHostKey {
		policy = HostKeyInsecure
	}
	if f := strings.TrimSpace(cfg.KnownHostsFile); f != "" {
		path = f
	}
	if path == "" && policy != HostKeyInsecure {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", "", fmt.Errorf("SSH known_hosts: 无法定位 HOME（可配置 terminal.ssh_known_hosts_file）: %w", herr)
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
	}
	return policy, path, nil
}

// Client 已建立的 SSH 连接。
type Client struct {
	cfg  Config
	conn *ssh.Client
}

// Dial 使用私钥连接目标主机。
func Dial(host string, port int, cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.User) == "" {
		return nil, fmt.Errorf("SSH 用户为空")
	}
	if strings.TrimSpace(cfg.IdentityFile) == "" {
		return nil, fmt.Errorf("SSH 私钥路径为空")
	}
	if port <= 0 {
		port = 22
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	signer, err := LoadPrivateKey(cfg.IdentityFile, "")
	if err != nil {
		return nil, err
	}
	callback, err := hostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}
	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: callback,
		Timeout:         cfg.Timeout,
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接 %s 失败: %w", addr, err)
	}
	return &Client{cfg: cfg, conn: conn}, nil
}

func hostKeyCallback(cfg Config) (ssh.HostKeyCallback, error) {
	policy, path, err := resolveHostKeySettings(cfg)
	if err != nil {
		return nil, err
	}
	if policy == HostKeyInsecure {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	if err := ensureKnownHostsFile(path); err != nil {
		return nil, err
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("加载 known_hosts %s 失败: %w", path, err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		verr := cb(hostname, remote, key)
		if verr == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(verr, &keyErr) {
			return verr
		}
		host := hostOnly(hostname)
		if len(keyErr.Want) > 0 {
			return fmt.Errorf("主机 %s 的 SSH 指纹与 known_hosts 记录不一致（%s，可能是重装系统或中间人攻击）；确认无误后执行 ssh-keygen -R %s -f %s 删除旧记录再重试",
				hostname, ssh.FingerprintSHA256(key), host, path)
		}
		if policy != HostKeyAcceptNew {
			return fmt.Errorf("主机 %s 的 SSH 指纹未登记（策略 strict）；请先执行 ssh-keyscan %s >> %s，或将 terminal.ssh_host_key_policy 设为 accept-new",
				hostname, host, path)
		}
		return appendKnownHost(path, hostname, remote, key)
	}, nil
}

func hostOnly(hostname string) string {
	if h, _, err := net.SplitHostPort(hostname); err == nil {
		return h
	}
	return hostname
}

func ensureKnownHostsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建 known_hosts 目录失败: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return fmt.Errorf("创建 known_hosts %s 失败: %w", path, err)
	}
	return f.Close()
}

func appendKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	knownHostsWriteMu.Lock()
	defer knownHostsWriteMu.Unlock()
	// 并发时可能已被其它连接写入，重读一次
	if cb, err := knownhosts.New(path); err == nil {
		if cb(hostname, remote, key) == nil {
			return nil
		}
	}
	addrs := []string{knownhosts.Normalize(hostname)}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("写入 known_hosts %s 失败: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(knownhosts.Line(addrs, key) + "\n"); err != nil {
		return fmt.Errorf("写入 known_hosts %s 失败: %w", path, err)
	}
	return nil
}

// Close 关闭连接。
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// SSHClient 暴露底层连接（高级用途）。
func (c *Client) SSHClient() *ssh.Client {
	if c == nil {
		return nil
	}
	return c.conn
}

// LoadPrivateKey 从文件加载私钥（PEM / OpenSSH），passphrase 可空。
func LoadPrivateKey(path, passphrase string) (ssh.Signer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取私钥 %s 失败: %w", path, err)
	}
	return ParsePrivateKey(raw, passphrase)
}

// ParsePrivateKey 解析私钥字节。
func ParsePrivateKey(raw []byte, passphrase string) (ssh.Signer, error) {
	var (
		signer ssh.Signer
		err    error
	)
	if strings.TrimSpace(passphrase) != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(raw, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %w", err)
	}
	return signer, nil
}
