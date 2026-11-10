package cdc

import (
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

const (
	SourceUnboundMsg   = "源端实例未绑定"
	SourceAddrMissMsg  = "这个地址没有登记成实例"
	SourceAddrMultiMsg = "这个地址对应多台实例，无法确定源端"
)

func instanceHasAddr(inst *store.Instance, addr string) bool {
	addr = strings.TrimSpace(addr)
	if inst == nil || addr == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(inst.Host), addr) || strings.EqualFold(strings.TrimSpace(inst.DomainName), addr) {
		return true
	}
	for _, d := range inst.Domains {
		if strings.EqualFold(strings.TrimSpace(d), addr) {
			return true
		}
	}
	return false
}

func sameSourceTenant(taskTenant, instTenant string) bool {
	taskTenant = strings.TrimSpace(taskTenant)
	if taskTenant == "" {
		return true
	}
	return strings.TrimSpace(instTenant) == taskTenant
}

// PickSourceInstance SourceID 已能取到实例时直接用。取不到时按地址匹配 host、主域名和附加域名。
// 唯一命中返回该实例；多台命中或一台都没有时返回说明，不猜测。
func PickSourceInstance(bound *store.Instance, candidates []store.Instance, tenantID string, addrs []string) (*store.Instance, string) {
	if bound != nil {
		return bound, ""
	}
	want := map[string]struct{}{}
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			want[strings.ToLower(addr)] = struct{}{}
		}
	}
	if len(want) == 0 {
		return nil, SourceUnboundMsg
	}
	var hits []store.Instance
	seen := map[string]struct{}{}
	for i := range candidates {
		inst := candidates[i]
		if strings.TrimSpace(inst.ID) == "" || !sameSourceTenant(tenantID, inst.TenantID) {
			continue
		}
		if _, ok := seen[inst.ID]; ok {
			continue
		}
		matched := false
		for addr := range want {
			if instanceHasAddr(&inst, addr) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		seen[inst.ID] = struct{}{}
		hits = append(hits, inst)
	}
	switch len(hits) {
	case 0:
		return nil, SourceAddrMissMsg
	case 1:
		cp := hits[0]
		return &cp, ""
	default:
		return nil, SourceAddrMultiMsg
	}
}

// LookupSourceCandidates 按地址取出可能的实例，并丢掉附加域名里的子串误伤。
func LookupSourceCandidates(listByAddr func(string) ([]store.Instance, error), addrs []string) ([]store.Instance, error) {
	if listByAddr == nil {
		return nil, nil
	}
	seen := map[string]struct{}{}
	var out []store.Instance
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		list, err := listByAddr(addr)
		if err != nil {
			return nil, err
		}
		for i := range list {
			inst := list[i]
			if strings.TrimSpace(inst.ID) == "" {
				continue
			}
			if _, ok := seen[inst.ID]; ok {
				continue
			}
			if !instanceHasAddr(&inst, addr) {
				continue
			}
			seen[inst.ID] = struct{}{}
			out = append(out, inst)
		}
	}
	return out, nil
}

// LoadTaskSourceInstance 先用任务上的实例 ID。对不上时按生效地址匹配，不写回任务。
func LoadTaskSourceInstance(st *store.DB, t *store.CDCTask, extra map[string]string) (*store.Instance, string, error) {
	if st == nil || t == nil {
		return nil, SourceUnboundMsg, nil
	}
	var bound *store.Instance
	if id := strings.TrimSpace(t.SourceID); id != "" {
		inst, err := st.GetInstance(id)
		if err != nil {
			return nil, "", err
		}
		bound = inst
	}
	if bound != nil {
		return bound, "", nil
	}
	host, _ := EffectiveSourceEndpoint(extra, nil)
	cands, err := LookupSourceCandidates(st.ListInstancesByAddr, []string{host})
	if err != nil {
		return nil, "", err
	}
	inst, reason := PickSourceInstance(nil, cands, t.TenantID, []string{host})
	return inst, reason, nil
}

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
