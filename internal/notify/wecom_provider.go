package notify

import (
	"fmt"
	"strings"

	"auto-dba/internal/config"
)

type wecomSender struct {
	corpID  string
	secret  string
	agentID string
	apiURL  string
}

func newWecomSender(cfg config.WechatNotifyConfig) *wecomSender {
	api := strings.TrimSpace(cfg.APIURL)
	if api == "" {
		api = "https://qyapi.weixin.qq.com/cgi-bin/"
	}
	return &wecomSender{
		corpID:  strings.TrimSpace(cfg.CorpID),
		secret:  strings.TrimSpace(cfg.APISecret),
		agentID: strings.TrimSpace(cfg.AgentID),
		apiURL:  api,
	}
}

func (s *wecomSender) Close() {}

func (s *wecomSender) Send(msg Message) error {
	to := strings.TrimSpace(msg.WecomToUser)
	party := strings.TrimSpace(msg.WecomToParty)
	if to == "" && party == "" {
		return fmt.Errorf("企微接收人为空：请指定 userid 或部门 ID")
	}
	content := strings.TrimSpace(msg.Markdown)
	if content == "" {
		content = strings.TrimSpace(msg.Text)
	}
	if content == "" {
		return fmt.Errorf("empty wecom content")
	}
	return sendWecomMarkdown(s.corpID, s.secret, s.agentID, to, party, content)
}

// ResolveWecomUserID 从用户档案取企微 userid；未绑定则返回空（不回退用户名/@all）。
func ResolveWecomUserID(imType, imAccount string) string {
	acc := strings.TrimSpace(imAccount)
	if acc == "" {
		return ""
	}
	t := strings.ToLower(strings.TrimSpace(imType))
	if t == "" || t == "wecom" || t == "wechat" || t == "qiyeweixin" {
		return acc
	}
	return ""
}

// ParseWecomTestTarget 解析测试输入：party:15 / 纯数字 → 部门；否则 → userid。
func ParseWecomTestTarget(raw string) (toUser, toParty string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "party:") {
		return "", strings.TrimSpace(raw[len("party:"):])
	}
	if strings.HasPrefix(lower, "dept:") {
		return "", strings.TrimSpace(raw[len("dept:"):])
	}
	// 纯数字（可多段 | 分隔）视为部门 ID
	onlyDigits := true
	for _, r := range raw {
		if r >= '0' && r <= '9' || r == '|' || r == ',' {
			continue
		}
		onlyDigits = false
		break
	}
	if onlyDigits {
		parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '|' || r == ',' })
		var ids []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				ids = append(ids, p)
			}
		}
		return "", strings.Join(ids, "|")
	}
	return raw, ""
}
