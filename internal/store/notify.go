package store

import (
	"encoding/json"
	"strings"
)

const KeyNotifyConfig = "notify_config"

const DefaultAwaitExecStuckHours = 2

// NotifyConfig 工单 / 告警等事件通知渠道配置。
// 邮件 SMTP 与 email_on_* 保持原逻辑；飞书/短信/电话等能力凭证在 Nacos notify 段。
type NotifyConfig struct {
	EmailOnTicketCreate     bool   `json:"email_on_ticket_create"`
	EmailOnTicketExecFailed bool   `json:"email_on_ticket_exec_failed"`
	EmailOnTicketAwaitExec  bool   `json:"email_on_ticket_await_exec"`
	EmailOnTicketAwaitStuck bool   `json:"email_on_ticket_await_exec_stuck"`
	EmailOnBackupFailed     bool   `json:"email_on_backup_failed"`
	EmailOnJobFailed        bool   `json:"email_on_job_failed"`
	EmailOnAlert            bool   `json:"email_on_alert"`
	AwaitExecStuckHours     int    `json:"await_exec_stuck_hours"`
	WebhookEnabled          bool   `json:"webhook_enabled"`
	WebhookURL              string `json:"webhook_url"`
	WebhookOnAlert          bool   `json:"webhook_on_alert"`
	WecomEnabled            bool   `json:"wecom_enabled"`
	WecomCorpID             string `json:"wecom_corp_id"`
	WecomAgentID            string `json:"wecom_agent_id"`
	WecomSecret             string `json:"wecom_secret"`
	WecomOnAlert            bool   `json:"wecom_on_alert"`
	WecomToUser             string `json:"wecom_to_user,omitempty"` // 已废弃，不再使用 @all 默认
	WecomOnTicketCreate     bool   `json:"wecom_on_ticket_create"`
	PhoneEnabled            bool   `json:"phone_enabled"`
	PhoneAPIURL             string `json:"phone_api_url"`
	PhoneOnAlert            bool   `json:"phone_on_alert"`
	// Nacos 渠道事件开关（凭证不在此）
	LarkOnTicketCreate bool `json:"lark_on_ticket_create"`
	LarkOnAlert        bool `json:"lark_on_alert"`
	LarkAlertOnAlert   bool `json:"lark_alert_on_alert"`
	SMSOnAlert         bool `json:"sms_on_alert"`
	DingtalkOnAlert    bool `json:"dingtalk_on_alert"`
	RobotLarkOnAlert   bool `json:"robot_lark_on_alert"`
	// RobotsOnTicketCreate 工单待审时是否向「方向含 ticket」的群机器人发送。
	RobotsOnTicketCreate bool `json:"robots_on_ticket_create"`
	// RobotRoutes 每条 Nacos robots 下标的方向（页面配置，不进 Nacos）。
	RobotRoutes []RobotRoute `json:"robot_routes,omitempty"`
	// 告警 trigger/repeat 全局默认频控（resolve 不受限，最终恢复必发一条）
	AlertNotifyMaxFire int    `json:"alert_notify_max_fire"` // 默认 3；0=不限制
	AlertNotifyWindow  string `json:"alert_notify_window"`   // 默认 15m
	AlertNotifyRepeat  string `json:"alert_notify_repeat"`   // 默认 5m；空=关闭续报
}

// RobotRoute 群机器人用途方向（index 对应 Nacos notify.robots 下标）。
type RobotRoute struct {
	Index int    `json:"index"`
	For   string `json:"for"` // alert | ticket | both
}

// NormalizeRobotFor 规范化方向；空/未知视为 alert。
func NormalizeRobotFor(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ticket":
		return "ticket"
	case "both":
		return "both"
	default:
		return "alert"
	}
}

// RobotRouteFor 返回指定下标的方向；未配置默认 alert。
func (c *NotifyConfig) RobotRouteFor(index int) string {
	if c == nil {
		return "alert"
	}
	for _, r := range c.RobotRoutes {
		if r.Index == index {
			return NormalizeRobotFor(r.For)
		}
	}
	return "alert"
}

// SanitizeRobotRoutes 去重下标、规范 for。
func SanitizeRobotRoutes(in []RobotRoute) []RobotRoute {
	if len(in) == 0 {
		return nil
	}
	seen := map[int]bool{}
	out := make([]RobotRoute, 0, len(in))
	for _, r := range in {
		if r.Index < 0 || seen[r.Index] {
			continue
		}
		seen[r.Index] = true
		out = append(out, RobotRoute{Index: r.Index, For: NormalizeRobotFor(r.For)})
	}
	return out
}

func DefaultNotifyConfig() *NotifyConfig {
	c := &NotifyConfig{
		AwaitExecStuckHours: DefaultAwaitExecStuckHours,
		WebhookOnAlert:      true,
		AlertNotifyMaxFire:  DefaultAlertNotifyMaxFire,
		AlertNotifyWindow:   DefaultAlertNotifyWindow,
		AlertNotifyRepeat:   DefaultAlertNotifyRepeat,
	}
	ApplyAlertNotifyDefaultsOnLoad(c)
	return c
}

// ApplyAlertNotifyDefaultsOnLoad 加载后补齐默认并同步全局策略缓存。
func ApplyAlertNotifyDefaultsOnLoad(c *NotifyConfig) {
	if c == nil {
		SyncGlobalAlertNotifyPolicy(DefaultAlertNotifyMaxFire, DefaultAlertNotifyWindow, DefaultAlertNotifyRepeat)
		return
	}
	if c.AlertNotifyMaxFire < 0 {
		c.AlertNotifyMaxFire = 0
	}
	windowWasEmpty := strings.TrimSpace(c.AlertNotifyWindow) == ""
	if windowWasEmpty {
		c.AlertNotifyWindow = DefaultAlertNotifyWindow
		// 旧配置无字段时 MaxFire 为 0；仅在 window 未写时补默认 3（显式 window + max=0 表示不限制）
		if c.AlertNotifyMaxFire == 0 {
			c.AlertNotifyMaxFire = DefaultAlertNotifyMaxFire
		}
	}
	if strings.TrimSpace(c.AlertNotifyRepeat) == "" {
		c.AlertNotifyRepeat = DefaultAlertNotifyRepeat
	}
	SyncGlobalAlertNotifyPolicy(c.AlertNotifyMaxFire, c.AlertNotifyWindow, c.AlertNotifyRepeat)
}

// AwaitStuckHours 返回有效的待执行卡住阈值（小时）。
func (c *NotifyConfig) AwaitStuckHours() int {
	if c == nil || c.AwaitExecStuckHours <= 0 {
		return DefaultAwaitExecStuckHours
	}
	return c.AwaitExecStuckHours
}

func (d *DB) GetNotifyConfig() (*NotifyConfig, error) {
	v, err := d.GetGlobalArg(KeyNotifyConfig)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(v) == "" {
		return DefaultNotifyConfig(), nil
	}
	var c NotifyConfig
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		return DefaultNotifyConfig(), nil
	}
	if c.AwaitExecStuckHours <= 0 {
		c.AwaitExecStuckHours = DefaultAwaitExecStuckHours
	}
	ApplyAlertNotifyDefaultsOnLoad(&c)
	return &c, nil
}

func (d *DB) SetNotifyConfig(c *NotifyConfig) error {
	if c == nil {
		return d.SetGlobalArg(KeyNotifyConfig, "", "通知渠道配置")
	}
	hours := c.AwaitExecStuckHours
	if hours <= 0 {
		hours = DefaultAwaitExecStuckHours
	}
	toStore := NotifyConfig{
		EmailOnTicketCreate:     c.EmailOnTicketCreate,
		EmailOnTicketExecFailed: c.EmailOnTicketExecFailed,
		EmailOnTicketAwaitExec:  c.EmailOnTicketAwaitExec,
		EmailOnTicketAwaitStuck: c.EmailOnTicketAwaitStuck,
		EmailOnBackupFailed:     c.EmailOnBackupFailed,
		EmailOnJobFailed:        c.EmailOnJobFailed,
		EmailOnAlert:            c.EmailOnAlert,
		AwaitExecStuckHours:     hours,
		WebhookEnabled:          c.WebhookEnabled,
		WebhookURL:              strings.TrimSpace(c.WebhookURL),
		WebhookOnAlert:          c.WebhookOnAlert,
		WecomEnabled:            c.WecomEnabled,
		WecomCorpID:             strings.TrimSpace(c.WecomCorpID),
		WecomAgentID:            strings.TrimSpace(c.WecomAgentID),
		WecomSecret:             strings.TrimSpace(c.WecomSecret),
		WecomOnAlert:            c.WecomOnAlert,
		WecomToUser:             "", // 废弃：不再持久化默认接收人
		WecomOnTicketCreate:     c.WecomOnTicketCreate,
		PhoneEnabled:            c.PhoneEnabled,
		PhoneAPIURL:             strings.TrimSpace(c.PhoneAPIURL),
		PhoneOnAlert:            c.PhoneOnAlert,
		LarkOnTicketCreate:      c.LarkOnTicketCreate,
		LarkOnAlert:             c.LarkOnAlert,
		LarkAlertOnAlert:        c.LarkAlertOnAlert,
		SMSOnAlert:              c.SMSOnAlert,
		DingtalkOnAlert:         c.DingtalkOnAlert,
		RobotLarkOnAlert:        c.RobotLarkOnAlert,
		RobotsOnTicketCreate:    c.RobotsOnTicketCreate,
		RobotRoutes:             SanitizeRobotRoutes(c.RobotRoutes),
		AlertNotifyMaxFire:      c.AlertNotifyMaxFire,
		AlertNotifyWindow:       strings.TrimSpace(c.AlertNotifyWindow),
		AlertNotifyRepeat:       strings.TrimSpace(c.AlertNotifyRepeat),
	}
	ApplyAlertNotifyDefaultsOnLoad(&toStore)
	raw, err := json.Marshal(toStore)
	if err != nil {
		return err
	}
	return d.SetGlobalArg(KeyNotifyConfig, string(raw), "通知渠道配置")
}

func (c *NotifyConfig) Public() map[string]any {
	if c == nil {
		c = DefaultNotifyConfig()
	}
	// 密钥 / URL 不回显；凭证以 Nacos notify 段为准，库内旧字段仅作服务端回退。
	return map[string]any{
		"email_on_ticket_create":           c.EmailOnTicketCreate,
		"email_on_ticket_exec_failed":      c.EmailOnTicketExecFailed,
		"email_on_ticket_await_exec":       c.EmailOnTicketAwaitExec,
		"email_on_ticket_await_exec_stuck": c.EmailOnTicketAwaitStuck,
		"email_on_backup_failed":           c.EmailOnBackupFailed,
		"email_on_job_failed":              c.EmailOnJobFailed,
		"email_on_alert":                   c.EmailOnAlert,
		"await_exec_stuck_hours":           c.AwaitStuckHours(),
		"webhook_on_alert":                 c.WebhookOnAlert,
		"wecom_on_alert":                   c.WecomOnAlert,
		"wecom_on_ticket_create":           c.WecomOnTicketCreate,
		"wecom_legacy_secret_set":          strings.TrimSpace(c.WecomSecret) != "",
		"phone_on_alert":                   c.PhoneOnAlert,
		"lark_on_ticket_create":            c.LarkOnTicketCreate,
		"lark_on_alert":                    c.LarkOnAlert,
		"lark_alert_on_alert":              c.LarkAlertOnAlert,
		"sms_on_alert":                     c.SMSOnAlert,
		"dingtalk_on_alert":                c.DingtalkOnAlert,
		"robot_lark_on_alert":              c.RobotLarkOnAlert,
		"robots_on_ticket_create":          c.RobotsOnTicketCreate,
		"robot_routes":                     SanitizeRobotRoutes(c.RobotRoutes),
		"alert_notify_max_fire":            c.AlertNotifyMaxFire,
		"alert_notify_window":              c.AlertNotifyWindow,
		"alert_notify_repeat":              c.AlertNotifyRepeat,
	}
}
