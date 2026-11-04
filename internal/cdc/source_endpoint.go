package cdc

import (
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

// EffectiveSourceEndpoint 任务实际使用的源端地址：
// 任务 source_host > Debezium database.hostname > 实例 host > 实例主域名。
// 同一实例下不同库可能走不同域名，实例 host 只作兜底。
func EffectiveSourceEndpoint(extra map[string]string, inst *store.Instance) (string, int) {
	host, port := "", 0
	if extra != nil {
		host = strings.TrimSpace(extra["source_host"])
		port, _ = strconv.Atoi(strings.TrimSpace(extra["source_port"]))
		if host == "" {
			cfg := CurrentConfigFromExtra(extra)
			host = strings.TrimSpace(cfg["database.hostname"])
			if p, _ := strconv.Atoi(strings.TrimSpace(cfg["database.port"])); p > 0 {
				port = p
			}
		}
	}
	if host == "" && inst != nil {
		host = defaultStr(strings.TrimSpace(inst.Host), strings.TrimSpace(inst.DomainName))
	}
	if port <= 0 && inst != nil {
		port = inst.Port
	}
	return host, port
}

// TaskSourceInstance 返回按任务生效源端地址覆盖 Host/Port 的实例副本，用于 CDC 源端连接。
// 生效顺序与 EffectiveSourceEndpoint 一致：source_host > Debezium database.hostname > 实例 host。
func TaskSourceInstance(inst *store.Instance, extra map[string]string) *store.Instance {
	if inst == nil {
		return nil
	}
	cp := *inst
	host, port := EffectiveSourceEndpoint(extra, inst)
	if host != "" {
		cp.Host = host
	}
	if port > 0 {
		cp.Port = port
	}
	return &cp
}
