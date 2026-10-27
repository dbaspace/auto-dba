package cloudsvc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/cloud"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

func (s *Service) Submit(ctx context.Context, p cloud.Payload, submitter, tenantID, instanceID string) (*store.Issue, error) {
	if strings.TrimSpace(p.Vendor) == "" {
		p.Vendor = cloud.VendorTencent
	}
	if strings.TrimSpace(p.Engine) == "" {
		p.Engine = cloud.EnginePostgres
	}
	if _, err := cloud.GetWriter(p.Vendor, p.Engine); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.AccountID) == "" {
		return nil, cloud.ErrNoAccount
	}
	user, err := s.DB.GetUserByUsername(submitter)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, cloud.ErrNotBound
	}
	ok, err := s.DB.UserBoundToCloudAccount(p.AccountID, user.ID)
	if err != nil || !ok {
		return nil, cloud.ErrNotBound
	}
	reader, err := cloud.GetReader(p.Vendor, p.Engine)
	if err != nil {
		return nil, err
	}
	if err := cloud.CheckDisk(p.Action, p.BeforeDisk, p.DiskGB, reader.Caps().ScaleDiskDown); err != nil {
		return nil, err
	}
	if p.Action == cloud.ActionCreate || p.Action == cloud.ActionScaleSpec || p.Action == cloud.ActionScaleDisk {
		if q, qerr := s.Quote(ctx, p); qerr == nil {
			p.Quote = q
		}
	}
	if p.Action == cloud.ActionCreate && !reader.Caps().Create {
		return nil, fmt.Errorf("%w：资源申请", cloud.ErrUnsupported)
	}
	if (p.Action == cloud.ActionRegister || p.Action == cloud.ActionRebind) && strings.TrimSpace(p.CloudID) == "" {
		return nil, fmt.Errorf("缺少云实例")
	}
	if p.Action == cloud.ActionRestart && !reader.Caps().Restart {
		return nil, fmt.Errorf("%w：重启", cloud.ErrUnsupported)
	}
	if p.Action == cloud.ActionSwitch && !reader.Caps().Switchover {
		return nil, fmt.Errorf("%w：主备切换", cloud.ErrUnsupported)
	}
	n, err := s.DB.CountOpenCloudChanges(p.CloudID, "")
	if err != nil {
		return nil, err
	}
	if p.CloudID != "" && n > 0 {
		return nil, fmt.Errorf("该云实例已有未完成变更")
	}
	p.Blip, p.Impact = cloud.ImpactOf(p.Action, p.BeforeCPU, p.BeforeMem, p.CPU, p.MemoryMB)
	if p.PasswordEnc != "" && !secret.IsEncrypted(p.PasswordEnc) {
		enc, err := secret.Encrypt(s.Secret, p.PasswordEnc)
		if err != nil {
			return nil, err
		}
		p.PasswordEnc = enc
	}
	approver := s.approver(instanceID)
	issueType := store.CloudIssueTypeForEngine(p.Engine)
	issue := &store.Issue{
		IssueType: issueType, Type: issueType,
		InstanceID: instanceID, TenantID: tenantID, SQLContent: p.JSON(),
		Description: p.Impact, Status: store.StatusPending, Submitter: submitter, AssignedTo: approver,
	}
	if err := s.DB.CreateIssue(issue); err != nil {
		return nil, err
	}
	return issue, nil
}

func (s *Service) approver(instanceID string) string {
	if it, err := s.DB.GetInstance(instanceID); err == nil && it != nil && len(it.Approvers) > 0 {
		return it.Approvers[0]
	}
	v, _ := s.DB.GetGlobalArg(store.ArgCloudPgApprovers)
	v = strings.TrimSpace(v)
	if v == "" {
		return "admin"
	}
	if i := strings.IndexAny(v, ", "); i > 0 {
		return strings.TrimSpace(v[:i])
	}
	return v
}

func (s *Service) Quote(ctx context.Context, p cloud.Payload) (cloud.Quote, error) {
	acc, err := s.DB.GetCloudAccount(p.AccountID)
	if err != nil || acc == nil {
		return cloud.Quote{}, cloud.ErrNoAccount
	}
	eng := p.Engine
	if eng == "" {
		eng = cloud.EnginePostgres
	}
	reader, err := cloud.GetReader(acc.Vendor, eng)
	if err != nil {
		return cloud.Quote{}, err
	}
	cred, err := s.Cred(acc, cloud.RoleReadonly, p.Region)
	if err != nil {
		return cloud.Quote{}, err
	}
	var q cloud.Quote
	err = cloud.RetryRead(ctx, nil, func() error {
		var e error
		if p.Action == cloud.ActionCreate {
			q, e = reader.QuoteCreate(ctx, cred, cloud.CreateInput{Region: p.Region, Zone: p.Zone, CPU: p.CPU, MemoryMB: p.MemoryMB, DiskGB: p.DiskGB, Name: p.Name})
		} else {
			q, e = reader.QuoteUpgrade(ctx, cred, cloud.SpecInput{CloudID: p.CloudID, CPU: p.CPU, MemoryMB: p.MemoryMB, DiskGB: p.DiskGB, InstanceType: p.InstanceType})
		}
		return e
	})
	return q, err
}

// Execute 只在已通过且审核记录完整时调用变更密钥。已有任务 ID 时只查询进度。
func (s *Service) Execute(ctx context.Context, it *store.Issue) error {
	if it == nil || !store.IsCloudIssueType(it.IssueType) {
		return fmt.Errorf("不是云变更工单")
	}
	if it.Status != store.StatusApproved && it.Status != store.StatusRunning {
		return cloud.ErrNoApproval
	}
	p, err := cloud.ParsePayload(it.SQLContent)
	if err != nil {
		return s.fail(it, err)
	}
	if p.Engine == "" {
		p.Engine = store.EngineForCloudIssueType(it.IssueType)
	}
	if !p.Approval.Valid() {
		return s.fail(it, cloud.ErrNoApproval)
	}
	reader, err := cloud.GetReader(p.Vendor, p.Engine)
	if err != nil {
		return s.fail(it, err)
	}
	if err := cloud.CheckDisk(p.Action, p.BeforeDisk, p.DiskGB, reader.Caps().ScaleDiskDown); err != nil {
		return s.fail(it, err)
	}
	if p.TaskID == "" {
		n, err := s.DB.CountOpenCloudChanges(p.CloudID, it.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return s.fail(it, fmt.Errorf("该云实例已有未完成变更"))
		}
	}
	acc, err := s.DB.GetCloudAccount(p.AccountID)
	if err != nil || acc == nil {
		return s.fail(it, cloud.ErrNoAccount)
	}
	if p.TaskID != "" {
		return s.poll(ctx, it, p, acc, reader)
	}
	if p.Action == cloud.ActionRegister || p.Action == cloud.ActionRebind {
		if err := s.applyPlatformBind(p, acc); err != nil {
			return s.fail(it, err)
		}
		return s.DB.UpdateIssueExec(it.ID, store.StatusExecuted, ticketexec.ToJSON(&ticketexec.Result{Success: true, Message: "已写入平台资源"}))
	}
	writer, err := cloud.GetWriter(p.Vendor, p.Engine)
	if err != nil {
		return s.fail(it, err)
	}
	op, err := s.Cred(acc, cloud.RoleOperate, p.Region)
	if err != nil {
		return s.fail(it, err)
	}
	task, err := s.submit(ctx, writer, op, p)
	if err != nil {
		if cloud.IsRateLimit(err) {
			return s.fail(it, fmt.Errorf("云厂商限流: %w", err))
		}
		return s.fail(it, err)
	}
	if task.Detail != "" && p.CloudID == "" {
		p.CloudID = task.Detail
	}
	p.TaskID = task.ID
	it.SQLContent = p.JSON()
	_ = s.DB.UpdateIssueSQL(it.ID, it.SQLContent)
	_ = s.DB.UpdateIssueStatus(it.ID, store.StatusRunning, "")
	it.Status = store.StatusRunning
	return s.poll(ctx, it, p, acc, reader)
}

func (s *Service) submit(ctx context.Context, w cloud.Writer, cred cloud.Cred, p cloud.Payload) (cloud.Task, error) {
	switch p.Action {
	case cloud.ActionCreate:
		pass, _ := secret.Decrypt(s.Secret, p.PasswordEnc)
		return w.Create(ctx, cred, cloud.CreateInput{Region: p.Region, Zone: p.Zone, CPU: p.CPU, MemoryMB: p.MemoryMB, DiskGB: p.DiskGB, Name: p.Name, Admin: p.AdminUser, Password: pass})
	case cloud.ActionScaleSpec, cloud.ActionScaleDisk:
		return w.ModifySpec(ctx, cred, cloud.SpecInput{CloudID: p.CloudID, CPU: p.CPU, MemoryMB: p.MemoryMB, DiskGB: p.DiskGB, InstanceType: p.InstanceType})
	case cloud.ActionRestart:
		return w.Restart(ctx, cred, p.CloudID)
	case cloud.ActionSwitch:
		if !w.Caps().Switchover {
			return cloud.Task{}, fmt.Errorf("%w：主备切换", cloud.ErrUnsupported)
		}
		return w.Switchover(ctx, cred, p.CloudID)
	case cloud.ActionParam:
		return w.ModifyParameters(ctx, cred, p.CloudID, p.Params)
	default:
		return cloud.Task{}, fmt.Errorf("%w：%s", cloud.ErrUnsupported, p.Action)
	}
}

func (s *Service) poll(ctx context.Context, it *store.Issue, p cloud.Payload, acc *store.CloudAccount, reader cloud.Reader) error {
	cred, err := s.Cred(acc, cloud.RoleReadonly, p.Region)
	if err != nil {
		return s.fail(it, err)
	}
	deadline := time.Now().Add(20 * time.Minute)
	for {
		if ctx.Err() != nil {
			return nil
		}
		var task cloud.Task
		err := cloud.RetryRead(ctx, nil, func() error {
			var e error
			task, e = reader.DescribeTask(ctx, cred, p.CloudID, p.TaskID)
			return e
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if cloud.IsRateLimit(err) {
				return s.fail(it, fmt.Errorf("云厂商限流: %w", err))
			}
			return s.fail(it, err)
		}
		if task.Status == "success" {
			eng := p.Engine
			if eng == "" {
				eng = cloud.EnginePostgres
			}
			_ = s.SyncAccountEngine(context.Background(), acc, eng)
			return s.DB.UpdateIssueExec(it.ID, store.StatusExecuted, ticketexec.ToJSON(&ticketexec.Result{Success: true, Message: "云上任务完成 " + p.TaskID + "；登记平台实例请另提工单"}))
		}
		if task.Status == "failed" {
			return s.fail(it, fmt.Errorf("云上任务失败: %s", task.Detail))
		}
		if time.Now().After(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

// applyPlatformBind 仅在工单已审批后调用：登记平台实例或改绑云账号。
func (s *Service) applyPlatformBind(p cloud.Payload, acc *store.CloudAccount) error {
	eng := p.Engine
	if eng == "" {
		eng = cloud.EnginePostgres
	}
	row, err := s.DB.GetCloudResourceByCloudID(acc.Vendor, eng, p.CloudID)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("云资源不存在，请先同步清单")
	}
	if p.Action == cloud.ActionRebind {
		row.AccountID = acc.ID
		if row.InstanceID == "" {
			matches, _ := s.DB.InstancesByCloudID(row.TenantID, row.Vendor, row.CloudID)
			if len(matches) == 1 {
				row.InstanceID = matches[0].ID
			}
		}
		if row.InstanceID == "" {
			return fmt.Errorf("尚未登记平台实例")
		}
		if err := s.DB.SetInstanceCloudAccount(row.InstanceID, acc.ID); err != nil {
			return err
		}
		row.Reconcile = cloud.ReconcileLinked
		return s.DB.UpdateCloudResourceOwnership(row.ID, row.AccountID, row.InstanceID, row.Reconcile)
	}
	if row.InstanceID != "" {
		if err := s.DB.SetInstanceCloudAccount(row.InstanceID, acc.ID); err != nil {
			return err
		}
		row.Reconcile = cloud.ReconcileLinked
		return s.DB.UpdateCloudResourceOwnership(row.ID, acc.ID, row.InstanceID, row.Reconcile)
	}
	host, port := netFromExtras(row)
	dbType := eng
	if eng == cloud.EngineHost {
		dbType = "host"
	}
	if port <= 0 {
		switch eng {
		case cloud.EngineMySQL:
			port = 3306
		case cloud.EngineRedis:
			port = 6379
		case cloud.EngineHost:
			port = 22
		default:
			port = 5432
		}
	}
	name := row.Name
	if name == "" {
		name = p.CloudID
	}
	region := row.Region
	if region == "" {
		region = p.Region
	}
	inst := &store.Instance{
		TenantID: acc.TenantID, Name: name, DBType: dbType, Host: host, Port: port,
		CloudVendor: acc.Vendor, CloudInstanceID: p.CloudID, CloudRegion: region, CloudAccountID: acc.ID,
	}
	if err := s.DB.CreateInstance(inst); err != nil {
		return err
	}
	row.InstanceID = inst.ID
	row.AccountID = acc.ID
	row.Reconcile = cloud.ReconcileLinked
	return s.DB.UpdateCloudResourceOwnership(row.ID, row.AccountID, row.InstanceID, row.Reconcile)
}

func netFromExtras(row *store.CloudInstance) (string, int) {
	if row == nil || row.Extras == nil {
		return "", 0
	}
	nets, _ := row.Extras["DBInstanceNetInfo"].([]any)
	if len(nets) == 0 {
		return "", 0
	}
	m, _ := nets[0].(map[string]any)
	if m == nil {
		return "", 0
	}
	host, _ := m["Ip"].(string)
	if strings.TrimSpace(host) == "" {
		host, _ = m["Address"].(string)
	}
	port := 0
	switch n := m["Port"].(type) {
	case float64:
		port = int(n)
	case string:
		fmt.Sscan(n, &port)
	}
	return strings.TrimSpace(host), port
}

func (s *Service) fail(it *store.Issue, err error) error {
	msg := err.Error()
	_ = s.DB.UpdateIssueExec(it.ID, store.StatusFailed, ticketexec.ToJSON(&ticketexec.Result{Success: false, Message: msg}))
	return nil
}

func (s *Service) Resume(ctx context.Context) error {
	list, err := s.DB.ListRunningCloudIssues()
	if err != nil {
		return err
	}
	for _, it := range list {
		if err := s.Execute(ctx, it); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) AttachApproval(it *store.Issue, by, opinion, policyID, alertID string) error {
	p, err := cloud.ParsePayload(it.SQLContent)
	if err != nil {
		return err
	}
	if opinion == "" {
		opinion = "审批通过"
	}
	p.Approval = cloud.Approval{By: by, At: time.Now().UTC().Format(time.RFC3339), Opinion: opinion, PolicyID: policyID, AlertID: alertID}
	it.SQLContent = p.JSON()
	return s.DB.UpdateIssueSQL(it.ID, it.SQLContent)
}
