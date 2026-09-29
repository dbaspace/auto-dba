package dbdeploy

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

// AgentClient 控制面 → Db Agent HTTP（/v1/db/*）。
type AgentClient struct {
	HTTP *http.Client
}

func NewAgentClient() *AgentClient {
	return &AgentClient{HTTP: httpx.AgentHTTP(120 * time.Second)}
}

type AgentAuth struct {
	BaseURL  string
	Username string
	Password string
}

type JobResp struct {
	OK      bool   `json:"ok"`
	JobID   string `json:"job_id,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

type PrecheckResp struct {
	OK    bool             `json:"ok"`
	Items []map[string]any `json:"items,omitempty"`
	Msg   string           `json:"message,omitempty"`
}

type ReplVerifyResp struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Msg    string `json:"message,omitempty"`
}

func (c *AgentClient) Probe(ctx context.Context, auth AgentAuth) error {
	_, err := c.doJSON(ctx, http.MethodGet, auth, "/healthz", nil, nil)
	return err
}

func (c *AgentClient) Precheck(ctx context.Context, auth AgentAuth, body map[string]any) (*PrecheckResp, error) {
	var out PrecheckResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/db/precheck", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) EnsureFS(ctx context.Context, auth AgentAuth, body map[string]any) (*JobResp, error) {
	var out JobResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/db/fs/ensure", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) FilePut(ctx context.Context, auth AgentAuth, body map[string]any) (*JobResp, error) {
	var out JobResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/db/file/put", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) ArtifactInstall(ctx context.Context, auth AgentAuth, body map[string]any) (*JobResp, error) {
	var out JobResp
	code, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/db/artifact/install", body, &out)
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
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/db/systemd/install", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) SystemdCtrl(ctx context.Context, auth AgentAuth, body map[string]any) (*JobResp, error) {
	var out JobResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/db/systemd/ctrl", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) Exec(ctx context.Context, auth AgentAuth, body map[string]any) (*JobResp, error) {
	var out JobResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/db/exec", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AgentClient) ReplVerify(ctx context.Context, auth AgentAuth, body map[string]any) (*ReplVerifyResp, error) {
	var out ReplVerifyResp
	if _, err := c.doJSON(ctx, http.MethodPost, auth, "/v1/db/repl/verify", body, &out); err != nil {
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
			return nil, fmt.Errorf("db-agent job %s timeout", jobID)
		}
		var out JobResp
		if _, err := c.doJSON(ctx, http.MethodGet, auth, "/v1/db/jobs/"+jobID, nil, &out); err != nil {
			return nil, err
		}
		st := strings.ToLower(out.Status)
		if st == "success" || st == "ok" {
			out.OK = true
			return &out, nil
		}
		if st == "failed" || st == "error" {
			return &out, fmt.Errorf("db-agent job failed: %s", out.Message)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *AgentClient) doJSON(ctx context.Context, method string, auth AgentAuth, path string, body any, out any) (int, error) {
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
	cli := c.HTTP
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
