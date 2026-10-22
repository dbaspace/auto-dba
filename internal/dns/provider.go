package dns

import (
	"strings"
	"sync"
	"time"
)

// Provider 按需连接解析库；连接失败时 30 秒内直接返回上次错误，之后重试。
type Provider struct {
	DSN      string
	NodeUser string
	NodePass string
	NodeDB   string

	mu      sync.Mutex
	store   *Store
	lastErr error
	lastTry time.Time
}

const providerRetry = 30 * time.Second

func (p *Provider) Configured() bool {
	return p != nil && strings.TrimSpace(p.DSN) != ""
}

func (p *Provider) Get() (*Store, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.store != nil {
		return p.store, nil
	}
	if p.lastErr != nil && time.Since(p.lastTry) < providerRetry {
		return nil, p.lastErr
	}
	p.lastTry = time.Now()
	s, err := Open(p.DSN, p.NodeUser, p.NodePass, p.NodeDB)
	if err != nil {
		p.lastErr = err
		return nil, err
	}
	p.store, p.lastErr = s, nil
	return s, nil
}
