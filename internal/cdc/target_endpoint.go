package cdc

import (
	"fmt"
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

// MatchInstanceByEndpoint 按 host/域名/domains 匹配实例；port>0 时还要比端口。
// 仅唯一命中时返回实例，否则 nil。
func MatchInstanceByEndpoint(list []store.Instance, host string, port int) *store.Instance {
	want := strings.ToLower(strings.TrimSpace(host))
	if want == "" {
		return nil
	}
	var hits []*store.Instance
	for i := range list {
		it := &list[i]
		if !instanceEndpointMatches(it, want) {
			continue
		}
		if port > 0 && it.Port > 0 && it.Port != port {
			continue
		}
		hits = append(hits, it)
	}
	if len(hits) != 1 {
		return nil
	}
	return hits[0]
}

func instanceEndpointMatches(it *store.Instance, wantLower string) bool {
	if it == nil || wantLower == "" {
		return false
	}
	names := []string{it.Host, it.DomainName}
	names = append(names, it.Domains...)
	for _, raw := range names {
		for _, part := range strings.Split(raw, ",") {
			if strings.EqualFold(strings.TrimSpace(part), wantLower) {
				return true
			}
		}
	}
	return false
}

func targetHostPortFromExtra(extra map[string]string) (string, int) {
	if extra == nil {
		return "", 0
	}
	host := strings.TrimSpace(extra["target_host"])
	port, _ := strconv.Atoi(strings.TrimSpace(extra["target_port"]))
	return host, port
}

func formatHostPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if port > 0 {
		return fmt.Sprintf("%s:%d", host, port)
	}
	return host
}

// ResolveTaskTargetInstance 解析 CDC 目标端 StarRocks 实例：
// target_instance_id > extra.target_host/port 匹配登记实例。
// 返回实例与错误文案；文案空表示已解析到实例。
// 若任务 TargetID 为空且唯一命中，会写回 TargetID（内存 + 库）。
func ResolveTaskTargetInstance(st *store.DB, t *store.CDCTask, extra map[string]string) (*store.Instance, string) {
	if t == nil {
		return nil, "目标端未配置"
	}
	host, port := targetHostPortFromExtra(extra)
	addr := formatHostPort(host, port)

	if id := strings.TrimSpace(t.TargetID); id != "" {
		if inst, _ := st.GetInstance(id); inst != nil {
			return inst, ""
		}
		if host != "" {
			if hit := findStarRocksByEndpoint(st, host, port); hit != nil {
				return hit, ""
			}
			return nil, fmt.Sprintf("目标实例未登记（%s）", addr)
		}
		return nil, "目标实例不存在"
	}

	if host == "" {
		return nil, "目标端未配置"
	}
	hit := findStarRocksByEndpoint(st, host, port)
	if hit == nil {
		return nil, fmt.Sprintf("目标实例未登记（%s）", addr)
	}
	t.TargetID = hit.ID
	if st != nil && strings.TrimSpace(t.ID) != "" {
		_ = st.UpdateCDCTargetID(t.ID, hit.ID)
	}
	return hit, ""
}

func findStarRocksByEndpoint(st *store.DB, host string, port int) *store.Instance {
	if st == nil {
		return nil
	}
	list, err := st.ListInstances("", "")
	if err != nil || len(list) == 0 {
		return nil
	}
	var sr []store.Instance
	for _, it := range list {
		eng := store.NormalizeEngineType(it.DBType)
		if eng == "starrocks" || eng == "sr" || strings.EqualFold(strings.TrimSpace(it.DBType), "starrocks") {
			sr = append(sr, it)
		}
	}
	if len(sr) == 0 {
		sr = list
	}
	return MatchInstanceByEndpoint(sr, host, port)
}
