package sqlinsight

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"auto-dba/internal/awr"
	"auto-dba/internal/store"
)

// AWRReportLoader 解耦 AWR 生成包；优先走 store 表，表未就绪时返回空。
type AWRReportLoader interface {
	ListReportsForDay(ctx context.Context, day time.Time, host string, port int) ([]AWRReportView, error)
}

// StubAWRLoader 空实现。
type StubAWRLoader struct{}

func (StubAWRLoader) ListReportsForDay(context.Context, time.Time, string, int) ([]AWRReportView, error) {
	return nil, nil
}

// StoreAWRLoader 从 tbl_db_awr_reports 加载（含 sections）。
type StoreAWRLoader struct {
	DB *store.DB
}

func (l StoreAWRLoader) ListReportsForDay(ctx context.Context, day time.Time, host string, port int) ([]AWRReportView, error) {
	if l.DB == nil {
		return nil, nil
	}
	dayOnly := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	list, _, err := l.DB.ListAWRReports(ctx, store.AWRListFilter{
		ReportDate: &dayOnly,
		Host:       strings.TrimSpace(host),
		Port:       int64(port),
		Limit:      500,
	})
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "no such table") {
			return nil, nil
		}
		return nil, err
	}
	out := make([]AWRReportView, 0, len(list))
	for _, r := range list {
		if r == nil {
			continue
		}
		full, err := l.DB.GetAWRReport(ctx, store.AWRGetFilter{
			ReportDate: &dayOnly, Host: r.Host, Port: r.Port,
		})
		if err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "does not exist") || strings.Contains(msg, "no such table") {
				return nil, nil
			}
			return nil, err
		}
		view := AWRReportView{
			Host: r.Host, Port: r.Port, InstanceID: r.InstanceID, InstanceName: r.InstanceName,
			Anomalies: r.Anomalies,
		}
		if full != nil {
			view.Sections = full.Sections
			if len(full.Anomalies) > 0 {
				view.Anomalies = full.Anomalies
			}
		}
		out = append(out, view)
	}
	return out, nil
}

func findAWRMetric(s *awr.AwrSections, key string) (*awr.AwrMetric, string) {
	if s == nil {
		return nil, ""
	}
	for _, sec := range s.Sections {
		if sec == nil {
			continue
		}
		for _, m := range sec.Metrics {
			if m != nil && m.Key == key {
				return m, sec.Name
			}
		}
	}
	return nil, ""
}

func awrMetricStat(m *awr.AwrMetric, stat string) *float64 {
	if m == nil {
		return nil
	}
	switch stat {
	case "max":
		if m.Max != nil {
			return m.Max
		}
	case "min":
		if m.Min != nil {
			return m.Min
		}
	case "last":
		if m.Last != nil {
			return m.Last
		}
	case "avg":
		if m.Avg != nil {
			return m.Avg
		}
	}
	if m.Max != nil {
		return m.Max
	}
	if m.Avg != nil {
		return m.Avg
	}
	if m.Last != nil {
		return m.Last
	}
	if m.Min != nil {
		return m.Min
	}
	return nil
}

func parseAWRSections(rep *AWRReportView) (*awr.AwrSections, map[string]*awr.AwrMetric) {
	metrics := map[string]*awr.AwrMetric{}
	if rep == nil || len(rep.Sections) == 0 {
		return nil, metrics
	}
	sec := &awr.AwrSections{}
	if err := json.Unmarshal(rep.Sections, sec); err != nil {
		return nil, metrics
	}
	for _, s := range sec.Sections {
		if s == nil {
			continue
		}
		for _, m := range s.Metrics {
			if m == nil || m.Key == "" {
				continue
			}
			metrics[m.Key] = m
		}
	}
	return sec, metrics
}
