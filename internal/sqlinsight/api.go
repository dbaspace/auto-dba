package sqlinsight

import (
	"context"
	"strings"
)

// CreateRun 手动触发分析。
func (s *Service) CreateRun(ctx context.Context, req *RunCreateReq, operator string) (*RunItem, error) {
	if req == nil {
		req = &RunCreateReq{}
	}
	day, err := parseDay(req.Date)
	if err != nil {
		return nil, err
	}
	if operator == "" {
		operator = "manual"
	}
	scenario := normalizeScenario(req.UserScenario, req.ScenarioTags)
	run, err := s.ExecuteRun(ctx, day, strings.TrimSpace(req.Host), req.Port, "manual", operator, scenario)
	if err != nil {
		return nil, err
	}
	return runToDTO(run, true), nil
}

// GetRun 获取单次运行。
func (s *Service) GetRun(ctx context.Context, id string) (*RunItem, error) {
	r, err := s.Store.GetSqlInsightRun(ctx, id)
	if err != nil {
		return nil, err
	}
	return runToDTO(r, true), nil
}

// ListRuns 分页列出运行。
func (s *Service) ListRuns(ctx context.Context, q *RunQuery) (*RunListData, error) {
	if q == nil {
		q = &RunQuery{}
	}
	offset, limit, cur := pageOffset(q.Current, q.PageSize, 20)
	list, total, err := s.Store.ListSqlInsightRuns(ctx, q.Host, q.Port, offset, limit)
	if err != nil {
		return nil, err
	}
	items := make([]*RunItem, 0, len(list))
	for _, r := range list {
		items = append(items, runToDTO(r, false))
	}
	return &RunListData{List: items, Pagination: Pagination{Total: total, Current: cur, PageSize: limit}}, nil
}

// ListFindings 分页列出发现。
func (s *Service) ListFindings(ctx context.Context, q *FindingQuery) (*FindingListData, error) {
	if q == nil {
		q = &FindingQuery{}
	}
	offset, limit, cur := pageOffset(q.Current, q.PageSize, 50)
	list, total, err := s.Store.ListSqlInsightFindings(ctx, q.RunID, q.Host, q.Severity, q.Port, offset, limit)
	if err != nil {
		return nil, err
	}
	items := make([]*FindingItem, 0, len(list))
	for _, r := range list {
		items = append(items, findingToDTO(r))
	}
	return &FindingListData{List: items, Pagination: Pagination{Total: total, Current: cur, PageSize: limit}}, nil
}

// Summary 汇总。
func (s *Service) Summary(ctx context.Context, q *SummaryQuery) (*SummaryData, error) {
	if q == nil {
		q = &SummaryQuery{}
	}
	if strings.TrimSpace(q.RunID) != "" {
		r, err := s.Store.GetSqlInsightRun(ctx, q.RunID)
		if err != nil {
			return nil, err
		}
		return &SummaryData{
			RunID: r.ID, AnalysisDate: r.AnalysisDate.Format("2006-01-02"),
			CriticalCount: r.CriticalCount, WarningCount: r.WarningCount, InfoCount: r.InfoCount,
			FindingTotal: r.FindingCount, Severity: r.Severity,
		}, nil
	}
	list, _, err := s.Store.ListSqlInsightFindings(ctx, "", q.Host, "", q.Port, 0, 500)
	if err != nil {
		return nil, err
	}
	if d := strings.TrimSpace(q.Date); d != "" {
		filtered := list[:0]
		for _, f := range list {
			if f.AnalysisDate.Format("2006-01-02") == d {
				filtered = append(filtered, f)
			}
		}
		list = filtered
	}
	crit, warn, info := 0, 0, 0
	sev := siSevNone
	for _, f := range list {
		switch f.Severity {
		case siSevCritical:
			crit++
			sev = siSevCritical
		case siSevWarning:
			warn++
			if sev != siSevCritical {
				sev = siSevWarning
			}
		default:
			info++
			if sev == siSevNone {
				sev = siSevInfo
			}
		}
	}
	return &SummaryData{
		CriticalCount: crit, WarningCount: warn, InfoCount: info,
		FindingTotal: len(list), Severity: sev,
	}, nil
}
