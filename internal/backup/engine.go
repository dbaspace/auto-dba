package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/notify"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// Config 备份引擎运行参数。
type Config struct {
	WorkDir         string
	AllowEmbedded   bool
	DefaultTimeout  time.Duration
	AgentArtifact   string // 本地 linux 制品路径
	AgentRemoteDir  string
	AgentListenPort int
	SSHUser            string
	SSHIdentityFile    string
	SSHInsecureHostKey bool
}

// Engine 备份控制面。
type Engine struct {
	Store  *store.DB
	Secret string
	Cfg    Config
	mu     sync.Mutex
	busy   map[string]bool // instance_id
}

func New(st *store.DB, secretKey string, cfg Config) *Engine {
	if strings.TrimSpace(cfg.WorkDir) == "" {
		cfg.WorkDir = filepath.Join(os.TempDir(), "auto-dba-backup")
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 6 * time.Hour
	}
	return &Engine{Store: st, Secret: secretKey, Cfg: cfg, busy: map[string]bool{}}
}

// Start 手动或调度触发一次策略执行（异步）。
func (e *Engine) Start(policyID, trigger, operator string) (string, error) {
	p, err := e.Store.GetBackupPolicy(policyID)
	if err != nil || p == nil {
		return "", fmt.Errorf("策略不存在")
	}
	if !p.Enabled && trigger == store.BackupTriggerCron {
		return "", fmt.Errorf("策略未启用")
	}
	busy, err := e.Store.HasRunningBackupForInstance(p.InstanceID)
	if err != nil {
		return "", err
	}
	if busy {
		return "", fmt.Errorf("该实例已有备份在执行")
	}
	e.mu.Lock()
	if e.busy[p.InstanceID] {
		e.mu.Unlock()
		return "", fmt.Errorf("该实例已有备份在执行")
	}
	e.busy[p.InstanceID] = true
	e.mu.Unlock()

	run := &store.BackupRun{
		PolicyID:   p.ID,
		InstanceID: p.InstanceID,
		Engine:     p.Engine,
		Method:     p.Method,
		Status:     store.BackupRunPending,
		Trigger:    trigger,
		CreatedBy:  operator,
	}
	if run.Method == "" {
		run.Method = store.BackupMethodLogicalFull
	}
	if err := e.Store.CreateBackupRun(run); err != nil {
		e.mu.Lock()
		delete(e.busy, p.InstanceID)
		e.mu.Unlock()
		return "", err
	}
	go e.execute(run.ID, p.ID)
	return run.ID, nil
}

func (e *Engine) execute(runID, policyID string) {
	p, _ := e.Store.GetBackupPolicy(policyID)
	run, _ := e.Store.GetBackupRun(runID)
	if p == nil || run == nil {
		return
	}
	defer func() {
		e.mu.Lock()
		delete(e.busy, p.InstanceID)
		e.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), e.Cfg.DefaultTimeout)
	defer cancel()

	now := time.Now().UTC().Format(time.RFC3339)
	run.Status = store.BackupRunRunning
	run.StartedAt = now
	_ = e.Store.UpdateBackupRun(run)

	result, err := e.dispatch(ctx, p, run)
	finished := time.Now().UTC().Format(time.RFC3339)
	run.FinishedAt = finished
	if err != nil {
		run.Status = store.BackupRunFailed
		run.ErrorMessage = err.Error()
		if result != nil {
			run.LogSummary = result.Log
		}
		_ = e.Store.UpdateBackupRun(run)
		go notify.BackupFailed(e.Store, e.Secret, p, run)
		return
	}
	run.Status = store.BackupRunSuccess
	run.SizeBytes = result.SizeBytes
	run.Checksum = result.Checksum
	run.ObjectKey = result.ObjectKey
	run.LocalPath = result.LocalPath
	run.LogSummary = result.Log
	_ = e.Store.UpdateBackupRun(run)
}

func (e *Engine) dispatch(ctx context.Context, p *store.BackupPolicy, run *store.BackupRun) (*dumpResult, error) {
	method := strings.TrimSpace(p.Method)
	if method == "" {
		method = store.BackupMethodLogicalFull
	}
	run.Method = method
	switch method {
	case store.BackupMethodLogicalFull:
		return e.runLogical(ctx, p, run)
	case store.BackupMethodPhysicalFull, store.BackupMethodPhysicalIncr:
		return e.runPhysical(ctx, p, run)
	default:
		return nil, fmt.Errorf("不支持的备份方式: %s", method)
	}
}

type dumpResult struct {
	LocalPath  string
	ObjectKey  string
	SizeBytes  int64
	Checksum   string
	Log        string
}

func (e *Engine) runLogical(ctx context.Context, p *store.BackupPolicy, run *store.BackupRun) (*dumpResult, error) {
	inst, err := e.Store.GetInstance(p.InstanceID)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("实例不存在")
	}
	engine := store.NormalizeDBType(inst.DBType)
	if engine != "postgres" && engine != "mysql" && engine != "tidb" && engine != "oceanbase" {
		return nil, fmt.Errorf("仅支持 PostgreSQL / MySQL 逻辑备份，当前类型 %s", inst.DBType)
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
	dbs := p.Databases
	if len(dbs) == 0 {
		if engine == "postgres" {
			dbs = []string{"postgres"}
		} else {
			return nil, fmt.Errorf("请至少指定一个数据库")
		}
	}

	if strings.TrimSpace(p.AgentID) != "" {
		return e.runViaAgent(ctx, p, inst, storage, host, pass, dbs)
	}
	if !e.Cfg.AllowEmbedded {
		return nil, fmt.Errorf("未绑定 Backup Agent，且平台禁止内嵌执行（请配置 agent 或 backup.allow_embedded）")
	}
	return e.runEmbedded(ctx, p, inst, storage, host, pass, dbs, run.ID)
}

func (e *Engine) runEmbedded(ctx context.Context, p *store.BackupPolicy, inst *store.Instance, storage *store.BackupStorage, host, pass string, dbs []string, runID string) (*dumpResult, error) {
	workdir := e.Cfg.WorkDir
	if storage.Kind == store.BackupStorageLocal && strings.TrimSpace(storage.LocalPath) != "" {
		workdir = storage.LocalPath
	}
	if err := os.MkdirAll(workdir, 0o750); err != nil {
		return nil, err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	baseName := fmt.Sprintf("%s_%s_%s", sanitizeName(inst.Name), sanitizeName(dbs[0]), stamp)
	engine := store.NormalizeDBType(inst.DBType)
	var outPath string
	var logBuf strings.Builder
	var err error

	switch engine {
	case "postgres":
		outPath = filepath.Join(workdir, baseName+".dump")
		err = dumpPostgres(ctx, host, inst.Port, inst.Username, pass, dbs[0], p.Parallel, outPath, &logBuf)
	default:
		outPath = filepath.Join(workdir, baseName+".sql")
		err = dumpMySQL(ctx, host, inst.Port, inst.Username, pass, dbs, p.Parallel, outPath, &logBuf)
	}
	if err != nil {
		return &dumpResult{Log: logBuf.String()}, err
	}

	fi, err := os.Stat(outPath)
	if err != nil {
		return &dumpResult{Log: logBuf.String()}, err
	}
	if fi.Size() <= 0 {
		return &dumpResult{Log: logBuf.String()}, fmt.Errorf("备份文件大小为 0，校验失败")
	}
	sum, err := fileSHA256(outPath)
	if err != nil {
		return &dumpResult{Log: logBuf.String()}, err
	}
	logBuf.WriteString(fmt.Sprintf("\nchecksum=sha256:%s size=%d\n", sum, fi.Size()))

	result := &dumpResult{
		LocalPath: outPath,
		SizeBytes: fi.Size(),
		Checksum:  "sha256:" + sum,
		Log:       trimLog(logBuf.String()),
	}
	if storage.Kind == store.BackupStorageS3 {
		key := objectKey(storage.Prefix, filepath.Base(outPath))
		if err := uploadS3(ctx, storage, e.Secret, outPath, key); err != nil {
			return result, fmt.Errorf("上传对象存储失败: %w", err)
		}
		result.ObjectKey = key
		logBuf.WriteString("uploaded to s3: " + key + "\n")
		result.Log = trimLog(logBuf.String())
	} else {
		result.ObjectKey = outPath
	}
	_ = runID
	return result, nil
}

func (e *Engine) runViaAgent(ctx context.Context, p *store.BackupPolicy, inst *store.Instance, storage *store.BackupStorage, host, pass string, dbs []string) (*dumpResult, error) {
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
	req := LogicalBackupRequest{
		Engine:   store.NormalizeDBType(inst.DBType),
		Host:     host,
		Port:     inst.Port,
		Username: inst.Username,
		Password: pass,
		Databases: dbs,
		Parallel: p.Parallel,
		Format:   "custom",
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
		sk, derr := secret.Decrypt(e.Secret, storage.SecretKeyEnc)
		if derr == nil {
			req.Dest.SecretKey = sk
		}
	}
	resp, err := CallLogicalBackup(ctx, auth, req)
	if err != nil {
		return nil, err
	}
	if resp.SizeBytes <= 0 {
		return nil, fmt.Errorf("Agent 返回大小为 0，校验失败")
	}
	return &dumpResult{
		LocalPath: resp.LocalPath,
		ObjectKey: resp.ObjectKey,
		SizeBytes: resp.SizeBytes,
		Checksum:  resp.Checksum,
		Log:       trimLog(resp.Message),
	}, nil
}

func dumpPostgres(ctx context.Context, host string, port int, user, pass, dbname string, parallel int, outPath string, log *strings.Builder) error {
	bin, err := exec.LookPath("pg_dump")
	if err != nil {
		return fmt.Errorf("未找到 pg_dump，请安装 PostgreSQL 客户端或使用 Backup Agent")
	}
	args := []string{
		"-h", host,
		"-p", fmt.Sprintf("%d", port),
		"-U", user,
		"-Fc",
		"-f", outPath,
		dbname,
	}
	if parallel > 1 {
		// 目录格式才支持 -j；P0 用单文件 -Fc
		_ = parallel
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
	out, err := cmd.CombinedOutput()
	log.Write(out)
	if err != nil {
		return fmt.Errorf("pg_dump 失败: %w; %s", err, trimLog(string(out)))
	}
	return nil
}

func dumpMySQL(ctx context.Context, host string, port int, user, pass string, dbs []string, parallel int, outPath string, log *strings.Builder) error {
	if bin, err := exec.LookPath("mydumper"); err == nil {
		dir := strings.TrimSuffix(outPath, filepath.Ext(outPath)) + "_mydumper"
		_ = os.RemoveAll(dir)
		args := []string{
			"-h", host,
			"-P", fmt.Sprintf("%d", port),
			"-u", user,
			"-p", pass,
			"-o", dir,
			"--threads", fmt.Sprintf("%d", max(1, parallel)),
			"--compress",
		}
		for _, db := range dbs {
			args = append(args, "-B", db)
		}
		cmd := exec.CommandContext(ctx, bin, args...)
		out, err := cmd.CombinedOutput()
		log.Write(out)
		if err != nil {
			return fmt.Errorf("mydumper 失败: %w; %s", err, trimLog(string(out)))
		}
		// 打包目录为 tar 以便统一产物
		if err := tarDir(dir, outPath+".tar"); err != nil {
			return err
		}
		_ = os.RemoveAll(dir)
		// 调整路径
		if err := os.Rename(outPath+".tar", outPath); err != nil {
			return err
		}
		return nil
	}
	bin, err := exec.LookPath("mysqldump")
	if err != nil {
		return fmt.Errorf("未找到 mydumper/mysqldump，请安装客户端或使用 Backup Agent")
	}
	args := []string{
		"-h", host,
		"-P", fmt.Sprintf("%d", port),
		"-u", user,
		"--single-transaction",
		"--routines",
		"--triggers",
		"--set-gtid-purged=OFF",
	}
	args = append(args, dbs...)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+pass)
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd.Stdout = f
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	err = cmd.Run()
	log.WriteString(errBuf.String())
	if err != nil {
		return fmt.Errorf("mysqldump 失败: %w; %s", err, trimLog(errBuf.String()))
	}
	return nil
}

func tarDir(srcDir, dest string) error {
	bin, err := exec.LookPath("tar")
	if err != nil {
		return fmt.Errorf("打包 mydumper 输出需要 tar: %w", err)
	}
	cmd := exec.Command(bin, "-cf", dest, "-C", filepath.Dir(srcDir), filepath.Base(srcDir))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar 失败: %w; %s", err, string(out))
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "db"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func objectKey(prefix, name string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func trimLog(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		return s[:4000] + "…"
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// StartScheduler 定时扫描启用策略（60s）。jobreg 接管后勿再调用。
func (e *Engine) StartScheduler(ctx context.Context) {
	tick := time.NewTicker(60 * time.Second)
	go func() {
		e.ScanDue()
		for {
			select {
			case <-ctx.Done():
				tick.Stop()
				return
			case <-tick.C:
				e.ScanDue()
			}
		}
	}()
}

// ScanDue 扫描到期备份策略（供 jobreg 调用）。
func (e *Engine) ScanDue() {
	e.scanDue()
}

func (e *Engine) scanDue() {
	list, err := e.Store.ListEnabledBackupPolicies()
	if err != nil {
		return
	}
	now := time.Now()
	for _, p := range list {
		expr := strings.TrimSpace(p.ScheduleExpr)
		if expr == "" {
			continue
		}
		if !scheduleDue(expr, e.Store.LastBackupRunAt(p.ID), now) {
			continue
		}
		if _, err := e.Start(p.ID, store.BackupTriggerCron, "scheduler"); err != nil {
			// 并发或未启用等忽略
			_ = err
		}
	}
}
