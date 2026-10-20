package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/httpx"
)

// AgentAuth Agent 鉴权。
type AgentAuth struct {
	BaseURL  string
	Username string
	Password string
}

// LogicalDest 产物落点。
type LogicalDest struct {
	Kind      string `json:"kind"`
	LocalPath string `json:"local_path,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
}

// LogicalBackupRequest Agent 逻辑备份请求。
type LogicalBackupRequest struct {
	Engine    string      `json:"engine"`
	Host      string      `json:"host"`
	Port      int         `json:"port"`
	Username  string      `json:"username"`
	Password  string      `json:"password"`
	Databases []string    `json:"databases"`
	Parallel  int         `json:"parallel"`
	Format    string      `json:"format"`
	Dest      LogicalDest `json:"dest"`
}

// LogicalBackupResponse Agent 响应。
type LogicalBackupResponse struct {
	OK        bool   `json:"ok"`
	JobID     string `json:"job_id,omitempty"`
	Status    string `json:"status,omitempty"`
	Message   string `json:"message,omitempty"`
	LocalPath string `json:"local_path,omitempty"`
	ObjectKey string `json:"object_key,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
	Checksum  string `json:"checksum,omitempty"`
	SetLabel  string `json:"set_label,omitempty"`
	ExtraJSON string `json:"extra_json,omitempty"`
}

func httpNewPost(ctx context.Context, url string, payload []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func agentDo(req *http.Request, timeout time.Duration) (*http.Response, error) {
	return httpx.AgentHTTP(timeout).Do(req)
}

func agentDoLong(req *http.Request) (*http.Response, error) {
	return httpx.AgentHTTPLong(6 * time.Hour).Do(req)
}

func readBodyLimit(r io.Reader, n int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, n))
}

func applyBasicAuth(req *http.Request, auth AgentAuth) {
	if strings.TrimSpace(auth.Username) != "" {
		req.SetBasicAuth(auth.Username, auth.Password)
	}
}

// LogicalRestoreRequest Agent 逻辑恢复请求。
type LogicalRestoreRequest struct {
	Engine    string      `json:"engine"`
	Host      string      `json:"host"`
	Port      int         `json:"port"`
	Username  string      `json:"username"`
	Password  string      `json:"password"`
	Database  string      `json:"database,omitempty"`
	Databases []string    `json:"databases,omitempty"`
	LocalPath string      `json:"local_path,omitempty"`
	ObjectKey string      `json:"object_key,omitempty"`
	Dest      LogicalDest `json:"dest,omitempty"`
}

// PhysicalRestoreRequest Agent 物理恢复请求（同机 data_dir）。
// ChainDirs 为 base→…→tip 的本机备份目录链；仅 tip（全量）时可只填 LocalPath。
type PhysicalRestoreRequest struct {
	Engine      string   `json:"engine"`
	LocalPath   string   `json:"local_path,omitempty"`
	ObjectKey   string   `json:"object_key,omitempty"`
	DataDir     string   `json:"data_dir"`
	ConfirmStop bool     `json:"confirm_stop"`
	ChainDirs   []string `json:"chain_dirs,omitempty"`
}

// PITRRestoreRequest Agent 时间点恢复：物理还原 + WAL/binlog 追到 recover_to。
type PITRRestoreRequest struct {
	Engine         string `json:"engine"`
	LocalPath      string `json:"local_path,omitempty"`
	ObjectKey      string `json:"object_key,omitempty"`
	DataDir        string `json:"data_dir"`
	ConfirmStop    bool   `json:"confirm_stop"`
	RecoverTo      string `json:"recover_to"`
	WalOrBinlogDir string `json:"wal_or_binlog_dir"`
}

// CallPITRRestore 提交 PITR 并轮询 job。
func CallPITRRestore(ctx context.Context, auth AgentAuth, req PITRRestoreRequest) (*LogicalBackupResponse, error) {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("Agent Base URL 为空")
	}
	payload, _ := json.Marshal(req)
	httpReq, err := httpNewPost(ctx, base+"/v1/restore/pitr", payload)
	if err != nil {
		return nil, err
	}
	applyBasicAuth(httpReq, auth)
	resp, err := agentDo(httpReq, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("Backup Agent 调用失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := readBodyLimit(resp.Body, 1<<20)
	var out LogicalBackupResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("Agent 响应无效 HTTP %d: %s", resp.StatusCode, string(body))
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusAccepted {
		msg := out.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent PITR 失败: %s", msg)
	}
	if out.OK && !strings.EqualFold(out.Status, "running") && out.JobID == "" {
		return &out, nil
	}
	if out.JobID == "" {
		msg := out.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent PITR 失败: %s", msg)
	}
	return waitBackupJob(ctx, auth, out.JobID)
}

// CallPhysicalRestore 提交物理全量恢复并轮询 job。
func CallPhysicalRestore(ctx context.Context, auth AgentAuth, req PhysicalRestoreRequest) (*LogicalBackupResponse, error) {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("Agent Base URL 为空")
	}
	payload, _ := json.Marshal(req)
	httpReq, err := httpNewPost(ctx, base+"/v1/restore/physical", payload)
	if err != nil {
		return nil, err
	}
	applyBasicAuth(httpReq, auth)
	resp, err := agentDo(httpReq, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("Backup Agent 调用失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := readBodyLimit(resp.Body, 1<<20)
	var out LogicalBackupResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("Agent 响应无效 HTTP %d: %s", resp.StatusCode, string(body))
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusAccepted {
		msg := out.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent 物理恢复失败: %s", msg)
	}
	if out.OK && !strings.EqualFold(out.Status, "running") && out.JobID == "" {
		return &out, nil
	}
	if out.JobID == "" {
		msg := out.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent 物理恢复失败: %s", msg)
	}
	return waitBackupJob(ctx, auth, out.JobID)
}

// CallLogicalRestore 提交逻辑恢复并轮询 job。
func CallLogicalRestore(ctx context.Context, auth AgentAuth, req LogicalRestoreRequest) (*LogicalBackupResponse, error) {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("Agent Base URL 为空")
	}
	payload, _ := json.Marshal(req)
	httpReq, err := httpNewPost(ctx, base+"/v1/restore/logical", payload)
	if err != nil {
		return nil, err
	}
	applyBasicAuth(httpReq, auth)
	resp, err := agentDo(httpReq, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("Backup Agent 调用失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := readBodyLimit(resp.Body, 1<<20)
	var out LogicalBackupResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("Agent 响应无效 HTTP %d: %s", resp.StatusCode, string(body))
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusAccepted {
		msg := out.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent 恢复失败: %s", msg)
	}
	if out.OK && !strings.EqualFold(out.Status, "running") && out.JobID == "" {
		return &out, nil
	}
	if out.JobID == "" {
		msg := out.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent 恢复失败: %s", msg)
	}
	return waitBackupJob(ctx, auth, out.JobID)
}

// CallLogicalBackup 提交逻辑备份并轮询 job（Agent 返回 202）。
func CallLogicalBackup(ctx context.Context, auth AgentAuth, req LogicalBackupRequest) (*LogicalBackupResponse, error) {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("Agent Base URL 为空")
	}
	payload, _ := json.Marshal(req)
	httpReq, err := httpNewPost(ctx, base+"/v1/backup/logical", payload)
	if err != nil {
		return nil, err
	}
	applyBasicAuth(httpReq, auth)
	resp, err := agentDo(httpReq, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("Backup Agent 调用失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := readBodyLimit(resp.Body, 1<<20)
	var out LogicalBackupResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("Agent 响应无效 HTTP %d: %s", resp.StatusCode, string(body))
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusAccepted {
		msg := out.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent 备份失败: %s", msg)
	}
	// 兼容旧同步 Agent：一次返回完整结果
	if out.OK && out.LocalPath != "" && !strings.EqualFold(out.Status, "running") {
		return &out, nil
	}
	if out.JobID == "" {
		msg := out.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent 备份失败: %s", msg)
	}
	return waitBackupJob(ctx, auth, out.JobID)
}

func waitBackupJob(ctx context.Context, auth AgentAuth, jobID string) (*LogicalBackupResponse, error) {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	deadline := time.Now().Add(6 * time.Hour)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("备份 job %s 超时", jobID)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/backup/jobs/"+jobID, nil)
		if err != nil {
			return nil, err
		}
		applyBasicAuth(req, auth)
		resp, err := agentDo(req, 30*time.Second)
		if err != nil {
			return nil, err
		}
		body, _ := readBodyLimit(resp.Body, 1<<20)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("查询备份 job HTTP %d: %s", resp.StatusCode, string(body))
		}
		var wrap struct {
			OK      bool                   `json:"ok"`
			JobID   string                 `json:"job_id"`
			Status  string                 `json:"status"`
			Message string                 `json:"message"`
			Job     *LogicalBackupResponse `json:"job"`
			LogicalBackupResponse
		}
		if err := json.Unmarshal(body, &wrap); err != nil {
			return nil, fmt.Errorf("job 响应无效: %s", string(body))
		}
		out := wrap.LogicalBackupResponse
		if wrap.Job != nil {
			out = *wrap.Job
			out.OK = wrap.OK
		}
		if wrap.JobID != "" {
			out.JobID = wrap.JobID
		} else if out.JobID == "" {
			out.JobID = jobID
		}
		if wrap.Status != "" {
			out.Status = wrap.Status
		}
		if wrap.Message != "" && out.Message == "" {
			out.Message = wrap.Message
		}
		st := strings.ToLower(out.Status)
		if st == "" && wrap.Job != nil {
			st = strings.ToLower(wrap.Job.Status)
		}
		switch st {
		case "success", "ok":
			out.OK = true
			return &out, nil
		case "failed", "error":
			msg := out.Message
			if msg == "" {
				msg = string(body)
			}
			return &out, fmt.Errorf("Agent 备份失败: %s", msg)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// ProbeAgent 探活。
func ProbeAgent(ctx context.Context, auth AgentAuth) error {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return err
	}
	applyBasicAuth(req, auth)
	resp, err := agentDo(req, 10*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// RedisRDBRequest Agent 拉取 Redis RDB。
type RedisRDBRequest struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username,omitempty"`
	Password       string `json:"password,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// RedisRDBStream Agent 返回的 RDB 流（调用方必须 Close）。
type RedisRDBStream struct {
	Body     io.ReadCloser
	SizeHint int64
	JobID    string
	Checksum string
}

// CallRedisRDB 调用 Agent 拉取 RDB，成功时 Content-Type 为 octet-stream。
func CallRedisRDB(ctx context.Context, auth AgentAuth, req RedisRDBRequest) (*RedisRDBStream, error) {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("Agent Base URL 为空")
	}
	payload, _ := json.Marshal(req)
	httpReq, err := httpNewPost(ctx, base+"/v1/backup/redis-rdb", payload)
	if err != nil {
		return nil, err
	}
	applyBasicAuth(httpReq, auth)
	resp, err := agentDoLong(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Backup Agent 调用失败: %w", err)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := readBodyLimit(resp.Body, 1<<20)
		return nil, fmt.Errorf("Agent HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	// 失败时 Agent 返回 JSON {ok:false}
	if strings.Contains(ct, "application/json") {
		defer resp.Body.Close()
		body, _ := readBodyLimit(resp.Body, 1<<20)
		var fail struct {
			OK      bool   `json:"ok"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &fail)
		msg := fail.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent 拉取 RDB 失败: %s", msg)
	}
	var sizeHint int64
	if v := resp.Header.Get("X-Size-Bytes"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			sizeHint = n
		}
	}
	return &RedisRDBStream{
		Body:     resp.Body,
		SizeHint: sizeHint,
		JobID:    resp.Header.Get("X-Job-Id"),
		Checksum: resp.Header.Get("X-Checksum"),
	}, nil
}

// CallCapabilities 拉取 Agent 能力报告。
func CallCapabilities(ctx context.Context, auth AgentAuth, groups []string) (*CapabilitiesReport, error) {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("Agent Base URL 为空")
	}
	q := ""
	if len(groups) > 0 {
		q = "?groups=" + strings.Join(NormalizeGroups(groups), ",")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/capabilities"+q, nil)
	if err != nil {
		return nil, err
	}
	applyBasicAuth(req, auth)
	resp, err := agentDo(req, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := readBodyLimit(resp.Body, 1<<20)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var rep CapabilitiesReport
	if err := json.Unmarshal(body, &rep); err != nil {
		return nil, fmt.Errorf("capabilities 响应无效: %w", err)
	}
	return &rep, nil
}

// CallHAEnvCheck 调用 Agent 高可用环境检查。
func CallHAEnvCheck(ctx context.Context, auth AgentAuth, kind, conf string) (map[string]any, error) {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	payload, _ := json.Marshal(map[string]string{"kind": kind, "conf": conf})
	httpReq, err := httpNewPost(ctx, base+"/v1/ha/env-check", payload)
	if err != nil {
		return nil, err
	}
	applyBasicAuth(httpReq, auth)
	resp, err := agentDo(httpReq, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := readBodyLimit(resp.Body, 1<<20)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("env-check HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("env-check 响应无效: %s", string(body))
	}
	return out, nil
}
