package store

import (
	"strings"
	"sync"
	"time"
)

const (
	ArgHardenViewCredential   = "client_access_harden_view_credential"
	ArgHardenStepUp           = "client_access_harden_step_up"
	ArgHardenTicketExecute    = "client_access_harden_ticket_execute"
	ArgHardenK8sTunnel        = "client_access_harden_k8s_tunnel"
	ArgHardenReadOnlySession  = "client_access_harden_readonly_session"
	ArgAllowCredentialScope   = "client_access_allow_credential_scope"
	ArgAuditRetentionDays     = "client_access_audit_retention_days"
	ArgK8sAgentTunnelToken    = "k8s_agent_tunnel_token"
	ArgSecurityAlertThreshold = "client_access_security_alert_threshold"

	ClientScopeCredential = "credential"
	URNAccountsViewCred   = "um.action.accounts.view_credential"
	URNTicketsExecute     = "um.action.tickets.execute"

	defaultAuditRetentionDays     = 90
	defaultSecurityAlertThreshold = 20
)

func (d *DB) boolArgDefaultFalse(key string) bool {
	v, _ := d.GetGlobalArg(key)
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (d *DB) HardenViewCredential() bool   { return d.boolArgDefaultFalse(ArgHardenViewCredential) }
func (d *DB) HardenStepUp() bool           { return d.boolArgDefaultFalse(ArgHardenStepUp) }
func (d *DB) HardenTicketExecute() bool    { return d.boolArgDefaultFalse(ArgHardenTicketExecute) }
func (d *DB) HardenK8sTunnel() bool        { return d.boolArgDefaultFalse(ArgHardenK8sTunnel) }
func (d *DB) HardenReadOnlySession() bool  { return d.boolArgDefaultFalse(ArgHardenReadOnlySession) }
func (d *DB) AllowCredentialScope() bool   { return d.boolArgDefaultFalse(ArgAllowCredentialScope) }

func (d *DB) AuditRetentionDays() int {
	n := d.intArg(ArgAuditRetentionDays, defaultAuditRetentionDays)
	if n < 0 {
		return 0
	}
	if n == 0 {
		return defaultAuditRetentionDays
	}
	return n
}

func (d *DB) SecurityAlertThreshold() int {
	n := d.intArg(ArgSecurityAlertThreshold, defaultSecurityAlertThreshold)
	if n <= 0 {
		return defaultSecurityAlertThreshold
	}
	return n
}

func (d *DB) K8sAgentTunnelToken() string {
	v, _ := d.GetGlobalArg(ArgK8sAgentTunnelToken)
	return strings.TrimSpace(v)
}

// --- step-up（进程内短时凭证，默认 10 分钟）---

var (
	stepUpMu   sync.Mutex
	stepUpUntil = map[string]time.Time{}
)

const stepUpTTL = 10 * time.Minute

func MarkStepUp(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	stepUpMu.Lock()
	stepUpUntil[userID] = time.Now().Add(stepUpTTL)
	stepUpMu.Unlock()
}

func HasValidStepUp(userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	stepUpMu.Lock()
	defer stepUpMu.Unlock()
	until, ok := stepUpUntil[userID]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(stepUpUntil, userID)
		return false
	}
	return true
}

// --- 安全计数（用于短时告警）---

type secCounter struct {
	n    int
	until time.Time
}

var (
	secMu       sync.Mutex
	secCounters = map[string]*secCounter{}
)

// BumpSecurityDenial 窗口内累计拒绝次数；返回是否达到阈值（达到后清零窗口，避免刷屏）。
func (d *DB) BumpSecurityDenial(key string) bool {
	th := d.SecurityAlertThreshold()
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	now := time.Now()
	secMu.Lock()
	defer secMu.Unlock()
	c := secCounters[key]
	if c == nil || now.After(c.until) {
		c = &secCounter{n: 0, until: now.Add(10 * time.Minute)}
		secCounters[key] = c
	}
	c.n++
	if c.n >= th {
		c.n = 0
		c.until = now.Add(10 * time.Minute)
		return true
	}
	return false
}
