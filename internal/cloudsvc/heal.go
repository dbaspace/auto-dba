package cloudsvc

import (
	"context"
	"time"

	"auto-dba/internal/cloud"
	"auto-dba/internal/store"
)

func (s *Service) Heal(ctx context.Context) error {
	policies, err := s.DB.ListEnabledCloudPolicies()
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}
	alerts, err := s.DB.ListFiringAlertsForHeal()
	if err != nil {
		return err
	}
	for _, a := range alerts {
		instID := labelInstance(a.Labels)
		if instID == "" {
			continue
		}
		inst, err := s.DB.GetInstance(instID)
		if err != nil || inst == nil || inst.CloudAccountID == "" {
			continue
		}
		for _, pol := range policies {
			if pol.InstanceID != "" && pol.InstanceID != inst.ID {
				continue
			}
			if pol.CloudID != "" && pol.CloudID != inst.CloudInstanceID {
				continue
			}
			cp := cloud.Policy{
				Enabled: pol.Enabled, Severity: pol.Severity, RuleKeyword: pol.RuleKeyword, Action: pol.Action,
				CapCPU: pol.CapCPU, CapMemoryMB: pol.CapMemoryMB, DiskStepGB: pol.DiskStepGB,
				CooldownMin: pol.CooldownMin, DailyMax: pol.DailyMax, Window: pol.Window, Mode: pol.Mode,
			}
			if !cp.Hit(a.Severity, a.Title, a.RuleID) {
				continue
			}
			s.healOne(ctx, inst, pol, cp, a)
		}
	}
	return nil
}

func (s *Service) healOne(ctx context.Context, inst *store.Instance, pol store.CloudPolicy, cp cloud.Policy, a store.Alert) {
	row, _ := s.DB.GetCloudPostgresByCloudID(inst.CloudVendor, inst.CloudInstanceID)
	beforeCPU, beforeMem, beforeDisk := 0, 0, 0
	if row != nil {
		beforeCPU, beforeMem, beforeDisk = row.CPU, row.MemoryMB, row.DiskGB
	}
	targetCPU, targetMem, targetDisk := beforeCPU, beforeMem, beforeDisk
	if cp.Action == cloud.ActionScaleSpec {
		targetCPU = cp.CapCPU
		targetMem = cp.CapMemoryMB
		if targetCPU == 0 {
			targetCPU = beforeCPU
		}
		if targetMem == 0 {
			targetMem = beforeMem
		}
	}
	if cp.Action == cloud.ActionScaleDisk {
		step := cp.DiskStepGB
		if step <= 0 {
			step = 10
		}
		targetDisk = beforeDisk + step
	}
	since := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	today, _ := s.DB.CountCloudActionsSince(inst.CloudInstanceID, cp.Action, since)
	lastAt := time.Time{}
	if at, _ := s.DB.LastCloudActionAt(inst.CloudInstanceID, cp.Action); at != "" {
		lastAt, _ = time.Parse(time.RFC3339, at)
	}
	recent, _ := s.DB.RecentCloudSwitch(inst.CloudVendor, inst.CloudInstanceID, time.Now().Add(-30*time.Minute).UTC().Format(time.RFC3339))
	ok, reason := cloud.HealDecision(cp, time.Now(), lastAt, today, recent, beforeCPU, beforeMem, targetCPU, targetMem, beforeDisk, targetDisk)
	if !ok {
		_ = s.DB.AddCloudEvent(inst.TenantID, inst.CloudVendor, inst.CloudAccountID, inst.CloudInstanceID, "heal_skip", reason, time.Now().UTC().Format(time.RFC3339))
		_ = s.DB.AddAlertEvent(&store.AlertEvent{AlertID: a.ID, Action: "cloud_heal_skip", Detail: reason, Operator: "system"})
		return
	}
	p := cloud.Payload{
		Action: cp.Action, Vendor: inst.CloudVendor, Engine: cloud.EnginePostgres,
		AccountID: inst.CloudAccountID, CloudID: inst.CloudInstanceID,
		Region: inst.CloudRegion, BeforeCPU: beforeCPU, BeforeMem: beforeMem, BeforeDisk: beforeDisk,
		CPU: targetCPU, MemoryMB: targetMem, DiskGB: targetDisk,
	}
	p.Blip, p.Impact = cloud.ImpactOf(p.Action, beforeCPU, beforeMem, targetCPU, targetMem)
	issue := &store.Issue{
		IssueType: store.IssueTypeCloudPg, Type: store.IssueTypeCloudPg, InstanceID: inst.ID, TenantID: inst.TenantID, // 自愈本期仅 PG
		SQLContent: p.JSON(), Description: "告警触发自愈: " + a.Title, Status: store.StatusPending,
		Submitter: "system", AssignedTo: s.approver(inst.ID),
	}
	if err := s.DB.CreateIssue(issue); err != nil {
		return
	}
	if cp.Mode != "auto" {
		return
	}
	if err := s.AttachApproval(issue, "system", "系统按自愈策略自动通过", pol.ID, a.ID); err != nil {
		return
	}
	issue.Status = store.StatusApproved
	_ = s.DB.UpdateIssueStatus(issue.ID, store.StatusApproved, "")
	_ = s.Execute(ctx, issue)
}
