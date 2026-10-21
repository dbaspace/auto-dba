package copilot

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"auto-dba/internal/aiops"
	"auto-dba/internal/store"
)

var hourRE = regexp.MustCompile(`(\d+)\s*(?:个)?小时`)

// prefetchBECPU 先对平台实例目录，再按该实例的 host 查已配置 Prometheus 的 BE CPU。
// 不是 BE CPU 问句时返回空。目录没有实例、引擎不符或查询失败时，说明停在哪一步。
func (s *Service) prefetchBECPU(ctx context.Context, tenant, message string) string {
	if !wantsBECPU(message) {
		return ""
	}
	u := aiops.ParseUtterance(message)
	if u.Write {
		return ""
	}
	host := strings.TrimSpace(u.Object)
	if err := validatePromHost(host); err != nil {
		return "平台基础数据：未识别出合法实例主机，未到 Prometheus 查询。"
	}
	if s == nil || s.Store == nil {
		return "平台基础数据：实例目录不可用，未到 Prometheus 查询。"
	}
	list, err := s.Store.ListInstancesInTenant(tenant, "", host)
	if err != nil {
		return "平台基础数据：查询实例目录失败（" + err.Error() + "），未到 Prometheus 查询。"
	}
	it := pickInstance(list, host)
	if it == nil {
		return fmt.Sprintf("平台基础数据：目录里没有实例 %s，未到 Prometheus 查询。", host)
	}
	fact := formatInstanceFact(it)
	if !isStarRocksLike(it.DBType) {
		return fact + fmt.Sprintf("\n该实例引擎是 %s，不是 StarRocks/Doris，未按 BE CPU 查询。", it.DBType)
	}
	label := strings.TrimSpace(it.Host)
	if label == "" {
		label = strings.TrimSpace(it.DomainName)
	}
	if err := validatePromHost(label); err != nil {
		return fact + "\n实例主机名不能用作 Prometheus 标签，未查询。"
	}
	hours := utteranceHours(u.Time)
	res := s.ExecProm(ctx, it, beCPUExpr, hours)
	head := fmt.Sprintf("%s\n已用该实例 host=%s 查询全局参数 awr_prometheus_base_url 上最近 %d 小时的 BE CPU（starrocks_be_cpu，按 instance 的非 idle 占比）。", fact, label, hours)
	if !res.OK {
		return head + "\nPrometheus 查询失败：" + res.Text() + "\n不要编造数值，说明是 Prometheus 这一步失败。"
	}
	return head + "\n" + res.Text()
}

var beWordRE = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])be(?:[^a-z0-9]|$)`)

func wantsBECPU(q string) bool {
	low := strings.ToLower(q)
	if !strings.Contains(low, "cpu") {
		return false
	}
	return beWordRE.MatchString(low) || strings.Contains(q, "节点")
}

func utteranceHours(timeStr string) int {
	m := hourRE.FindStringSubmatch(timeStr)
	if len(m) != 2 {
		return 1
	}
	return rangeHours(m[1])
}

func isStarRocksLike(dbType string) bool {
	d := strings.ToLower(strings.TrimSpace(dbType))
	return d == "sr" || strings.Contains(d, "starrocks") || strings.Contains(d, "doris")
}

func pickInstance(list []store.Instance, host string) *store.Instance {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil
	}
	var fuzzy *store.Instance
	for i := range list {
		it := &list[i]
		if strings.EqualFold(it.Host, host) || strings.EqualFold(it.DomainName, host) || strings.EqualFold(it.Name, host) {
			return it
		}
		for _, d := range it.Domains {
			if strings.EqualFold(strings.TrimSpace(d), host) {
				return it
			}
		}
		if fuzzy == nil {
			fuzzy = it
		}
	}
	return fuzzy
}

func formatInstanceFact(it *store.Instance) string {
	if it == nil {
		return "平台基础数据：无"
	}
	return fmt.Sprintf("平台基础数据：name=%s host=%s domain=%s db_type=%s port=%d", it.Name, it.Host, it.DomainName, it.DBType, it.Port)
}
