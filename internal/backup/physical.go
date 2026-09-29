package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// PhysicalBackupRequest Agent 物理备份请求。
type PhysicalBackupRequest struct {
	Engine           string      `json:"engine"`
	Method           string      `json:"method"` // physical_full | physical_incr
	Host             string      `json:"host"`
	Port             int         `json:"port"`
	Username         string      `json:"username"`
	Password         string      `json:"password"`
	Parallel         int         `json:"parallel"`
	Compress         bool        `json:"compress"`
	IncrementalBased string      `json:"incremental_basedir,omitempty"` // MySQL xtrabackup 增量基目录
	Dest             LogicalDest `json:"dest"`
}

// PhysicalBackupResponse Agent 物理备份响应。
type PhysicalBackupResponse struct {
	OK        bool   `json:"ok"`
	JobID     string `json:"job_id,omitempty"`
	Message   string `json:"message,omitempty"`
	LocalPath string `json:"local_path,omitempty"`
	ObjectKey string `json:"object_key,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
	Checksum  string `json:"checksum,omitempty"`
	SetLabel  string `json:"set_label,omitempty"`
	ExtraJSON string `json:"extra_json,omitempty"`
}

func (e *Engine) runPhysical(ctx context.Context, p *store.BackupPolicy, run *store.BackupRun) (*dumpResult, error) {
	if strings.TrimSpace(p.AgentID) == "" {
		return nil, fmt.Errorf("物理备份必须绑定 Backup Agent（不可内嵌执行）")
	}
	inst, err := e.Store.GetInstance(p.InstanceID)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("实例不存在")
	}
	engine := store.NormalizeDBType(inst.DBType)
	if engine != "postgres" && engine != "mysql" && engine != "tidb" && engine != "oceanbase" {
		return nil, fmt.Errorf("物理备份仅支持 PostgreSQL / MySQL，当前类型 %s", inst.DBType)
	}
	storage, err := e.Store.GetBackupStorage(p.StorageID)
	if err != nil || storage == nil {
		return nil, fmt.Errorf("存储配置不存在")
	}
	pass, err := secret.Decrypt(e.Secret, inst.Password)
	if err != nil {
		return nil, fmt.Errorf("解密实例密码失败: %w", err)
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}

	method := strings.TrimSpace(p.Method)
	if method == "" {
		method = store.BackupMethodPhysicalFull
	}
	run.Method = method

	var parent *store.BackupRun
	incrBased := ""
	if method == store.BackupMethodPhysicalIncr {
		parent, err = e.Store.LastSuccessfulPhysicalBase(p.ID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, fmt.Errorf("物理增量需要先有成功的物理全量/增量基；请先执行 physical_full")
		}
		run.ParentRunID = parent.ID
		incrBased = strings.TrimSpace(parent.LocalPath)
		if incrBased == "" {
			return nil, fmt.Errorf("基备份缺少 local_path，无法做增量（Agent 目录产物）")
		}
		if engine == "postgres" {
			// PG 原生 pg_basebackup 无 MySQL 式增量目录；提示用全量+WAL（PITR）或后续 pgBackRest
			return nil, fmt.Errorf("PostgreSQL 物理增量请使用连续 WAL/pgBackRest；当前 Agent 支持 physical_full（pg_basebackup）。MySQL 可用 physical_incr（xtrabackup）")
		}
	}

	agent, err := e.Store.GetBackupAgent(p.AgentID)
	if err != nil || agent == nil {
		return nil, fmt.Errorf("Backup Agent 不存在")
	}
	agentPass := ""
	if agent.PasswordEnc != "" {
		agentPass, err = secret.Decrypt(e.Secret, agent.PasswordEnc)
		if err != nil {
			return nil, fmt.Errorf("解密 Agent 密码失败: %w", err)
		}
	}
	auth := AgentAuth{BaseURL: agent.BaseURL, Username: agent.Username, Password: agentPass}
	req := PhysicalBackupRequest{
		Engine:           engine,
		Method:           method,
		Host:             host,
		Port:             inst.Port,
		Username:         inst.Username,
		Password:         pass,
		Parallel:         p.Parallel,
		Compress:         p.Compress,
		IncrementalBased: incrBased,
		Dest: LogicalDest{
			Kind:      storage.Kind,
			LocalPath: storage.LocalPath,
			Endpoint:  storage.Endpoint,
			Bucket:    storage.Bucket,
			Prefix:    storage.Prefix,
			AccessKey: storage.AccessKey,
		},
	}
	if storage.SecretKeyEnc != "" {
		if sk, derr := secret.Decrypt(e.Secret, storage.SecretKeyEnc); derr == nil {
			req.Dest.SecretKey = sk
		}
	}

	resp, err := CallPhysicalBackup(ctx, auth, req)
	if err != nil {
		return nil, err
	}
	if resp.SizeBytes <= 0 {
		return nil, fmt.Errorf("物理备份产物大小为 0，校验失败")
	}
	run.SetLabel = resp.SetLabel
	if run.SetLabel == "" {
		run.SetLabel = time.Now().UTC().Format("20060102T150405Z")
	}
	if resp.ExtraJSON != "" {
		run.ExtraJSON = resp.ExtraJSON
	} else if parent != nil {
		extra, _ := json.Marshal(map[string]any{"parent_run_id": parent.ID, "parent_path": parent.LocalPath})
		run.ExtraJSON = string(extra)
	}
	return &dumpResult{
		LocalPath: resp.LocalPath,
		ObjectKey: resp.ObjectKey,
		SizeBytes: resp.SizeBytes,
		Checksum:  resp.Checksum,
		Log:       trimLog(resp.Message),
	}, nil
}

// CallPhysicalBackup 提交物理备份并轮询 job（Agent 返回 202）。
func CallPhysicalBackup(ctx context.Context, auth AgentAuth, req PhysicalBackupRequest) (*PhysicalBackupResponse, error) {
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("Agent Base URL 为空")
	}
	payload, _ := json.Marshal(req)
	httpReq, err := httpNewPost(ctx, base+"/v1/backup/physical", payload)
	if err != nil {
		return nil, err
	}
	applyBasicAuth(httpReq, auth)
	resp, err := agentDo(httpReq, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("Backup Agent 物理备份调用失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := readBodyLimit(resp.Body, 1<<20)
	var submitted PhysicalBackupResponse
	var meta struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &submitted); err != nil {
		return nil, fmt.Errorf("Agent 响应无效 HTTP %d: %s", resp.StatusCode, string(body))
	}
	_ = json.Unmarshal(body, &meta)
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusAccepted {
		msg := submitted.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent 物理备份失败: %s", msg)
	}
	// 兼容旧同步 Agent
	if submitted.OK && submitted.LocalPath != "" && !strings.EqualFold(meta.Status, "running") {
		return &submitted, nil
	}
	if submitted.JobID == "" {
		msg := submitted.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("Agent 物理备份失败: %s", msg)
	}
	fin, err := waitBackupJob(ctx, auth, submitted.JobID)
	if err != nil {
		return nil, err
	}
	return &PhysicalBackupResponse{
		OK:        fin.OK,
		JobID:     fin.JobID,
		Message:   fin.Message,
		LocalPath: fin.LocalPath,
		ObjectKey: fin.ObjectKey,
		SizeBytes: fin.SizeBytes,
		Checksum:  fin.Checksum,
		SetLabel:  fin.SetLabel,
		ExtraJSON: fin.ExtraJSON,
	}, nil
}
