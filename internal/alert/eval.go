package alert

import (
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

// EvalSeries 一条规则评估命中的序列。
type EvalSeries struct {
	Labels map[string]string
	Value  float64
}

// MergeLabels 规则标签与样本标签合并（样本优先）。
func MergeLabels(rule, sample map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range rule {
		out[k] = v
	}
	for k, v := range sample {
		if k == "__name__" {
			continue
		}
		out[k] = v
	}
	return out
}

// TitleFrom 从注解 / 规则名生成标题。
func TitleFrom(rule store.AlertRule, labels map[string]string) string {
	if s := strings.TrimSpace(rule.Annotations["summary"]); s != "" {
		return s
	}
	if s := strings.TrimSpace(rule.Annotations["title"]); s != "" {
		return s
	}
	svc := labels["service"]
	if svc == "" {
		svc = labels["job"]
	}
	if svc != "" {
		return rule.Name + " · " + svc
	}
	return rule.Name
}

// FormatValue 格式化当前值展示。
func FormatValue(v float64) string {
	if v == float64(int64(v)) && v < 1e9 {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.4g", v)
}

// SamplesToSeries 将 Prometheus 瞬时结果转为评估序列。
func SamplesToSeries(samples []promclient.Sample) []EvalSeries {
	out := make([]EvalSeries, 0, len(samples))
	for _, s := range samples {
		v, ok := promclient.InstantValue(s)
		if !ok {
			continue
		}
		labels := map[string]string{}
		for k, val := range s.Metric {
			labels[k] = val
		}
		out = append(out, EvalSeries{Labels: labels, Value: v})
	}
	return out
}

// ShouldFire 判断 pending 是否已超过 for 时长。
func ShouldFire(pendingSince string, forDur time.Duration, now time.Time) bool {
	if pendingSince == "" {
		return forDur <= 0
	}
	t, err := time.Parse(time.RFC3339, pendingSince)
	if err != nil {
		return true
	}
	return !now.Before(t.Add(forDur))
}

// ApplySilence 若命中任一生效静默，将 handle 置为 silenced；否则若原先为 silenced 且无人认领则回到 unclaimed。
func ApplySilence(a *store.Alert, silences []store.AlertSilence) bool {
	silenced := false
	for _, s := range silences {
		if store.MatchersMatchLabels(store.ParseAlertMatchers(s.Matchers), a.Labels) {
			silenced = true
			break
		}
	}
	if silenced {
		if a.Handle != store.AlertHandleSilenced {
			a.Handle = store.AlertHandleSilenced
			return true
		}
		return false
	}
	if a.Handle == store.AlertHandleSilenced && a.Owner == "" {
		a.Handle = store.AlertHandleUnclaimed
		return true
	}
	if a.Handle == store.AlertHandleSilenced && a.Owner != "" {
		a.Handle = store.AlertHandleAcknowledged
		return true
	}
	return false
}

// ThresholdHint 从注解取阈值说明。
func ThresholdHint(ann map[string]string) string {
	if ann == nil {
		return ""
	}
	if v := strings.TrimSpace(ann["threshold"]); v != "" {
		return v
	}
	return strings.TrimSpace(ann["description"])
}
