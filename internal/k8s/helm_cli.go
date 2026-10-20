package k8s

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// HelmUpgradeInstall 通过本机 helm CLI 执行 upgrade --install（需平台主机已安装 helm）。
func HelmUpgradeInstall(ctx context.Context, kubeconfigPath, release, namespace, chart string, valuesFiles []string, setVals []string, wait bool) (string, error) {
	if err := requireHelm(); err != nil {
		return "", err
	}
	release = strings.TrimSpace(release)
	namespace = strings.TrimSpace(namespace)
	chart = strings.TrimSpace(chart)
	if release == "" || namespace == "" || chart == "" {
		return "", fmt.Errorf("release/namespace/chart 必填")
	}
	args := []string{"upgrade", "--install", release, chart, "-n", namespace, "--create-namespace"}
	if wait {
		args = append(args, "--wait", "--timeout", "10m")
	}
	for _, vf := range valuesFiles {
		vf = strings.TrimSpace(vf)
		if vf != "" {
			args = append(args, "-f", vf)
		}
	}
	for _, s := range setVals {
		s = strings.TrimSpace(s)
		if s != "" {
			args = append(args, "--set", s)
		}
	}
	return runHelm(ctx, kubeconfigPath, args)
}

// HelmRollback 回滚到上一 revision（revision<=0）或指定 revision。
func HelmRollback(ctx context.Context, kubeconfigPath, release, namespace string, revision int) (string, error) {
	if err := requireHelm(); err != nil {
		return "", err
	}
	release = strings.TrimSpace(release)
	namespace = strings.TrimSpace(namespace)
	if release == "" || namespace == "" {
		return "", fmt.Errorf("release/namespace 必填")
	}
	args := []string{"rollback", release, "-n", namespace}
	if revision > 0 {
		args = []string{"rollback", release, fmt.Sprintf("%d", revision), "-n", namespace}
	}
	return runHelm(ctx, kubeconfigPath, args)
}

func requireHelm() error {
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("本机未找到 helm 可执行文件，请安装 Helm 3 或改用 kubeconfig 侧工具")
	}
	return nil
}

func runHelm(ctx context.Context, kubeconfigPath string, args []string) (string, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "helm", args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigPath, "HELM_KUBEAPISERVER=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	if err != nil {
		return out, fmt.Errorf("helm %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return out, nil
}

// WriteTempKubeconfig 将 ClusterCred 写成临时 kubeconfig，调用方负责 Remove。
func WriteTempKubeconfig(cred ClusterCred) (path string, cleanup func(), err error) {
	cleanup = func() {}
	dir, err := os.MkdirTemp("", "auto-dba-helm-*")
	if err != nil {
		return "", cleanup, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	path = filepath.Join(dir, "config")
	var raw []byte
	switch strings.ToLower(strings.TrimSpace(cred.AuthType)) {
	case AuthKubeconfig, "":
		raw = []byte(cred.Credential)
		if len(bytes.TrimSpace(raw)) == 0 {
			cleanup()
			return "", func() {}, fmt.Errorf("kubeconfig 凭据为空")
		}
	case AuthToken:
		server := strings.TrimSpace(cred.APIServer)
		if server == "" {
			cleanup()
			return "", func() {}, fmt.Errorf("token 认证须配置 APIServer")
		}
		clusterBlock := fmt.Sprintf("    server: %s\n", server)
		if strings.TrimSpace(cred.CAData) != "" {
			caB64 := base64.StdEncoding.EncodeToString([]byte(cred.CAData))
			clusterBlock += "    certificate-authority-data: " + caB64 + "\n"
		} else {
			clusterBlock += "    insecure-skip-tls-verify: true\n"
		}
		raw = []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: cluster
  cluster:
%susers:
- name: user
  user:
    token: %s
contexts:
- name: ctx
  context:
    cluster: cluster
    user: user
current-context: ctx
`, clusterBlock, strings.TrimSpace(cred.Credential)))
	default:
		cleanup()
		return "", func() {}, fmt.Errorf("不支持的 AuthType=%s（Helm CLI 需 kubeconfig 或 token）", cred.AuthType)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}
