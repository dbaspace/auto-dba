package redisrdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/backup"
	"auto-dba/internal/redisquery"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// Service Redis RDB 内存分析服务。
type Service struct {
	Store  *store.DB
	Secret string
	Root   string

	mu sync.Mutex
}

func New(st *store.DB, secret, root string) *Service {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join(os.TempDir(), "auto-dba-redis-rdb")
	}
	_ = os.MkdirAll(root, 0o750)
	return &Service{Store: st, Secret: secret, Root: root}
}

func (s *Service) jobDir(id string) string {
	return filepath.Join(s.Root, id)
}

func (s *Service) ListInstances(ctx context.Context, q string) ([]*InstanceItem, error) {
	_ = ctx
	list, err := s.Store.ListInstances("redis", q)
	if err != nil {
		return nil, err
	}
	out := make([]*InstanceItem, 0, len(list))
	for i := range list {
		inst := &list[i]
		host := strings.TrimSpace(inst.Host)
		if host == "" {
			host = strings.TrimSpace(inst.DomainName)
		}
		out = append(out, &InstanceItem{
			ID:          inst.ID,
			Name:        strings.TrimSpace(inst.Name),
			Host:        host,
			Port:        int64(inst.Port),
			Environment: inst.EnvID,
			Region:      inst.RegionID,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateUploadJob 保存上传的 RDB 并异步分析。
func (s *Service) CreateUploadJob(ctx context.Context, instanceID, note, createdBy, fileName string, size int64, src io.Reader) (*Job, error) {
	if size <= 0 {
		return nil, fmt.Errorf("空文件")
	}
	if size > MaxUploadSize {
		return nil, fmt.Errorf("文件过大，上限 %d MB", MaxUploadSize>>20)
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext != "" && ext != ".rdb" {
		return nil, fmt.Errorf("仅支持 .rdb 文件")
	}
	id := uuid.NewString()
	dir := s.jobDir(id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	rdbPath := filepath.Join(dir, "dump.rdb")
	dst, err := os.Create(rdbPath)
	if err != nil {
		return nil, err
	}
	written, err := io.Copy(dst, io.LimitReader(src, MaxUploadSize+1))
	_ = dst.Close()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if written > MaxUploadSize {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("文件过大，上限 %d MB", MaxUploadSize>>20)
	}

	instName := ""
	if instanceID != "" {
		if inst, e := s.Store.GetInstance(instanceID); e == nil && inst != nil {
			instName = inst.Name
		}
	}
	job := &Job{
		ID:           id,
		InstanceID:   strings.TrimSpace(instanceID),
		InstanceName: instName,
		FileName:     filepath.Base(fileName),
		FileSize:     written,
		Status:       JobPending,
		Note:         strings.TrimSpace(note),
		Source:       "upload",
		CreatedAt:    time.Now().UTC(),
		CreatedBy:    createdBy,
	}
	if err := s.saveJob(job); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	go s.runAnalyze(id)
	return job, nil
}

// ListAgents 列出已登记平台 Agent（RDB 能力可用者优先展示全部，前端可按 rdb_ready 过滤）。
func (s *Service) ListAgents() ([]*AgentItem, error) {
	list, err := s.Store.ListBackupAgents()
	if err != nil {
		return nil, err
	}
	out := make([]*AgentItem, 0, len(list))
	for i := range list {
		a := &list[i]
		rep := backup.DecodeCapabilities(a.Capabilities)
		rdbOK := backup.HasCapability(rep, "redis_rdb")
		// 旧数据无 capabilities：若 engines 含 redis 或 enabled_groups 含 rdb 则视为可用
		if !rdbOK {
			eg := strings.ToLower(a.EnabledGroups + "," + a.Engines)
			if strings.Contains(eg, "redis") || strings.Contains(eg, "rdb") {
				rdbOK = true
			}
		}
		out = append(out, &AgentItem{
			ID:            a.ID,
			Name:          a.Name,
			BaseURL:       a.BaseURL,
			HostInstance:  a.HostInstance,
			Remark:        a.Remark,
			EnabledGroups: a.EnabledGroups,
			Capabilities:  a.Capabilities,
			RDBReady:      rdbOK,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RDBReady != out[j].RDBReady {
			return out[i].RDBReady
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// CreateAgentFetchJob 经 Backup Agent 拉取 RDB 并异步分析。
func (s *Service) CreateAgentFetchJob(ctx context.Context, instanceID, agentID, note, createdBy string) (*Job, error) {
	instanceID = strings.TrimSpace(instanceID)
	agentID = strings.TrimSpace(agentID)
	if instanceID == "" {
		return nil, fmt.Errorf("instance_id required")
	}
	if agentID == "" {
		return nil, fmt.Errorf("agent_id required")
	}
	inst, err := s.Store.GetInstance(instanceID)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("Redis 实例不存在")
	}
	if !strings.EqualFold(strings.TrimSpace(inst.DBType), "redis") {
		return nil, fmt.Errorf("非 Redis 实例")
	}
	agent, err := s.Store.GetBackupAgent(agentID)
	if err != nil || agent == nil {
		return nil, fmt.Errorf("平台 Agent 不存在")
	}
	capRep := backup.DecodeCapabilities(agent.Capabilities)
	if !backup.HasCapability(capRep, "redis_rdb") {
		eg := strings.ToLower(agent.EnabledGroups + "," + agent.Engines)
		if !strings.Contains(eg, "redis") && !strings.Contains(eg, "rdb") {
			return nil, fmt.Errorf("该平台 Agent 未启用 RDB 能力（下发时请勾选「Redis RDB 分析」并安装 redis-cli）")
		}
	}

	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	if host == "" {
		return nil, fmt.Errorf("实例未配置 Host")
	}
	port := inst.Port
	if port <= 0 {
		port = 6379
	}
	pass := ""
	if strings.TrimSpace(inst.Password) != "" {
		p, derr := secret.Decrypt(s.Secret, inst.Password)
		if derr != nil {
			return nil, fmt.Errorf("解密实例密码失败: %w", derr)
		}
		pass = p
	}
	agentPass := ""
	if strings.TrimSpace(agent.PasswordEnc) != "" {
		ap, derr := secret.Decrypt(s.Secret, agent.PasswordEnc)
		if derr != nil {
			return nil, fmt.Errorf("解密 Agent 密码失败: %w", derr)
		}
		agentPass = ap
	}

	id := uuid.NewString()
	dir := s.jobDir(id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	job := &Job{
		ID:           id,
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		FileName:     "dump.rdb",
		Status:       JobPending,
		Note:         strings.TrimSpace(note),
		Source:       "agent",
		AgentID:      agent.ID,
		AgentName:    agent.Name,
		CreatedAt:    time.Now().UTC(),
		CreatedBy:    createdBy,
	}
	if err := s.saveJob(job); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}

	go s.runAgentFetch(id, backup.AgentAuth{
		BaseURL:  agent.BaseURL,
		Username: agent.Username,
		Password: agentPass,
	}, backup.RedisRDBRequest{
		Host:     host,
		Port:     port,
		Username: inst.Username,
		Password: pass,
	})
	return job, nil
}

func (s *Service) runAgentFetch(id string, auth backup.AgentAuth, req backup.RedisRDBRequest) {
	job, err := s.loadJob(id)
	if err != nil || job == nil {
		return
	}
	job.Status = JobRunning
	job.StartedAt = time.Now().UTC()
	job.Error = ""
	_ = s.saveJob(job)

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	stream, err := backup.CallRedisRDB(ctx, auth, req)
	if err != nil {
		job.Status = JobFailed
		job.Error = err.Error()
		job.FinishedAt = time.Now().UTC()
		_ = s.saveJob(job)
		return
	}
	defer stream.Body.Close()

	rdbPath := filepath.Join(s.jobDir(id), "dump.rdb")
	dst, err := os.Create(rdbPath)
	if err != nil {
		job.Status = JobFailed
		job.Error = err.Error()
		job.FinishedAt = time.Now().UTC()
		_ = s.saveJob(job)
		return
	}
	written, err := io.Copy(dst, io.LimitReader(stream.Body, MaxUploadSize+1))
	_ = dst.Close()
	if err != nil {
		job.Status = JobFailed
		job.Error = fmt.Sprintf("写入 RDB 失败: %v", err)
		job.FinishedAt = time.Now().UTC()
		_ = s.saveJob(job)
		return
	}
	if written > MaxUploadSize {
		_ = os.Remove(rdbPath)
		job.Status = JobFailed
		job.Error = fmt.Sprintf("RDB 过大，上限 %d MB", MaxUploadSize>>20)
		job.FinishedAt = time.Now().UTC()
		_ = s.saveJob(job)
		return
	}
	if written <= 0 {
		job.Status = JobFailed
		job.Error = "空 RDB"
		job.FinishedAt = time.Now().UTC()
		_ = s.saveJob(job)
		return
	}
	job.FileSize = written
	_ = s.saveJob(job)

	// 复用同一分析路径（不再改 status 为 pending）
	s.finishAnalyze(job)
}

func (s *Service) runAnalyze(id string) {
	job, err := s.loadJob(id)
	if err != nil || job == nil {
		return
	}
	job.Status = JobRunning
	job.StartedAt = time.Now().UTC()
	job.Error = ""
	_ = s.saveJob(job)
	s.finishAnalyze(job)
}

func (s *Service) finishAnalyze(job *Job) {
	if job == nil {
		return
	}
	id := job.ID
	rdbPath := filepath.Join(s.jobDir(id), "dump.rdb")
	rep, err := AnalyzeFile(rdbPath, DefaultTopN)
	if err != nil {
		job.Status = JobFailed
		job.Error = err.Error()
		job.FinishedAt = time.Now().UTC()
		_ = s.saveJob(job)
		return
	}
	rep.JobID = id
	if err := s.saveReport(id, rep); err != nil {
		job.Status = JobFailed
		job.Error = err.Error()
		job.FinishedAt = time.Now().UTC()
		_ = s.saveJob(job)
		return
	}
	job.Status = JobSucceeded
	job.Error = ""
	job.FinishedAt = time.Now().UTC()
	_ = s.saveJob(job)
}

func (s *Service) ListJobs() ([]*Job, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Job{}, nil
		}
		return nil, err
	}
	out := make([]*Job, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		job, err := s.loadJob(e.Name())
		if err != nil || job == nil {
			continue
		}
		out = append(out, job)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Service) GetJob(id string) (*Job, error) {
	return s.loadJob(id)
}

func (s *Service) GetReport(id string) (*Report, error) {
	job, err := s.loadJob(id)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("任务不存在")
	}
	if job.Status != JobSucceeded {
		return nil, fmt.Errorf("任务未成功完成（当前状态: %s）", job.Status)
	}
	return s.loadReport(id)
}

func (s *Service) DeleteJob(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "..") || strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("非法任务 ID")
	}
	return os.RemoveAll(s.jobDir(id))
}

func (s *Service) OnlineSummary(ctx context.Context, instanceID string) (*OnlineSummary, error) {
	inst, err := s.Store.GetInstance(instanceID)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("实例不存在")
	}
	if !strings.EqualFold(strings.TrimSpace(inst.DBType), "redis") {
		return nil, fmt.Errorf("非 Redis 实例")
	}
	pass := ""
	if strings.TrimSpace(inst.Password) != "" {
		p, err := secret.Decrypt(s.Secret, inst.Password)
		if err != nil {
			return nil, fmt.Errorf("解密实例密码失败: %w", err)
		}
		pass = p
	}
	opt := redisquery.Options{Username: inst.Username, Password: pass}
	mem, err := redisquery.InfoSection(ctx, inst, opt, "memory")
	if err != nil {
		return nil, fmt.Errorf("INFO memory: %w", err)
	}
	ks, err := redisquery.InfoSection(ctx, inst, opt, "keyspace")
	if err != nil {
		return nil, fmt.Errorf("INFO keyspace: %w", err)
	}
	st, _ := redisquery.InfoSection(ctx, inst, opt, "stats")
	parsed := parseOnline(mem, ks, st)
	sum := &OnlineSummary{
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		CollectedAt:  time.Now().UTC(),
		Memory:       mem,
		Keyspace:     ks,
		Stats:        st,
		Parsed:       parsed,
		Advice:       buildOnlineAdvice(parsed),
	}
	return sum, nil
}

func (s *Service) saveJob(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.jobDir(job.ID), "job.json"), b, 0o640)
}

func (s *Service) loadJob(id string) (*Job, error) {
	b, err := os.ReadFile(filepath.Join(s.jobDir(id), "job.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var job Job
	if err := json.Unmarshal(b, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *Service) saveReport(id string, rep *Report) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.jobDir(id), "report.json"), b, 0o640)
}

func (s *Service) loadReport(id string) (*Report, error) {
	b, err := os.ReadFile(filepath.Join(s.jobDir(id), "report.json"))
	if err != nil {
		return nil, err
	}
	var rep Report
	if err := json.Unmarshal(b, &rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

func parseOnline(mem, ks, st map[string]string) OnlineParsed {
	p := OnlineParsed{
		UsedMemoryHuman: mem["used_memory_human"],
		MaxMemoryPolicy: mem["maxmemory_policy"],
	}
	p.UsedMemoryBytes = atoi64(mem["used_memory"])
	p.MaxMemoryBytes = atoi64(mem["maxmemory"])
	p.MemFragmentation = atof(mem["mem_fragmentation_ratio"])
	p.EvictedKeys = atoi64(st["evicted_keys"])
	p.ExpiredKeys = atoi64(st["expired_keys"])
	for k, v := range ks {
		if !strings.HasPrefix(strings.ToLower(k), "db") {
			continue
		}
		// keys=1,expires=0,avg_ttl=0
		for _, part := range strings.Split(v, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "keys":
				p.TotalKeys += atoi64(kv[1])
			case "expires":
				p.TotalExpires += atoi64(kv[1])
			}
		}
	}
	if p.MaxMemoryBytes > 0 && p.UsedMemoryBytes > 0 {
		p.UsedMemoryPctOfMax = float64(p.UsedMemoryBytes) / float64(p.MaxMemoryBytes) * 100
	}
	return p
}

func buildOnlineAdvice(p OnlineParsed) []AdviceItem {
	var out []AdviceItem
	if p.MaxMemoryBytes == 0 {
		out = append(out, AdviceItem{
			Level: "warn", Code: "no_maxmemory", Title: "未设置 maxmemory",
			Detail:  "实例未配置内存上限，OOM 时可能被系统杀掉。",
			Suggest: "生产环境建议设置 maxmemory 与合适的 maxmemory-policy。",
		})
	} else if p.UsedMemoryPctOfMax >= 90 {
		out = append(out, AdviceItem{
			Level: "critical", Code: "memory_pressure", Title: "内存使用接近上限",
			Detail:  fmt.Sprintf("已用约 %.1f%% of maxmemory。", p.UsedMemoryPctOfMax),
			Suggest: "扩容、清理大 Key，或检查淘汰策略是否符合预期。",
		})
	}
	if p.MemFragmentation >= 1.5 {
		out = append(out, AdviceItem{
			Level: "warn", Code: "high_fragmentation", Title: "内存碎片率偏高",
			Detail:  fmt.Sprintf("mem_fragmentation_ratio=%.2f。", p.MemFragmentation),
			Suggest: "关注 activedefrag；必要时在低峰重启从节点做切换。",
		})
	}
	if p.EvictedKeys > 0 {
		out = append(out, AdviceItem{
			Level: "info", Code: "evictions", Title: "存在键淘汰",
			Detail:  fmt.Sprintf("累计 evicted_keys=%d。", p.EvictedKeys),
			Suggest: "结合 maxmemory-policy 确认是否为预期行为。",
		})
	}
	if len(out) == 0 {
		out = append(out, AdviceItem{
			Level: "info", Code: "online_ok", Title: "在线内存指标正常",
			Detail: "建议定期上传 RDB 做离线大 Key / 前缀分析以补充在线视图。",
		})
	}
	return out
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
