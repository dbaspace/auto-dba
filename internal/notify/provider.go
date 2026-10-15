package notify

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

// Channel 通知渠道标识（不含 email）。
type Channel string

const (
	ChannelWecom     Channel = "wecom"
	ChannelLark      Channel = "lark"
	ChannelLarkAlert Channel = "lark_alert"
	ChannelSMS       Channel = "sms"
	ChannelCall      Channel = "call"
	ChannelRobot     Channel = "robot"
)

// Message 统一外发消息（邮件仍走原路径）。
type Message struct {
	Title   string
	Text    string
	Markdown string
	// LarkUserIDs 飞书 user_id 列表
	LarkUserIDs []string
	// WecomToUser 企微 touser（| 分隔 userid）
	WecomToUser string
	// WecomToParty 企微 toparty（| 分隔部门 ID）
	WecomToParty string
	// Phones 短信/电话号码
	Phones []string
	// RobotKind 空则发 Nacos robots 全部；否则仅该 kind
	RobotKind string
	// RobotURLs 额外 webhook（如 DB webhook_url）；kind 由 URL 推断或 RobotKind
	RobotURLs []string
	// RobotFor 方向过滤：alert | ticket（空=alert）；与页面 robot_routes 匹配
	RobotFor string
	// RobotRoutes 页面配置的方向表（下标对应 Nacos robots）
	RobotRoutes []store.RobotRoute
	// RobotIndex 非 nil 时仅发该 Nacos robots 下标
	RobotIndex *int
}

type channelSender interface {
	Send(msg Message) error
	Close()
}

type registry struct {
	mu     sync.RWMutex
	cfg    config.NotifyModuleConfig
	wecom  channelSender
	lark   channelSender
	alert  channelSender
	sms    channelSender
	call   channelSender
}

var globalReg registry

// InitProviders 按 Nacos/本地 notify 段重建渠道（邮件除外）。
func InitProviders(cfg config.NotifyModuleConfig) {
	globalReg.mu.Lock()
	defer globalReg.mu.Unlock()
	globalReg.closeLocked()
	globalReg.cfg = cfg

	if cfg.WechatConfigured() {
		globalReg.wecom = newWecomSender(cfg.Provider.Wechat)
		log.Printf("notify-provider: registered wecom")
	}
	if cfg.LarkConfigured() {
		s, err := newLarkSender(cfg.Provider.Lark)
		if err != nil {
			log.Printf("notify-provider: lark init failed: %v", err)
		} else {
			globalReg.lark = s
			log.Printf("notify-provider: registered lark")
		}
	}
	if cfg.AlertLarkConfigured() {
		s, err := newLarkSender(cfg.Provider.AlertLark)
		if err != nil {
			log.Printf("notify-provider: alert_lark init failed: %v", err)
		} else {
			globalReg.alert = s
			log.Printf("notify-provider: registered alert_lark")
		}
	}
	if s := newSMSSender(cfg); s != nil {
		globalReg.sms = s
		log.Printf("notify-provider: registered sms backend=%s", strings.TrimSpace(cfg.Provider.SMS))
	}
	if s := newCallSender(cfg); s != nil {
		globalReg.call = s
		log.Printf("notify-provider: registered call backend=%s", strings.TrimSpace(cfg.Provider.Call))
	}
}

func (r *registry) closeLocked() {
	for _, s := range []channelSender{r.wecom, r.lark, r.alert, r.sms, r.call} {
		if s != nil {
			s.Close()
		}
	}
	r.wecom, r.lark, r.alert, r.sms, r.call = nil, nil, nil, nil, nil
}

// ProviderStatus 只读能力状态。
func ProviderStatus() map[string]any {
	globalReg.mu.RLock()
	defer globalReg.mu.RUnlock()
	st := globalReg.cfg.StatusPublic()
	st["wecom_ready"] = globalReg.wecom != nil
	st["lark_ready"] = globalReg.lark != nil
	st["alert_lark_ready"] = globalReg.alert != nil
	st["sms_ready"] = globalReg.sms != nil
	st["call_ready"] = globalReg.call != nil
	return st
}

// NotifyModule 返回当前生效的 notify 配置副本。
func NotifyModule() config.NotifyModuleConfig {
	globalReg.mu.RLock()
	defer globalReg.mu.RUnlock()
	return globalReg.cfg
}

func sendChannel(ch Channel, msg Message) error {
	globalReg.mu.RLock()
	defer globalReg.mu.RUnlock()
	var s channelSender
	switch ch {
	case ChannelWecom:
		s = globalReg.wecom
	case ChannelLark:
		s = globalReg.lark
	case ChannelLarkAlert:
		s = globalReg.alert
	case ChannelSMS:
		s = globalReg.sms
	case ChannelCall:
		s = globalReg.call
	case ChannelRobot:
		return sendRobots(globalReg.cfg, msg)
	default:
		return nil
	}
	if s == nil {
		return nil
	}
	return s.Send(msg)
}

// SendVia 向已注册渠道发送；未注册则静默跳过。
func SendVia(ch Channel, msg Message) {
	if err := sendChannel(ch, msg); err != nil {
		log.Printf("notify-provider: %s send failed: %v", ch, err)
	}
}

// SendViaErr 同 SendVia，但返回错误（测试接口用）；渠道未注册返回明确错误。
func SendViaErr(ch Channel, msg Message) error {
	globalReg.mu.RLock()
	ready := false
	switch ch {
	case ChannelWecom:
		ready = globalReg.wecom != nil
	case ChannelLark:
		ready = globalReg.lark != nil
	case ChannelLarkAlert:
		ready = globalReg.alert != nil
	case ChannelSMS:
		ready = globalReg.sms != nil
	case ChannelCall:
		ready = globalReg.call != nil
	case ChannelRobot:
		ready = len(globalReg.cfg.Robots) > 0 || len(msg.RobotURLs) > 0
	}
	globalReg.mu.RUnlock()
	if !ready {
		if ch == ChannelRobot {
			return fmt.Errorf("群机器人未配置：请在 Nacos notify.robots 填写 url")
		}
		return fmt.Errorf("渠道 %s 未在 Nacos notify 段配置或未启用", ch)
	}
	return sendChannel(ch, msg)
}
