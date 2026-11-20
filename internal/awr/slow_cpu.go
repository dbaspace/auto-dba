package awr

import (
	"context"
	"time"

	"auto-dba/internal/promclient"
)

// SlowReportCPUPeak 返回这些实例在窗口内的 CPU 峰值（百分数）。没有监控时 ok 为 false。
func (s *Service) SlowReportCPUPeak(ctx context.Context, names []string, from, to time.Time) (float64, bool) {
	if s == nil {
		return 0, false
	}
	cli, ok := s.newAwrPromClient()
	if !ok {
		return 0, false
	}
	var max float64
	found := false
	for _, name := range names {
		q := "max(qce_postgres_cpu_max{" + s.awrCloudSelector(name) + "})"
		samples, err := cli.QueryRange(ctx, q, from, to, time.Hour)
		if err != nil {
			continue
		}
		for _, sm := range samples {
			for _, p := range promclient.SampleToPoints(sm) {
				if !found || p.V > max {
					max = p.V
					found = true
				}
			}
		}
	}
	return max, found
}
