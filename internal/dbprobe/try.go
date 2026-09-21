package dbprobe

import (
	"fmt"

	"auto-dba/internal/netutil"
)

// tryHosts tries each endpoint from a comma-separated host list until fn succeeds.
func tryHosts[T any](rawHost string, port int, fn func(host string, port int) (T, error)) (T, error) {
	var zero T
	eps := netutil.Endpoints(rawHost, port)
	if len(eps) == 0 {
		return zero, fmt.Errorf("实例未配置连接主机")
	}
	var last error
	for _, ep := range eps {
		v, err := fn(ep.Host, ep.Port)
		if err == nil {
			return v, nil
		}
		last = err
	}
	if len(eps) > 1 && last != nil {
		return zero, fmt.Errorf("已尝试 %d 个节点均失败: %w", len(eps), last)
	}
	return zero, last
}
