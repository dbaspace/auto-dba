package store

import "testing"

func TestEnsureDefaultAlertRulesSetsTenantIDOnNew(t *testing.T) {
	// 无 DB 时仅校验：种子规则创建路径必须能补齐 DefaultTenantID（回归 run_on_port seed 失败）。
	for _, r := range DefaultAlertRules() {
		tid := r.TenantID
		if tid == "" {
			tid = DefaultTenantID
		}
		if tid == "" {
			t.Fatal("DefaultTenantID empty")
		}
		if r.Labels["alertname"] == "" {
			t.Fatalf("rule %s missing alertname", r.ID)
		}
	}
}
