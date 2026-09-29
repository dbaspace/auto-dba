package sqlinsight

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"auto-dba/internal/store"
)

func pageOffset(current, pageSize, def int) (offset, limit, cur int) {
	cur = current
	if cur < 1 {
		cur = 1
	}
	limit = pageSize
	if limit <= 0 {
		limit = def
	}
	if limit > 200 {
		limit = 200
	}
	offset = (cur - 1) * limit
	return
}

func metricToDTO(r *store.SqlInsightMetricRow) *MetricItem {
	if r == nil {
		return nil
	}
	return &MetricItem{
		ID: r.ID, MetricKey: r.MetricKey, Name: r.Name, Description: r.Description,
		Source: r.Source, ExprType: r.ExprType, BuiltinField: r.BuiltinField, Unit: r.Unit, Stat: r.Stat,
		ScopeHost: r.ScopeHost, ScopePort: r.ScopePort, Enabled: r.Enabled, System: r.System,
		CreatedBy: r.CreatedBy, UpdatedBy: r.UpdatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func ruleToDTO(r *store.SqlInsightRuleRow) *RuleItem {
	if r == nil {
		return nil
	}
	return &RuleItem{
		ID: r.ID, Name: r.Name, Description: r.Description, MetricKey: r.MetricKey,
		RuleKind: r.RuleKind, Direction: r.Direction, WarnThreshold: r.WarnThreshold, CritThreshold: r.CritThreshold,
		BaselineWindow: r.BaselineWindow, ZScoreK: r.ZScoreK, Enabled: r.Enabled, System: r.System,
		CreatedBy: r.CreatedBy, UpdatedBy: r.UpdatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// CreateMetric 创建自定义指标。
func (s *Service) CreateMetric(ctx context.Context, req *MetricCreateReq, operator string) (*MetricItem, error) {
	key := strings.TrimSpace(req.MetricKey)
	if key == "" {
		return nil, fmt.Errorf("metric_key 必填")
	}
	bf := builtinByKey(req.BuiltinField)
	if bf == nil {
		return nil, fmt.Errorf("builtin_field 不在受控目录中")
	}
	src := strings.TrimSpace(req.Source)
	if src == "" {
		src = bf.Source
	}
	if src != bf.Source {
		return nil, fmt.Errorf("source 须与内置字段一致: %s", bf.Source)
	}
	expr := strings.TrimSpace(req.ExprType)
	if expr == "" {
		expr = siExprBuiltin
	}
	if expr != siExprBuiltin {
		return nil, fmt.Errorf("一期仅支持 expr_type=builtin_field")
	}
	stat := strings.TrimSpace(req.Stat)
	if stat == "" {
		stat = bf.DefaultStat
	}
	unit := strings.TrimSpace(req.Unit)
	if unit == "" {
		unit = bf.Unit
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if operator == "" {
		operator = "manual"
	}
	row := &store.SqlInsightMetricRow{
		ID: uuid.NewString(), MetricKey: key, Name: strings.TrimSpace(req.Name), Description: req.Description,
		Source: src, ExprType: expr, BuiltinField: bf.Key, Unit: unit, Stat: stat,
		ScopeHost: strings.TrimSpace(req.ScopeHost), ScopePort: req.ScopePort, Enabled: enabled,
		CreatedBy: operator, UpdatedBy: operator,
	}
	if err := s.Store.InsertSqlInsightMetric(ctx, row); err != nil {
		return nil, err
	}
	return metricToDTO(row), nil
}

// UpdateMetric 更新非系统指标。
func (s *Service) UpdateMetric(ctx context.Context, id string, req *MetricUpdateReq, operator string) (*MetricItem, error) {
	cur, err := s.Store.GetSqlInsightMetricByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.System {
		return nil, fmt.Errorf("系统预置指标不可修改")
	}
	if req.Name != nil {
		cur.Name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		cur.Description = *req.Description
	}
	if req.BuiltinField != nil {
		bf := builtinByKey(*req.BuiltinField)
		if bf == nil {
			return nil, fmt.Errorf("builtin_field 不在受控目录中")
		}
		cur.BuiltinField = bf.Key
		cur.Source = bf.Source
	}
	if req.Source != nil {
		cur.Source = strings.TrimSpace(*req.Source)
	}
	if req.ExprType != nil {
		cur.ExprType = strings.TrimSpace(*req.ExprType)
	}
	if req.Unit != nil {
		cur.Unit = strings.TrimSpace(*req.Unit)
	}
	if req.Stat != nil {
		cur.Stat = strings.TrimSpace(*req.Stat)
	}
	if req.ScopeHost != nil {
		cur.ScopeHost = strings.TrimSpace(*req.ScopeHost)
	}
	if req.ScopePort != nil {
		cur.ScopePort = *req.ScopePort
	}
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if operator == "" {
		operator = "manual"
	}
	cur.UpdatedBy = operator
	if err := s.Store.UpdateSqlInsightMetric(ctx, cur); err != nil {
		return nil, err
	}
	out, _ := s.Store.GetSqlInsightMetricByID(ctx, id)
	return metricToDTO(out), nil
}

// DeleteMetric 删除非系统指标。
func (s *Service) DeleteMetric(ctx context.Context, id string) error {
	return s.Store.DeleteSqlInsightMetric(ctx, id)
}

// ListMetrics 分页列出指标。
func (s *Service) ListMetrics(ctx context.Context, q *MetricQuery) (*MetricListData, error) {
	offset, limit, cur := pageOffset(q.Current, q.PageSize, 50)
	enabledOnly := q.Enabled != nil && *q.Enabled
	list, total, err := s.Store.ListSqlInsightMetrics(ctx, q.Source, enabledOnly, offset, limit)
	if err != nil {
		return nil, err
	}
	items := make([]*MetricItem, 0, len(list))
	for _, r := range list {
		items = append(items, metricToDTO(r))
	}
	return &MetricListData{List: items, Pagination: Pagination{Total: total, Current: cur, PageSize: limit}}, nil
}

// CreateRule 创建分析规则。
func (s *Service) CreateRule(ctx context.Context, req *RuleCreateReq, operator string) (*RuleItem, error) {
	kind := strings.TrimSpace(req.RuleKind)
	if kind != siRuleThreshold && kind != siRuleDoD && kind != siRuleBaseline {
		return nil, fmt.Errorf("rule_kind 须为 threshold / dod / baseline")
	}
	if _, err := s.Store.GetSqlInsightMetricByKey(ctx, req.MetricKey); err != nil {
		if builtinByKey(req.MetricKey) == nil {
			return nil, fmt.Errorf("metric_key 不存在：%v", err)
		}
	}
	dir := strings.TrimSpace(req.Direction)
	if dir == "" {
		if kind == siRuleDoD {
			dir = siDirSpike
		} else {
			dir = siDirAbove
		}
	}
	win := strings.TrimSpace(req.BaselineWindow)
	if win == "" {
		win = siWindow30d
	}
	zk := 3.0
	if req.ZScoreK != nil {
		zk = *req.ZScoreK
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if operator == "" {
		operator = "manual"
	}
	row := &store.SqlInsightRuleRow{
		ID: uuid.NewString(), Name: strings.TrimSpace(req.Name), Description: req.Description,
		MetricKey: strings.TrimSpace(req.MetricKey), RuleKind: kind, Direction: dir,
		WarnThreshold: req.WarnThreshold, CritThreshold: req.CritThreshold,
		BaselineWindow: win, ZScoreK: zk, Enabled: enabled, CreatedBy: operator, UpdatedBy: operator,
	}
	if err := s.Store.InsertSqlInsightRule(ctx, row); err != nil {
		return nil, err
	}
	return ruleToDTO(row), nil
}

// UpdateRule 更新非系统规则。
func (s *Service) UpdateRule(ctx context.Context, id string, req *RuleUpdateReq, operator string) (*RuleItem, error) {
	cur, err := s.Store.GetSqlInsightRuleByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.System {
		return nil, fmt.Errorf("系统预置规则不可修改")
	}
	if req.Name != nil {
		cur.Name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		cur.Description = *req.Description
	}
	if req.MetricKey != nil {
		cur.MetricKey = strings.TrimSpace(*req.MetricKey)
	}
	if req.RuleKind != nil {
		cur.RuleKind = strings.TrimSpace(*req.RuleKind)
	}
	if req.Direction != nil {
		cur.Direction = strings.TrimSpace(*req.Direction)
	}
	if req.WarnThreshold != nil {
		cur.WarnThreshold = req.WarnThreshold
	}
	if req.CritThreshold != nil {
		cur.CritThreshold = req.CritThreshold
	}
	if req.BaselineWindow != nil {
		cur.BaselineWindow = strings.TrimSpace(*req.BaselineWindow)
	}
	if req.ZScoreK != nil {
		cur.ZScoreK = *req.ZScoreK
	}
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if operator == "" {
		operator = "manual"
	}
	cur.UpdatedBy = operator
	if err := s.Store.UpdateSqlInsightRule(ctx, cur); err != nil {
		return nil, err
	}
	out, _ := s.Store.GetSqlInsightRuleByID(ctx, id)
	return ruleToDTO(out), nil
}

// DeleteRule 删除非系统规则。
func (s *Service) DeleteRule(ctx context.Context, id string) error {
	return s.Store.DeleteSqlInsightRule(ctx, id)
}

// ListRules 分页列出规则。
func (s *Service) ListRules(ctx context.Context, q *RuleQuery) (*RuleListData, error) {
	offset, limit, cur := pageOffset(q.Current, q.PageSize, 50)
	enabledOnly := q.Enabled != nil && *q.Enabled
	list, total, err := s.Store.ListSqlInsightRules(ctx, q.MetricKey, enabledOnly, offset, limit)
	if err != nil {
		return nil, err
	}
	items := make([]*RuleItem, 0, len(list))
	for _, r := range list {
		items = append(items, ruleToDTO(r))
	}
	return &RuleListData{List: items, Pagination: Pagination{Total: total, Current: cur, PageSize: limit}}, nil
}
