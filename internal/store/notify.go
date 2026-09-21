package store

import (
	"encoding/json"
	"strings"
)

const KeyNotifyConfig = "notify_config"

// NotifyConfig 工单等事件通知渠道配置（邮件复用 SMTP；其它渠道本期占位）。
type NotifyConfig struct {
	EmailOnTicketCreate bool   `json:"email_on_ticket_create"`
	WebhookEnabled      bool   `json:"webhook_enabled"`
	WebhookURL          string `json:"webhook_url"`
	WecomEnabled        bool   `json:"wecom_enabled"`
	WecomCorpID         string `json:"wecom_corp_id"`
	WecomAgentID        string `json:"wecom_agent_id"`
	WecomSecret         string `json:"wecom_secret"`
	PhoneEnabled        bool   `json:"phone_enabled"`
	PhoneAPIURL         string `json:"phone_api_url"`
}

func DefaultNotifyConfig() *NotifyConfig {
	return &NotifyConfig{}
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
	return &c, nil
}

func (d *DB) SetNotifyConfig(c *NotifyConfig) error {
	if c == nil {
		return d.SetGlobalArg(KeyNotifyConfig, "", "通知渠道配置")
	}
	toStore := NotifyConfig{
		EmailOnTicketCreate: c.EmailOnTicketCreate,
		WebhookEnabled:      c.WebhookEnabled,
		WebhookURL:          strings.TrimSpace(c.WebhookURL),
		WecomEnabled:        c.WecomEnabled,
		WecomCorpID:         strings.TrimSpace(c.WecomCorpID),
		WecomAgentID:        strings.TrimSpace(c.WecomAgentID),
		WecomSecret:         strings.TrimSpace(c.WecomSecret),
		PhoneEnabled:        c.PhoneEnabled,
		PhoneAPIURL:         strings.TrimSpace(c.PhoneAPIURL),
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
		"email_on_ticket_create": c.EmailOnTicketCreate,
		"webhook_enabled":        c.WebhookEnabled,
		"webhook_url":            c.WebhookURL,
		"wecom_enabled":          c.WecomEnabled,
		"wecom_corp_id":          c.WecomCorpID,
		"wecom_agent_id":         c.WecomAgentID,
		"wecom_secret_set":       strings.TrimSpace(c.WecomSecret) != "",
		"phone_enabled":          c.PhoneEnabled,
		"phone_api_url":          c.PhoneAPIURL,
	}
}
