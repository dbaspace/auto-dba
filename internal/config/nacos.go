package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"gopkg.in/yaml.v3"
)

// fetchNacosConfig 可在测试中替换。
var fetchNacosConfig = fetchNacosConfigSDK

// nacosDiskCacheRoot 本机 SDK 缓存根目录（可测时替换）。
var nacosDiskCacheRoot = func() string { return filepath.Join(os.TempDir(), "nacos") }

const watchDebounce = 800 * time.Millisecond

func applyNacosEnvOverrides(nc *NacosConfig) {
	if nc == nil {
		return
	}
	if v := strings.TrimSpace(os.Getenv("NACOS_ENABLED")); v != "" {
		nc.Enabled = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if v := strings.TrimSpace(os.Getenv("NACOS_ADDR")); v != "" {
		nc.ServerAddr = v
	}
	if v := strings.TrimSpace(os.Getenv("NACOS_NAMESPACE")); v != "" {
		nc.Namespace = v
	}
	if v := strings.TrimSpace(os.Getenv("NACOS_GROUP")); v != "" {
		nc.Group = v
	}
	if v := strings.TrimSpace(os.Getenv("NACOS_DATA_ID")); v != "" {
		nc.DataID = v
	}
	if v := strings.TrimSpace(os.Getenv("NACOS_USERNAME")); v != "" {
		nc.Username = v
	}
	if v := strings.TrimSpace(os.Getenv("NACOS_PASSWORD")); v != "" {
		nc.Password = v
	}
	if v := strings.TrimSpace(os.Getenv("NACOS_WATCH")); v != "" {
		on := v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		nc.Watch = &on
	}
	if strings.TrimSpace(nc.Group) == "" {
		nc.Group = "DEFAULT_GROUP"
	}
	if strings.TrimSpace(nc.DataID) == "" {
		nc.DataID = "auto-dba.yaml"
	}
	if strings.EqualFold(strings.TrimSpace(nc.Namespace), "public") {
		nc.Namespace = ""
	}
}

func parseServerAddrs(addr string) ([]constant.ServerConfig, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("server_addr 为空")
	}
	var out []constant.ServerConfig
	for _, part := range strings.Split(addr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		host, portStr, ok := strings.Cut(part, ":")
		if !ok || strings.TrimSpace(host) == "" || strings.TrimSpace(portStr) == "" {
			return nil, fmt.Errorf("server_addr 无效 %q（期望 host:port）", part)
		}
		port, err := strconv.ParseUint(strings.TrimSpace(portStr), 10, 64)
		if err != nil || port == 0 {
			return nil, fmt.Errorf("server_addr 端口无效 %q", part)
		}
		out = append(out, *constant.NewServerConfig(strings.TrimSpace(host), port))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("server_addr 无有效节点")
	}
	return out, nil
}

func nacosCacheSubdir(nc NacosConfig) string {
	ns := strings.TrimSpace(nc.Namespace)
	if ns == "" {
		ns = "public"
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, ns)
	return filepath.Join(os.TempDir(), "nacos", fmt.Sprintf("%s-%d", safe, os.Getpid()))
}

func newNacosConfigClient(nc NacosConfig) (config_client.IConfigClient, error) {
	servers, err := parseServerAddrs(nc.ServerAddr)
	if err != nil {
		return nil, err
	}
	base := nacosCacheSubdir(nc)
	cc := *constant.NewClientConfig(
		constant.WithNamespaceId(strings.TrimSpace(nc.Namespace)),
		constant.WithTimeoutMs(8000),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithLogDir(filepath.Join(base, "log")),
		constant.WithCacheDir(filepath.Join(base, "cache")),
		constant.WithLogLevel("warn"),
	)
	if u := strings.TrimSpace(nc.Username); u != "" {
		cc.Username = u
		cc.Password = nc.Password
	}
	client, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  &cc,
		ServerConfigs: servers,
	})
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败（%s）: %w", nc.ServerAddr, err)
	}
	return client, nil
}

var (
	sharedClientMu sync.Mutex
	sharedClient   config_client.IConfigClient
	sharedClientNC NacosConfig
)

func getOrCreateSharedClient(nc NacosConfig) (config_client.IConfigClient, error) {
	sharedClientMu.Lock()
	defer sharedClientMu.Unlock()
	if sharedClient != nil &&
		sharedClientNC.ServerAddr == nc.ServerAddr &&
		sharedClientNC.Namespace == nc.Namespace &&
		sharedClientNC.Username == nc.Username {
		return sharedClient, nil
	}
	client, err := newNacosConfigClient(nc)
	if err != nil {
		return nil, err
	}
	sharedClient = client
	sharedClientNC = nc
	return client, nil
}

func fetchNacosConfigSDK(nc NacosConfig) (string, error) {
	client, err := getOrCreateSharedClient(nc)
	if err != nil {
		// 客户端都建不起来时仍尝试磁盘缓存，方便离线/断网启动
		if cached, path, cerr := readLatestNacosDiskCache(nc); cerr == nil {
			log.Printf("nacos: 创建客户端失败，改用本地缓存 %s（%v）", path, err)
			return cached, nil
		}
		return "", err
	}
	content, err := client.GetConfig(vo.ConfigParam{
		DataId: strings.TrimSpace(nc.DataID),
		Group:  strings.TrimSpace(nc.Group),
	})
	if err != nil {
		if cached, path, cerr := readLatestNacosDiskCache(nc); cerr == nil {
			log.Printf("nacos: GetConfig 失败，改用本地缓存 %s dataId=%s group=%s（%v）",
				path, nc.DataID, nc.Group, err)
			return cached, nil
		}
		return "", fmt.Errorf("GetConfig dataId=%s group=%s namespace=%s: %w",
			nc.DataID, nc.Group, nc.Namespace, err)
	}
	log.Printf("已从 Nacos 加载配置 dataId=%s group=%s namespace=%s servers=%s",
		nc.DataID, nc.Group, nc.Namespace, nc.ServerAddr)
	return content, nil
}

// nacosCacheFileName 与 nacos-sdk-go 落盘命名一致：dataId@@group@@tenant
func nacosCacheFileName(dataID, group, namespace string) string {
	dataID = strings.TrimSpace(dataID)
	group = strings.TrimSpace(group)
	if group == "" {
		group = "DEFAULT_GROUP"
	}
	tenant := strings.TrimSpace(namespace)
	if strings.EqualFold(tenant, "public") {
		tenant = ""
	}
	return dataID + "@@" + group + "@@" + tenant
}

// readLatestNacosDiskCache 在 GetConfig 失败时，复用本机其它进程留下的最近一份缓存
//（cache 按 pid 隔离，新进程空目录会导致「server+cache 双失败」）。
func readLatestNacosDiskCache(nc NacosConfig) (content, path string, err error) {
	name := nacosCacheFileName(nc.DataID, nc.Group, nc.Namespace)
	root := nacosDiskCacheRoot()
	ents, err := os.ReadDir(root)
	if err != nil {
		return "", "", err
	}
	var bestPath string
	var bestMod time.Time
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name(), "cache", "config", name)
		st, serr := os.Stat(p)
		if serr != nil || st.IsDir() || st.Size() == 0 {
			continue
		}
		if bestPath == "" || st.ModTime().After(bestMod) {
			bestPath = p
			bestMod = st.ModTime()
		}
	}
	if bestPath == "" {
		return "", "", fmt.Errorf("无可用本地缓存 %s", name)
	}
	b, err := os.ReadFile(bestPath)
	if err != nil {
		return "", "", err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", "", fmt.Errorf("本地缓存为空: %s", bestPath)
	}
	return s, bestPath, nil
}

// BeforeApplyFn 在写入 live 之前调用（如 DSN Reopen）；返回 error 则中止本次热更。
type BeforeApplyFn func(live, next *Config, diff ReloadDiff) error

// OnRemoteChange 热更成功写入 live 之后的副作用回调。
type OnRemoteChange func(diff ReloadDiff)

var (
	watchMu     sync.Mutex
	watchClient config_client.IConfigClient
)

// WatchOptions 控制 ListenConfig 行为。
type WatchOptions struct {
	BeforeApply BeforeApplyFn
	AfterApply  OnRemoteChange
	Debounce    time.Duration // 0 用默认 watchDebounce
}

// StartNacosWatch 注册 ListenConfig；与 Load 共用同一 IConfigClient。
func StartNacosWatch(live *Config, configPath string, after OnRemoteChange) error {
	return StartNacosWatchOpts(live, configPath, WatchOptions{AfterApply: after})
}

// StartNacosWatchOpts 带 BeforeApply / debounce 的监听入口。
func StartNacosWatchOpts(live *Config, configPath string, opts WatchOptions) error {
	if live == nil || !live.Nacos.Enabled || !live.Nacos.WatchEnabled() {
		return nil
	}
	watchMu.Lock()
	defer watchMu.Unlock()
	if watchClient != nil {
		return nil
	}
	client, err := getOrCreateSharedClient(live.Nacos)
	if err != nil {
		return err
	}
	boot := live.Nacos
	debounce := opts.Debounce
	if debounce <= 0 {
		debounce = watchDebounce
	}
	var (
		debMu    sync.Mutex
		debTimer *time.Timer
		pending  string
	)
	applyOnce := func(data string) {
		next, err := MergeRemoteYAML(configPath, data)
		if err != nil {
			log.Printf("nacos watch: merge failed: %v", err)
			return
		}
		LockWrite()
		defer UnlockWrite()
		diff := DiffConfig(live, next)
		if !diff.Any() {
			log.Printf("nacos watch: dataId=%s 内容推送但业务字段无变化", boot.DataID)
			return
		}
		if opts.BeforeApply != nil {
			if err := opts.BeforeApply(live, next, diff); err != nil {
				log.Printf("nacos watch: before-apply aborted: %v", err)
				return
			}
		}
		ApplyInPlace(live, next)
		PublishJWT(live.App.JWTSecret)
		SyncFieldCrypto(live)
		log.Printf("nacos watch: applied dataId=%s group=%s sections=%v listen=%v jwt=%v",
			boot.DataID, boot.Group, diff.Sections(), diff.ListenAddr, diff.JWTSecret)
		if opts.AfterApply != nil {
			opts.AfterApply(diff)
		}
	}
	err = client.ListenConfig(vo.ConfigParam{
		DataId: strings.TrimSpace(boot.DataID),
		Group:  strings.TrimSpace(boot.Group),
		OnChange: func(namespace, group, dataId, data string) {
			_ = namespace
			_ = group
			_ = dataId
			debMu.Lock()
			pending = data
			if debTimer != nil {
				debTimer.Stop()
			}
			debTimer = time.AfterFunc(debounce, func() {
				debMu.Lock()
				payload := pending
				pending = ""
				debMu.Unlock()
				if payload == "" {
					return
				}
				applyOnce(payload)
			})
			debMu.Unlock()
		},
	})
	if err != nil {
		return fmt.Errorf("ListenConfig: %w", err)
	}
	watchClient = client
	log.Printf("nacos watch: listening dataId=%s group=%s namespace=%s debounce=%s",
		boot.DataID, boot.Group, boot.Namespace, debounce)
	return nil
}

func preserveHTTPFromLocal(dst *HTTPConfig, local HTTPConfig, remoteYAML string) {
	if dst.Host == "" {
		dst.Host = local.Host
	}
	if dst.Port == 0 {
		dst.Port = local.Port
	}
	if dst.ReadTimeout <= 0 {
		dst.ReadTimeout = local.ReadTimeout
	}
	if dst.WriteTimeout <= 0 {
		dst.WriteTimeout = local.WriteTimeout
	}
	if dst.PidFile == "" {
		dst.PidFile = local.PidFile
	}
	// bool 零值是 false：远端 http 段未写 serve_static 时 Unmarshal 会冲掉本地 true
	if !remoteYAMLHasHTTPKey(remoteYAML, "serve_static") {
		dst.ServeStatic = local.ServeStatic
	}
}

// remoteYAMLHasHTTPKey 判断远端 YAML 的 http: 映射中是否出现指定键（含显式 false）。
func remoteYAMLHasHTTPKey(remoteYAML, key string) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(remoteYAML), &doc); err != nil {
		return false
	}
	root := &doc
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "http" {
			continue
		}
		httpNode := root.Content[i+1]
		if httpNode.Kind != yaml.MappingNode {
			return false
		}
		for j := 0; j+1 < len(httpNode.Content); j += 2 {
			if httpNode.Content[j].Value == key {
				return true
			}
		}
		return false
	}
	return false
}

func materializeSSHPrivateKey(cfg *Config, configPath string) error {
	raw := strings.TrimSpace(cfg.Terminal.SSHPrivateKey)
	if raw == "" {
		return nil
	}
	pemText, err := normalizeSSHPrivateKeyPEM(raw)
	if err != nil {
		return fmt.Errorf("ssh_private_key: %w", err)
	}
	fp := sshKeyFingerprint(pemText)
	root := repoRootFromConfigPath(configPath)
	dir := filepath.Join(root, "config", "terminal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建 terminal 目录: %w", err)
	}
	path := filepath.Join(dir, ".runtime_id_ed25519")
	if prev, err := os.ReadFile(path); err == nil && string(prev) == pemText {
		cfg.Terminal.SSHIdentityFile = path
		cfg.Terminal.SSHPrivateKey = ""
		cfg.Terminal.SSHKeyFingerprint = fp
		return nil
	}
	if err := os.WriteFile(path, []byte(pemText), 0o600); err != nil {
		return fmt.Errorf("写入 SSH 私钥 %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)
	cfg.Terminal.SSHIdentityFile = path
	cfg.Terminal.SSHPrivateKey = ""
	cfg.Terminal.SSHKeyFingerprint = fp
	return nil
}

// materializeSSHKeyProfiles 校验 env_codes 唯一，并将各档案私钥物化为 .runtime_<name>。
func materializeSSHKeyProfiles(cfg *Config, configPath string) error {
	if cfg == nil {
		return nil
	}
	profiles := cfg.Terminal.SSHKeyProfiles
	if len(profiles) == 0 {
		return nil
	}
	seenEnv := map[string]string{}
	seenName := map[string]struct{}{}
	root := repoRootFromConfigPath(configPath)
	dir := filepath.Join(root, "config", "terminal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建 terminal 目录: %w", err)
	}
	for i := range profiles {
		p := &profiles[i]
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return fmt.Errorf("ssh_key_profiles[%d]: name 不能为空", i)
		}
		safe := sanitizeSSHProfileName(name)
		if safe == "" {
			return fmt.Errorf("ssh_key_profiles: name %q 无效", name)
		}
		if _, ok := seenName[safe]; ok {
			return fmt.Errorf("ssh_key_profiles: name %q 重复", name)
		}
		seenName[safe] = struct{}{}
		p.Name = name
		hasEnv := false
		for _, code := range p.EnvCodes {
			k := strings.ToLower(strings.TrimSpace(code))
			if k == "" {
				continue
			}
			hasEnv = true
			if prev, ok := seenEnv[k]; ok {
				return fmt.Errorf("ssh_key_profiles: env_code %q 同时出现在档案 %q 与 %q", code, prev, name)
			}
			seenEnv[k] = name
		}
		if !hasEnv {
			return fmt.Errorf("ssh_key_profiles[%q]: env_codes 不能为空", name)
		}
		raw := strings.TrimSpace(p.SSHPrivateKey)
		if raw != "" {
			pemText, err := normalizeSSHPrivateKeyPEM(raw)
			if err != nil {
				return fmt.Errorf("ssh_key_profiles[%q].ssh_private_key: %w", name, err)
			}
			fp := sshKeyFingerprint(pemText)
			path := filepath.Join(dir, ".runtime_"+safe)
			if prev, err := os.ReadFile(path); err == nil && string(prev) == pemText {
				p.SSHIdentityFile = path
				p.SSHPrivateKey = ""
				p.Fingerprint = fp
				continue
			}
			if err := os.WriteFile(path, []byte(pemText), 0o600); err != nil {
				return fmt.Errorf("写入 SSH 档案私钥 %s: %w", path, err)
			}
			_ = os.Chmod(path, 0o600)
			p.SSHIdentityFile = path
			p.SSHPrivateKey = ""
			p.Fingerprint = fp
			continue
		}
		idFile := strings.TrimSpace(p.SSHIdentityFile)
		if idFile == "" {
			return fmt.Errorf("ssh_key_profiles[%q]: 需配置 ssh_private_key 或 ssh_identity_file", name)
		}
		if !filepath.IsAbs(idFile) {
			idFile = filepath.Join(root, idFile)
			p.SSHIdentityFile = idFile
		}
		if st, err := os.Stat(idFile); err != nil || st.IsDir() {
			return fmt.Errorf("ssh_key_profiles[%q]: ssh_identity_file 不可用: %s", name, idFile)
		}
		if b, err := os.ReadFile(idFile); err == nil {
			p.Fingerprint = sshKeyFingerprint(string(b))
		}
	}
	cfg.Terminal.SSHKeyProfiles = profiles
	return nil
}

func sanitizeSSHProfileName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func sshKeyProfilesFingerprintEqual(a, b []SSHKeyProfile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name ||
			a[i].Fingerprint != b[i].Fingerprint ||
			a[i].SSHIdentityFile != b[i].SSHIdentityFile ||
			!stringSliceEqualFoldSorted(a[i].EnvCodes, b[i].EnvCodes) {
			return false
		}
	}
	return true
}

func stringSliceEqualFoldSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]string(nil), a...)
	bb := append([]string(nil), b...)
	for i := range aa {
		aa[i] = strings.ToLower(strings.TrimSpace(aa[i]))
		bb[i] = strings.ToLower(strings.TrimSpace(bb[i]))
	}
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func repoRootFromConfigPath(configPath string) string {
	root := "."
	if abs, err := filepath.Abs(filepath.Dir(configPath)); err == nil {
		if filepath.Base(abs) == "config" {
			root = filepath.Dir(abs)
		} else {
			root = abs
		}
	}
	return root
}
