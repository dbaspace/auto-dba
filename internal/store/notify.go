package store

import (
	"encoding/json"
	"strings"
)

const KeyNotifyConfig = "notify_config"

const DefaultAwaitExecStuckHours = 2

// NotifyConfig 工单 / 告警等事件通知渠道配置。
type NotifyConfig struct {
	EmailOnTicketCreate     bool   `json:"email_on_ticket_create"`
	EmailOnTicketExecFailed bool   `json:"email_on_ticket_exec_failed"`
	EmailOnTicketAwaitExec  bool   `json:"email_on_ticket_await_exec"`
	EmailOnTicketAwaitStuck bool   `json:"email_on_ticket_await_exec_stuck"`
	EmailOnBackupFailed     bool   `json:"email_on_backup_failed"`
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
	WecomToUser             string `json:"wecom_to_user"` // 默认 @all
	PhoneEnabled            bool   `json:"phone_enabled"`
	PhoneAPIURL             string `json:"phone_api_url"`
	PhoneOnAlert            bool   `json:"phone_on_alert"`
}

func DefaultNotifyConfig() *NotifyConfig {
	return &NotifyConfig{
		AwaitExecStuckHours: DefaultAwaitExecStuckHours,
		WebhookOnAlert:      true,
		WecomToUser:         "@all",
	}
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
	if strings.TrimSpace(c.WecomToUser) == "" {
		c.WecomToUser = "@all"
	}
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
	toUser := strings.TrimSpace(c.WecomToUser)
	if toUser == "" {
		toUser = "@all"
	}
	toStore := NotifyConfig{
		EmailOnTicketCreate:     c.EmailOnTicketCreate,
		EmailOnTicketExecFailed: c.EmailOnTicketExecFailed,
		EmailOnTicketAwaitExec:  c.EmailOnTicketAwaitExec,
		EmailOnTicketAwaitStuck: c.EmailOnTicketAwaitStuck,
		EmailOnBackupFailed:     c.EmailOnBackupFailed,
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
		WecomToUser:             toUser,
		PhoneEnabled:            c.PhoneEnabled,
		PhoneAPIURL:             strings.TrimSpace(c.PhoneAPIURL),
		PhoneOnAlert:            c.PhoneOnAlert,
	}
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
	return map[string]any{
		"email_on_ticket_create":           c.EmailOnTicketCreate,
		"email_on_ticket_exec_failed":      c.EmailOnTicketExecFailed,
		"email_on_ticket_await_exec":       c.EmailOnTicketAwaitExec,
		"email_on_ticket_await_exec_stuck": c.EmailOnTicketAwaitStuck,
		"email_on_backup_failed":           c.EmailOnBackupFailed,
		"email_on_alert":                   c.EmailOnAlert,
		"await_exec_stuck_hours":           c.AwaitStuckHours(),
		"webhook_enabled":                  c.WebhookEnabled,
		"webhook_url":                      c.WebhookURL,
		"webhook_on_alert":                 c.WebhookOnAlert,
		"wecom_enabled":                    c.WecomEnabled,
		"wecom_corp_id":                    c.WecomCorpID,
		"wecom_agent_id":                   c.WecomAgentID,
		"wecom_secret_set":                 strings.TrimSpace(c.WecomSecret) != "",
		"wecom_on_alert":                   c.WecomOnAlert,
		"wecom_to_user":                    c.WecomToUser,
		"phone_enabled":                    c.PhoneEnabled,
		"phone_api_url":                    c.PhoneAPIURL,
		"phone_on_alert":                   c.PhoneOnAlert,
	}
}
