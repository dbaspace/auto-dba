package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func genHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

var testAddr = &net.TCPAddr{IP: net.ParseIP("10.100.112.17"), Port: 22}

const testHost = "10.100.112.17:22"

func TestAcceptNewRecordsThenRejectsChangedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "known_hosts")
	cfg := Config{HostKeyPolicy: HostKeyAcceptNew, KnownHostsFile: path}
	key := genHostKey(t)

	cb, err := hostKeyCallback(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb(testHost, testAddr, key); err != nil {
		t.Fatalf("首次连接应放行: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "10.100.112.17") {
		t.Fatalf("known_hosts 未写入主机: %q", data)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts 权限应为 0600，实际 %v", fi.Mode().Perm())
	}

	cb, err = hostKeyCallback(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb(testHost, testAddr, key); err != nil {
		t.Fatalf("相同指纹应放行: %v", err)
	}
	before, _ := os.ReadFile(path)
	if strings.Count(string(before), "\n") != 1 {
		t.Fatalf("相同指纹不应重复写入: %q", before)
	}

	err = cb(testHost, testAddr, genHostKey(t))
	if err == nil || !strings.Contains(err.Error(), "ssh-keygen -R 10.100.112.17") {
		t.Fatalf("指纹变化应拒绝并提示 ssh-keygen -R，实际: %v", err)
	}
}

func TestStrictRejectsUnknownHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	cb, err := hostKeyCallback(Config{HostKeyPolicy: HostKeyStrict, KnownHostsFile: path})
	if err != nil {
		t.Fatal(err)
	}
	err = cb(testHost, testAddr, genHostKey(t))
	if err == nil || !strings.Contains(err.Error(), "strict") {
		t.Fatalf("strict 应拒绝未知主机，实际: %v", err)
	}
	if data, _ := os.ReadFile(path); len(data) != 0 {
		t.Fatalf("strict 不应写入 known_hosts: %q", data)
	}
}

func TestInsecureSkipsVerification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	for _, cfg := range []Config{
		{HostKeyPolicy: HostKeyInsecure, KnownHostsFile: path},
		{HostKeyPolicy: HostKeyStrict, InsecureHostKey: true, KnownHostsFile: path},
	} {
		cb, err := hostKeyCallback(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := cb(testHost, testAddr, genHostKey(t)); err != nil {
			t.Fatalf("insecure 应放行: %v", err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("insecure 不应创建 known_hosts")
	}
}

func TestSetDefaultsApplied(t *testing.T) {
	defaultsMu.RLock()
	oldPolicy, oldPath := defaultPolicy, defaultKnownHosts
	defaultsMu.RUnlock()
	t.Cleanup(func() { SetDefaults(oldPolicy, oldPath) })

	path := filepath.Join(t.TempDir(), "known_hosts")
	SetDefaults("STRICT", path)
	policy, got, err := resolveHostKeySettings(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if policy != HostKeyStrict || got != path {
		t.Fatalf("默认值未生效: policy=%s path=%s", policy, got)
	}
	policy, _, _ = resolveHostKeySettings(Config{HostKeyPolicy: HostKeyAcceptNew})
	if policy != HostKeyAcceptNew {
		t.Fatalf("Config.HostKeyPolicy 应覆盖默认值，实际 %s", policy)
	}
}
