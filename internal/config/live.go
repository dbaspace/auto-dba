package config

import (
	"sync"
	"sync/atomic"
)

var (
	liveMu  sync.RWMutex
	liveCfg *Config
	jwtSec  atomic.Value // string
)

// BindLive 绑定进程内唯一配置指针（启动 Load 后调用一次）。
func BindLive(c *Config) {
	liveMu.Lock()
	defer liveMu.Unlock()
	liveCfg = c
	if c != nil {
		jwtSec.Store(c.App.JWTSecret)
	}
}

// Live 返回已绑定的配置指针（无锁；调用方若需安全读请用 Snapshot / WithR）。
func Live() *Config {
	liveMu.RLock()
	defer liveMu.RUnlock()
	return liveCfg
}

// Snapshot 返回配置深拷贝（业务段为值类型，安全）。
func Snapshot() Config {
	liveMu.RLock()
	defer liveMu.RUnlock()
	if liveCfg == nil {
		return Config{}
	}
	return *liveCfg
}

// WithR 在读锁下访问 live 配置。
func WithR(fn func(*Config)) {
	liveMu.RLock()
	defer liveMu.RUnlock()
	if liveCfg != nil && fn != nil {
		fn(liveCfg)
	}
}

// WithW 在写锁下访问 live 配置（热更 Apply 用）。
func WithW(fn func(*Config)) {
	liveMu.Lock()
	defer liveMu.Unlock()
	if liveCfg != nil && fn != nil {
		fn(liveCfg)
	}
}

// CurrentJWT 无锁读取当前 JWT（atomic），供鉴权热更安全使用。
func CurrentJWT() string {
	v, _ := jwtSec.Load().(string)
	return v
}

// PublishJWT 在配置提交后更新鉴权用密钥。
func PublishJWT(secret string) {
	jwtSec.Store(secret)
}

// LockWrite / UnlockWrite 供 Nacos watch 在 Apply + hooks 期间持有写锁。
func LockWrite()   { liveMu.Lock() }
func UnlockWrite() { liveMu.Unlock() }
