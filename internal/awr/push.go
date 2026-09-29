package awr

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// dailyPush STUB：仅打日志，不发送企业微信/MDM。
func (s *Service) dailyPush(ctx context.Context, day time.Time) {
	if !s.pushEnabled() {
		return
	}
	dayStr := day.Format("2006-01-02")
	body := s.buildPushBody(ctx, day, dayStr)
	log.Printf("awr: push stub (no WeChat/MDM) day=%s body=\n%s", dayStr, body)
}

func (s *Service) buildPushBody(ctx context.Context, day time.Time, dayStr string) string {
	d := dateOnlyUTC(day)
	rows, total, err := s.Store.ListAWRReports(ctx, store.AWRListFilter{ReportDate: &d, Limit: 500})
	if err != nil {
		return fmt.Sprintf("日期 %s：读取报告失败：%v", dayStr, err)
	}
	var partial, failed []string
	for _, r := range rows {
		switch r.Status {
		case "partial":
			partial = append(partial, fmt.Sprintf("%s:%d", r.Host, r.Port))
		case "failed":
			failed = append(failed, fmt.Sprintf("%s:%d", r.Host, r.Port))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "日期: %s\n报告总数: %d\n", dayStr, total)
	if sum, err := s.Store.AnomalySummaryAWR(ctx, store.AWRAnomalySummaryFilter{ReportDate: &d, Limit: 10}); err == nil && sum != nil {
		fmt.Fprintf(&b, "异常概览: 严重(critical) %d / 告警(warning) %d / 命中规则 %d 条\n",
			sum.CriticalCount, sum.WarningCount, sum.AnomalyTotal)
		if len(sum.TopInstances) > 0 {
			b.WriteString("Top 异常实例:\n")
			for _, r := range sum.TopInstances {
				label := strings.TrimSpace(r.InstanceName)
				if label == "" {
					label = r.Host
				}
				fmt.Fprintf(&b, "  [%s] %s %s:%d 命中 %d 项\n",
					strings.ToUpper(r.Severity), label, r.Host, r.Port, r.AnomalyCount)
			}
		}
	}
	fmt.Fprintf(&b, "部分异常(partial): %d\n", len(partial))
	if len(partial) > 0 {
		fmt.Fprintf(&b, "  %s\n", strings.Join(partial, ", "))
	}
	fmt.Fprintf(&b, "失败(failed): %d\n", len(failed))
	if len(failed) > 0 {
		fmt.Fprintf(&b, "  %s\n", strings.Join(failed, ", "))
	}
	return b.String()
}
