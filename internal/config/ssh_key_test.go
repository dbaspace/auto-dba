package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

func testRSAMaterial(t *testing.T) (pemText, bareB64 string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	pemText = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
	bareB64 = base64.StdEncoding.EncodeToString(der)
	return pemText, bareB64
}

func TestNormalizeSSHPrivateKeyPEM_BareBase64(t *testing.T) {
	pemText, bare := testRSAMaterial(t)
	// 多行裸 Base64（模拟生产粘贴）
	var lines []string
	for i := 0; i < len(bare); i += 64 {
		end := i + 64
		if end > len(bare) {
			end = len(bare)
		}
		lines = append(lines, bare[i:end])
	}
	out, err := normalizeSSHPrivateKeyPEM(strings.Join(lines, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BEGIN RSA PRIVATE KEY") {
		t.Fatalf("want RSA PEM, got %q", out[:min(80, len(out))])
	}
	if _, err := ssh.ParsePrivateKey([]byte(out)); err != nil {
		t.Fatal(err)
	}
	_ = pemText
}

func TestNormalizeSSHPrivateKeyPEM_WrongOpenSSHHeader(t *testing.T) {
	_, bare := testRSAMaterial(t)
	wrong := "-----BEGIN OPENSSH PRIVATE KEY-----\n" + bare + "\n-----END OPENSSH PRIVATE KEY-----\n"
	out, err := normalizeSSHPrivateKeyPEM(wrong)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BEGIN RSA PRIVATE KEY") {
		t.Fatalf("should rewrap to RSA PEM: %q", out[:min(60, len(out))])
	}
	if _, err := ssh.ParsePrivateKey([]byte(out)); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeSSHPrivateKeyPEM_ValidPEMPassthrough(t *testing.T) {
	pemText, _ := testRSAMaterial(t)
	out, err := normalizeSSHPrivateKeyPEM(pemText)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BEGIN RSA PRIVATE KEY") {
		t.Fatal(out)
	}
}

func testOpenSSHMaterial(t *testing.T) (pemText, bareB64 string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal(err)
	}
	pemText = string(pem.EncodeToMemory(block))
	bareB64 = base64.StdEncoding.EncodeToString(block.Bytes)
	return pemText, bareB64
}

func TestNormalizeSSHPrivateKeyPEM_OpenSSHPassthrough(t *testing.T) {
	pemText, _ := testOpenSSHMaterial(t)
	out, err := normalizeSSHPrivateKeyPEM(pemText)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BEGIN OPENSSH PRIVATE KEY") {
		t.Fatalf("got %q", out[:min(80, len(out))])
	}
	if _, err := ssh.ParsePrivateKey([]byte(out)); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeSSHPrivateKeyPEM_OpenSSHBareBase64(t *testing.T) {
	_, bare := testOpenSSHMaterial(t)
	var lines []string
	for i := 0; i < len(bare); i += 70 {
		end := i + 70
		if end > len(bare) {
			end = len(bare)
		}
		lines = append(lines, bare[i:end])
	}
	out, err := normalizeSSHPrivateKeyPEM(strings.Join(lines, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BEGIN OPENSSH PRIVATE KEY") {
		t.Fatalf("want OPENSSH PEM, got %q", out[:min(80, len(out))])
	}
	if _, err := ssh.ParsePrivateKey([]byte(out)); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeSSHPrivateKeyPEM_RSAHeaderOnOpenSSHBody(t *testing.T) {
	_, bare := testOpenSSHMaterial(t)
	wrong := "-----BEGIN RSA PRIVATE KEY-----\n" + bare + "\n-----END RSA PRIVATE KEY-----\n"
	out, err := normalizeSSHPrivateKeyPEM(wrong)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BEGIN OPENSSH PRIVATE KEY") {
		t.Fatalf("should rewrap to OPENSSH: %q", out[:min(80, len(out))])
	}
	if strings.Contains(errString(err), "pkcs8") {
		t.Fatalf("must not fall through to pkcs8: %v", err)
	}
	if _, err := ssh.ParsePrivateKey([]byte(out)); err != nil {
		t.Fatal(err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestYamlSSHPrivateKeyHint(t *testing.T) {
	body := "terminal:\n  ssh_private_key: |\n    aaa\nbbb\n"
	parseErr := yaml.Unmarshal([]byte(body), &struct{}{})
	if parseErr == nil {
		t.Fatal("expected parse error for unindented block")
	}
	hint := yamlSSHPrivateKeyHint(body, parseErr)
	if !strings.Contains(hint, "ssh_private_key: |") {
		t.Fatalf("hint=%q err=%v", hint, parseErr)
	}
}

func TestMaterializeAndDiffFingerprint(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("app:\n  jwt_secret: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, bare := testRSAMaterial(t)
	old := &Config{}
	old.Terminal.SSHPrivateKey = bare
	if err := materializeSSHPrivateKey(old, cfgPath); err != nil {
		t.Fatal(err)
	}
	if old.Terminal.SSHKeyFingerprint == "" || old.Terminal.SSHPrivateKey != "" {
		t.Fatalf("fp=%q pem=%q", old.Terminal.SSHKeyFingerprint, old.Terminal.SSHPrivateKey)
	}

	nextSame := &Config{}
	nextSame.Terminal.SSHPrivateKey = bare
	if err := materializeSSHPrivateKey(nextSame, cfgPath); err != nil {
		t.Fatal(err)
	}
	dSame := DiffConfig(old, nextSame)
	if dSame.SSHKey || dSame.Terminal {
		t.Fatalf("same key should not diff: %+v", dSame)
	}

	key2, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	bare2 := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key2))
	nextDiff := &Config{}
	nextDiff.Terminal.SSHPrivateKey = bare2
	if err := materializeSSHPrivateKey(nextDiff, cfgPath); err != nil {
		t.Fatal(err)
	}
	dDiff := DiffConfig(old, nextDiff)
	if !dDiff.SSHKey || !dDiff.Terminal {
		t.Fatalf("changed key should diff: %+v", dDiff)
	}
}

func TestMergeRemoteYAML_UnindentedKeyHint(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	local := `
nacos:
  enabled: false
app:
  jwt_secret: local
`
	if err := os.WriteFile(cfgPath, []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}
	remote := "terminal:\n  ssh_user: sysadmin\n  ssh_private_key: |\n    -----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n"
	_, err := MergeRemoteYAML(cfgPath, remote)
	if err == nil {
		t.Fatal("expected yaml parse error")
	}
	if !strings.Contains(err.Error(), "parse remote yaml") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "ssh_private_key: |") {
		t.Fatalf("missing hint: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
