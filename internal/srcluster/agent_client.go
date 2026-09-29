package srcluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"auto-dba/internal/httpx"
)

// AgentClient 控制面 → Node Agent HTTP。
type AgentClient struct {
	HTTP *http.Client
	// HTTPLong 无首包超时：systemctl start/stop/restart 在 Agent 侧同步执行，可能远超 60s。
	HTTPLong *http.Client
}

// systemdCtrlTimeout 启停整体上限；成败交给 systemd 自身的 TimeoutStartSec/TimeoutStopSec。
const systemdCtrlTimeout = 10 * time.Minute

func NewAgentClient() *AgentClient {
	return &AgentClient{HTTP: httpx.AgentHTTP(120 * time.Second), HTTPLong: httpx.AgentHTTPLong(systemdCtrlTimeout)}
}

// systemdLongAction 需要等 systemd job 完成的动作。
func systemdLongAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "start", "stop", "restart":
		return true
	}
	return false
}

type AgentAuth struct {
	BaseURL  string
	Username string
	Password string
}

type PrecheckReq struct {
	Role      string `json:"role"` // fe | be
	ChangeID  string `json:"change_id,omitempty"`
	StepID    string `json:"step_id,omitempty"`
	HTTPPort  int    `json:"http_port,omitempty"`
	QueryPort int    `json:"query_port,omitempty"`
	EditPort  int    `json:"edit_port,omitempty"`
	HBPort    int    `json:"heartbeat_port,omitempty"`
	DataRoot  string `json:"data_root,omitempty"` // BE：按三条规则校验数据目录
}

type PrecheckItem struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Level   string `json:"level,omitempty"` // warn：仅提醒
	Message string `json:"message,omitempty"`
}

type PrecheckResp struct {
	OK    bool           `json:"ok"`
	Items []PrecheckItem `json:"items,omitempty"`
	Msg   string         `json:"message,omitempty"`
}

type DiskInitReq struct {
	Device       string `json:"device"`
	MountPoint   string `json:"mount_point"`
	FSType       string `json:"fstype"`
	PersistFstab bool   `json:"persist_fstab"`
	Auto         bool   `json:"auto,omitempty"` // 按数据目录三条规则：已挂载非系统盘直接用 / 系统盘拒绝 / 不存在则格式化空闲盘
	ChangeID     string `json:"change_id,omitempty"`
	StepID       string `json:"step_id,omitempty"`
	DryRun       bool   `json:"dry_run,omitempty"`
}

type FilePutReq struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Mode     string `json:"mode,omitempty"`
	ChangeID string `json:"change_id,omitempty"`
	StepID   string `json:"step_id,omitempty"`
}

type EnsureFSReq struct {
	Paths        []string `json:"paths"`
	BackupSuffix string   `json:"backup_suffix,omitempty"` // 非空：原地 bak.<suffix>
	ChangeID     string   `json:"change_id,omitempty"`
	StepID       string   `json:"step_id,omitempty"`
	DryRun       bool     `json:"dry_run,omitempty"`
}

type SystemdCtrlReq struct {
	Action   string `json:"action"` // start|stop|restart|enable|status|reset-failed
	Unit     string `json:"unit"`
	ChangeID string `json:"change_id,omitempty"`
	StepID   string `json:"step_id,omitempty"`
	DryRun   bool   `json:"dry_run,omitempty"`
}

type ExecReq struct {
	Command  string   `json:"command"`
	Args     []string `json:"args,omitempty"`
	ChangeID string   `json:"change_id,omitempty"`
	StepID   string   `json:"step_id,omitempty"`
	DryRun   bool     `json:"dry_run,omitempty"`
}

type JavaCheckReq struct {
	Role         string `json:"role"`
	SoftwarePath string `json:"software_path"`
	JDKMin       int    `json:"jdk_min"`
	JDKMax       int    `json:"jdk_max"`
}

type JavaCandidate struct {
	Home      string `json:"home"`
	Path      string `json:"path"`
	Major     int    `json:"major"`
	Version   string `json:"version"`
	Preferred bool   `json:"preferred,omitempty"` // /home/jdk 约定目录
}

type JavaCheckResp struct {
	OK         bool            `json:"ok"`
	Major      int             `json:"major"`
	Version    string          `json:"version"`
	Path       string          `json:"path"`
	Source     string          `json:"source"` // conf：fe.conf/be.conf 中 JAVA_HOME；path：/usr/bin/java
	ConfFile   string          `json:"conf_file,omitempty"`
	Message    string          `json:"message"`
	Candidates []JavaCandidate `json:"candidates,omitempty"`
	// 系统默认 java（/usr/bin/java 解析后）；旧 Agent 无 SystemChecked
	SystemChecked bool   `json:"system_checked,omitempty"`
	SystemPath    string `json:"system_path,omitempty"`
	SystemMajor   int    `json:"system_major,omitempty"`
	SystemVersion string `json:"system_version,omitempty"`
	SystemMessage string `json:"system_message,omitempty"`
}

type JavaInstallReq struct {
	DownloadURL string `json:"download_url,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	JavaHome    string `json:"java_home,omitempty"` // 本机已有 JDK：只切换 update-alternatives
	Priority    int    `json:"priority"`
	DryRun      bool   `json:"dry_run,omitempty"`
}

// OldAgentJavaMsg Node Agent 不支持 /v1/sr/java/*。
const OldAgentJavaMsg = "Node Agent 版本过旧，不支持 JDK 检查，请重新 SSH 下发 Agent"

type JobResp struct {
	OK      bool   `json:"ok"`
	JobID   string `json:"job_id,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

func (c *AgentClient) Probe(ctx context.Context, auth AgentAuth) error {
	_, err := c.doJSON(ctx, http.MethodGet, auth, "/healthz", nil, nil)
	return err
}

func (c *AgentClient) Precheck(ctx context.Context, auth AgentAuth, req PrecheckReq) (*PrecheckResp, error) {
	var out PrecheckResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/precheck", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) JavaCheck(ctx context.Context, auth AgentAuth, req JavaCheckReq) (*JavaCheckResp, error) {
	var out JavaCheckResp
	code, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/java/check", req, &out)
	if code == http.StatusNotFound {
		return nil, fmt.Errorf("%s", OldAgentJavaMsg)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) JavaInstall(ctx context.Context, auth AgentAuth, req JavaInstallReq) (*JobResp, error) {
	var out JobResp
	code, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/java/install", req, &out)
	if code == http.StatusNotFound {
		return nil, fmt.Errorf("%s", OldAgentJavaMsg)
	}
	if err != nil {
		return nil, err
	}
	if code == http.StatusAccepted && out.JobID != "" {
		return c.waitJob(ctx, auth, out.JobID)
	}
	return &out, nil
}

func (c *AgentClient) DiskInit(ctx context.Context, auth AgentAuth, req DiskInitReq) (*JobResp, error) {
	var out JobResp
	code, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/disk/init", req, &out)
	if err != nil {
		return nil, err
	}
	if code == http.StatusAccepted && out.JobID != "" {
		return c.waitJob(ctx, auth, out.JobID)
	}
	return &out, nil
}

func (c *AgentClient) EnsureFS(ctx context.Context, auth AgentAuth, req EnsureFSReq) (*JobResp, error) {
	var out JobResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/fs/ensure", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) FilePut(ctx context.Context, auth AgentAuth, req FilePutReq) (*JobResp, error) {
	var out JobResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/file/put", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) ArtifactInstall(ctx context.Context, auth AgentAuth, body map[string]any) (*JobResp, error) {
	var out JobResp
	code, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/artifact/install", body, &out)
	if err != nil {
		return nil, err
	}
	if code == http.StatusAccepted && out.JobID != "" {
		return c.waitJob(ctx, auth, out.JobID)
	}
	if out.OK || out.Status == "success" {
		return &out, nil
	}
	if out.Message != "" && !out.OK {
		return &out, fmt.Errorf("%s", out.Message)
	}
	return &out, nil
}

func (c *AgentClient) SystemdInstall(ctx context.Context, auth AgentAuth, body map[string]any) (*JobResp, error) {
	var out JobResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/systemd/install", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) SystemdCtrl(ctx context.Context, auth AgentAuth, req SystemdCtrlReq) (*JobResp, error) {
	var out JobResp
	cli := c.HTTP
	if systemdLongAction(req.Action) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, systemdCtrlTimeout)
		defer cancel()
		if c.HTTPLong != nil {
			cli = c.HTTPLong
		}
	}
	if _, err := c.doJSONWith(ctx, cli, http.MethodPost, auth, "/v1/sr/systemd/ctrl", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type DiagReq struct {
	Unit         string `json:"unit"`
	Role         string `json:"role"`
	SoftwarePath string `json:"software_path"`
	Lines        int    `json:"lines,omitempty"`
}

type ArtifactRestoreReq struct {
	TargetDir    string   `json:"target_dir"`
	BackupSuffix string   `json:"backup_suffix"`
	Dirs         []string `json:"dirs"`
	DryRun       bool     `json:"dry_run,omitempty"`
}

// OldAgentUpgradeMsg Node Agent 不支持 diag / artifact/restore。
const OldAgentUpgradeMsg = "Node Agent 版本过旧，不支持该操作，请在「Node Agent」页重新下发 Agent 后重试"

// Diag 启动失败诊断；返回的 int 为 HTTP 状态码（404 表示旧 Agent）。
func (c *AgentClient) Diag(ctx context.Context, auth AgentAuth, req DiagReq) (*JobResp, int, error) {
	var out JobResp
	code, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/diag", req, &out)
	if err != nil {
		return nil, code, err
	}
	return &out, code, nil
}

func (c *AgentClient) ArtifactRestore(ctx context.Context, auth AgentAuth, req ArtifactRestoreReq) (*JobResp, error) {
	var out JobResp
	code, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/artifact/restore", req, &out)
	if code == http.StatusNotFound {
		return nil, fmt.Errorf("%s", OldAgentUpgradeMsg)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) Exec(ctx context.Context, auth AgentAuth, req ExecReq) (*JobResp, error) {
	var out JobResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/sr/exec", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) waitJob(ctx context.Context, auth AgentAuth, jobID string) (*JobResp, error) {
	deadline := time.Now().Add(30 * time.Minute)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("agent job %s timeout", jobID)
		}
		var out JobResp
		if _, err := c.doJSON(ctx, http.MethodGet, auth, "/v1/sr/jobs/"+jobID, nil, &out); err != nil {
			return nil, err
		}
		st := strings.ToLower(out.Status)
		if st == "success" || st == "ok" {
			out.OK = true
			return &out, nil
		}
		if st == "failed" || st == "error" {
			return &out, fmt.Errorf("agent job failed: %s", out.Message)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *AgentClient) doJSON(ctx context.Context, method string, auth AgentAuth, path string, body any, out any) (int, error) {
	return c.doJSONWith(ctx, c.HTTP, method, auth, path, body, out)
}

func (c *AgentClient) doJSONWith(ctx context.Context, cli *http.Client, method string, auth AgentAuth, path string, body any, out any) (int, error) {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	if base == "" {
		return 0, fmt.Errorf("agent base_url 为空")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(auth.Username) != "" {
		req.SetBasicAuth(auth.Username, auth.Password)
	}
	if cli == nil {
		cli = http.DefaultClient
	}
	resp, err := cli.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("agent %s %s: %s", method, path, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode agent response: %w", err)
		}
	}
	return resp.StatusCode, nil
}
