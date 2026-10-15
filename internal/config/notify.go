package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// NotifyModuleConfig Nacos/本地 YAML 中的通知渠道能力（不含邮件；邮件仍走库表 SMTP）。
type NotifyModuleConfig struct {
	Provider     NotifyProviderConfig `yaml:"provider"`
	TeleProvider TeleProviderConfig   `yaml:"tele_provider"`
	Robots       []NotifyRobotConfig  `yaml:"robots"`
}

// NotifyProviderConfig 对齐 neptune-notify provider（无 email）。
type NotifyProviderConfig struct {
	Wechat    WechatNotifyConfig `yaml:"wechat"`
	Lark      LarkNotifyConfig   `yaml:"lark"`
	AlertLark LarkNotifyConfig   `yaml:"alert_lark"`
	Call      string             `yaml:"call"` // cloopen | huawei | yjzy
	SMS       string             `yaml:"sms"`  // cloopen | yjzy
}

type WechatNotifyConfig struct {
	Enabled   bool     `yaml:"enabled"`
	CorpID    string   `yaml:"corp_id"`
	AgentID   string   `yaml:"agent_id"`
	APISecret string   `yaml:"api_secret"`
	APIURL    string   `yaml:"api_url"`
	PartyIDs  PartyIDs `yaml:"party_ids"` // 告警默认 toparty，如 "15" 或 ["15","16"]
}

// PartyIDs 支持 YAML 字符串（逗号分隔）或字符串列表。
type PartyIDs []string

func (p *PartyIDs) UnmarshalYAML(n *yaml.Node) error {
	if n == nil || n.Kind == 0 {
		*p = nil
		return nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		*p = splitPartyIDs(n.Value)
		return nil
	case yaml.SequenceNode:
		var out []string
		for _, c := range n.Content {
			out = append(out, splitPartyIDs(c.Value)...)
		}
		*p = out
		return nil
	default:
		return fmt.Errorf("party_ids: expected string or list")
	}
}

func splitPartyIDs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '|' || r == ';' || r == ' '
	})
	var out []string
	seen := map[string]bool{}
	for _, x := range parts {
		x = strings.TrimSpace(x)
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

// JoinWecomParty 企微 toparty 用 | 连接。
func (p PartyIDs) JoinWecomParty() string {
	if len(p) == 0 {
		return ""
	}
	return strings.Join([]string(p), "|")
}

type LarkNotifyConfig struct {
	Enabled   bool   `yaml:"enabled"`
	AppID     string `yaml:"app_id"`
	AppSecret string `yaml:"app_secret"`
}

type TeleProviderConfig struct {
	Cloopen     CloopenSMSConfig  `yaml:"cloopen"`
	CloopenCall CloopenCallConfig `yaml:"cloopen_call"`
	Huawei      HuaweiCallConfig  `yaml:"huawei"`
	YjzyCall    YjzyTeleConfig    `yaml:"yjzy_call"`
	YjzySMS     YjzyTeleConfig    `yaml:"yjzy_sms"`
}

type CloopenSMSConfig struct {
	Enabled    bool   `yaml:"enabled"`
	APIAccount string `yaml:"api_account"`
	APIToken   string `yaml:"api_token"`
	AppID      string `yaml:"app_id"`
	TemplateID string `yaml:"template_id"`
	Singer     string `yaml:"singer"`
}

type CloopenCallConfig struct {
	Enabled    bool   `yaml:"enabled"`
	APIAccount string `yaml:"api_account"`
	APIToken   string `yaml:"api_token"`
	AppID      string `yaml:"app_id"`
	DisplayNum string `yaml:"display_num"`
	MediaName  string `yaml:"media_name"`
	TTS        bool   `yaml:"tts"`
}

type HuaweiCallConfig struct {
	Enabled         bool     `yaml:"enabled"`
	BaseURL         string   `yaml:"base_url"`
	APIKey          string   `yaml:"api_key"`
	APISecret       string   `yaml:"api_secret"`
	DisplayNumbers  []string `yaml:"display_numbers"`
	TemplateID      string   `yaml:"template_id"`
	ParamNum        int      `yaml:"param_num"`
	ParamSize       int      `yaml:"param_size"`
	StatusURL       string   `yaml:"status_url"`
}

type YjzyTeleConfig struct {
	BaseURL               string `yaml:"base_url"`
	CID                   string `yaml:"cid"`
	Pwd                   string `yaml:"pwd"`
	ProductID             string `yaml:"product_id"`
	ReportIntervalSeconds int    `yaml:"report_interval_seconds"`
	Singer                string `yaml:"singer"`
}

type NotifyRobotConfig struct {
	Kind string `yaml:"kind"` // lark | wechat | dingtalk
	URL  string `yaml:"url"`
}

// LarkConfigured 飞书私聊应用是否已启用且凭证齐全。
func (c NotifyModuleConfig) LarkConfigured() bool {
	return c.Provider.Lark.Enabled &&
		c.Provider.Lark.AppID != "" && c.Provider.Lark.AppSecret != ""
}

// AlertLarkConfigured 飞书告警应用是否可用。
func (c NotifyModuleConfig) AlertLarkConfigured() bool {
	return c.Provider.AlertLark.Enabled &&
		c.Provider.AlertLark.AppID != "" && c.Provider.AlertLark.AppSecret != ""
}

// WechatConfigured Nacos 企微应用是否可用。
func (c NotifyModuleConfig) WechatConfigured() bool {
	w := c.Provider.Wechat
	return w.Enabled && w.CorpID != "" && w.APISecret != "" && w.AgentID != ""
}

// StatusPublic 给前端/API 的只读能力状态（不含密钥）。
func (c NotifyModuleConfig) StatusPublic() map[string]any {
	smsBackend := c.Provider.SMS
	if smsBackend == "" {
		smsBackend = "cloopen"
	}
	callBackend := c.Provider.Call
	if callBackend == "" {
		callBackend = "cloopen"
	}
	smsOK := false
	switch smsBackend {
	case "yjzy":
		smsOK = c.TeleProvider.YjzySMS.BaseURL != "" && c.TeleProvider.YjzySMS.CID != ""
	default:
		smsOK = c.TeleProvider.Cloopen.Enabled && c.TeleProvider.Cloopen.APIAccount != "" &&
			c.TeleProvider.Cloopen.APIToken != "" && c.TeleProvider.Cloopen.AppID != ""
	}
	callOK := false
	switch callBackend {
	case "huawei":
		h := c.TeleProvider.Huawei
		callOK = h.Enabled && h.APIKey != "" && h.APISecret != ""
	case "yjzy":
		callOK = c.TeleProvider.YjzyCall.BaseURL != "" && c.TeleProvider.YjzyCall.CID != ""
	default:
		cc := c.TeleProvider.CloopenCall
		callOK = cc.Enabled && cc.APIAccount != "" && cc.APIToken != "" && cc.AppID != ""
	}
	robots := make([]map[string]any, 0, len(c.Robots))
	for i, r := range c.Robots {
		u := strings.TrimSpace(strings.Trim(r.URL, "`\"'"))
		if u == "" {
			continue
		}
		kind := strings.TrimSpace(r.Kind)
		fp := u
		if hi := strings.Index(u, "/hook/"); hi >= 0 {
			tok := u[hi+len("/hook/"):]
			if len(tok) > 8 {
				tok = "…" + tok[len(tok)-8:]
			}
			host := u
			if j := strings.Index(u, "://"); j >= 0 {
				host = u[j+3:]
				if k := strings.Index(host, "/"); k >= 0 {
					host = host[:k]
				}
			}
			fp = host + "/hook/" + tok
		}
		robots = append(robots, map[string]any{"index": i, "kind": kind, "url": fp})
	}
	partyIDs := []string(c.Provider.Wechat.PartyIDs)
	return map[string]any{
		"wechat_configured":     c.WechatConfigured(),
		"wecom_party_ids":       partyIDs,
		"lark_configured":       c.LarkConfigured(),
		"alert_lark_configured": c.AlertLarkConfigured(),
		"sms_configured":        smsOK,
		"sms_backend":           smsBackend,
		"call_configured":       callOK,
		"call_backend":          callBackend,
		"robots_count":          len(robots),
		"robots":                robots,
	}
}
