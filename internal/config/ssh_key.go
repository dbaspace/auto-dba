package config

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// OpenSSH 私钥线格式魔数（Base64 解码后正文开头）。
var opensshKeyMagic = []byte("openssh-key-v1\x00")

// normalizeSSHPrivateKeyPEM 将 Nacos 中的私钥正文归一为可被 ssh -i / ParsePrivateKey 使用的 PEM。
// 支持：已有 PEM 头、裸 Base64（OpenSSH / PKCS#1 / PKCS#8）、误用头包裹。
func normalizeSSHPrivateKeyPEM(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("ssh_private_key 为空")
	}
	var firstParseErr error
	if strings.Contains(raw, "BEGIN") && strings.Contains(raw, "PRIVATE KEY") {
		pemIn := ensureTrailingNewline(raw)
		if _, err := ssh.ParsePrivateKey([]byte(pemIn)); err == nil {
			return pemIn, nil
		} else {
			firstParseErr = err
		}
		// 头可能写错或正文被截断：剥头后按魔数 / DER 重包
	}
	body, err := decodeKeyBody(raw)
	if err != nil {
		return "", wrapSSHKeyErr(firstParseErr, err)
	}
	pemOut, err := wrapPrivateKeyBody(body)
	if err != nil {
		return "", wrapSSHKeyErr(firstParseErr, err)
	}
	if _, err := ssh.ParsePrivateKey([]byte(pemOut)); err != nil {
		return "", wrapSSHKeyErr(firstParseErr, fmt.Errorf("归一化后的私钥无法解析（是否加密口令/内容截断）: %w", err))
	}
	return pemOut, nil
}

func wrapSSHKeyErr(first, err error) error {
	if first == nil {
		return err
	}
	return fmt.Errorf("%w（首次 PEM 解析: %v）", err, first)
}

func decodeKeyBody(raw string) ([]byte, error) {
	b64 := extractKeyBase64(raw)
	if b64 == "" {
		return nil, fmt.Errorf("ssh_private_key 无有效 Base64 正文")
	}
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		compact := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, b64)
		der, err = base64.StdEncoding.DecodeString(compact)
		if err != nil {
			return nil, fmt.Errorf("ssh_private_key Base64 解码失败: %w", err)
		}
	}
	return der, nil
}

func extractKeyBase64(raw string) string {
	var b strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-----") {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

func wrapPrivateKeyBody(body []byte) (string, error) {
	if bytes.HasPrefix(body, opensshKeyMagic) {
		out := string(pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body}))
		if _, err := ssh.ParsePrivateKey([]byte(out)); err != nil {
			return "", fmt.Errorf("OpenSSH 私钥无法解析（是否加密口令/内容截断）: %w", err)
		}
		return out, nil
	}
	return wrapDERPrivateKey(body)
}

func wrapDERPrivateKey(der []byte) (string, error) {
	if _, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})), nil
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return "", fmt.Errorf("无法识别私钥格式（非 OpenSSH / PKCS#1 / PKCS#8）: %w", err)
	}
	switch key.(type) {
	case *rsa.PrivateKey, *ecdsa.PrivateKey, ed25519.PrivateKey:
		return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
	default:
		return "", fmt.Errorf("不支持的 PKCS#8 私钥类型 %T", key)
	}
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func sshKeyFingerprint(pemText string) string {
	sum := sha256.Sum256([]byte(pemText))
	return hex.EncodeToString(sum[:])
}

// yamlSSHPrivateKeyHint 在远程 YAML 解析失败时附加运维提示。
func yamlSSHPrivateKeyHint(remoteYAML string, parseErr error) string {
	if parseErr == nil {
		return ""
	}
	body := remoteYAML
	looksKey := strings.Contains(body, "ssh_private_key") ||
		strings.Contains(body, "BEGIN") && strings.Contains(body, "PRIVATE KEY") ||
		strings.Contains(body, "MIIE") || strings.Contains(body, "MIIB") ||
		strings.Contains(body, "openssh-key")
	msg := parseErr.Error()
	indentIssue := strings.Contains(msg, "expected ':'") || strings.Contains(msg, "did not find expected")
	if !looksKey && !indentIssue {
		return ""
	}
	return "；若配置了 terminal.ssh_private_key：请使用「ssh_private_key: |」块标量，且私钥每一行保持缩进（勿顶格）；OpenSSH / RSA PEM 或裸 Base64 均可，见 docs/nacos-config.md"
}
