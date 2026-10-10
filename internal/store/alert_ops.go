package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AlertInhibition 抑制：源告警 firing 时压制目标告警外发（仍落库）。
type AlertInhibition struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SourceMatchers string `json:"source_matchers"` // k=v,k2=v2
	TargetMatchers string `json:"target_matchers"`
	EqualLabels    string `json:"equal_labels"` // 逗号分隔 label 名，源与目标须相等
	Enabled        bool   `json:"enabled"`
	CreatedBy      string `json:"created_by"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// AlertRoute 通知路由：命中 matchers 时使用指定渠道与分组键。
type AlertRoute struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Matchers   string   `json:"matchers"`
	Channels   []string `json:"channels"` // webhook|wecom|phone|email
	GroupBy    []string `json:"group_by"`
	Continue   bool     `json:"continue"` // 命中后是否继续匹配后续路由
	Priority   int      `json:"priority"` // 越小越先
	Enabled    bool     `json:"enabled"`
	CreatedBy  string   `json:"created_by"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}

// AlertOncallSlot 值班槽：按星期几（0=周日）与时间窗。
type AlertOncallSlot struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Weekday   int      `json:"weekday"` // 0-6
	StartHM   string   `json:"start_hm"` // HH:MM
	EndHM     string   `json:"end_hm"`
	Usernames []string `json:"usernames"`
	Enabled   bool     `json:"enabled"`
	CreatedBy string   `json:"created_by"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func (d *DB) ensureAlertOpsTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_alert_inhibitions (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			source_matchers TEXT NOT NULL DEFAULT '',
			target_matchers TEXT NOT NULL DEFAULT '',
			equal_labels TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_alert_routes (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			matchers TEXT NOT NULL DEFAULT '',
			channels_json TEXT NOT NULL DEFAULT '[]',
			group_by_json TEXT NOT NULL DEFAULT '[]',
			cont INTEGER NOT NULL DEFAULT 0,
			priority INTEGER NOT NULL DEFAULT 100,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_alert_oncall (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			weekday INTEGER NOT NULL DEFAULT 0,
			start_hm TEXT NOT NULL DEFAULT '00:00',
			end_hm TEXT NOT NULL DEFAULT '23:59',
			usernames_json TEXT NOT NULL DEFAULT '[]',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// --- Inhibition ---

func (d *DB) ListAlertInhibitions() ([]AlertInhibition, error) {
	rows, err := d.Query(`SELECT id, name, source_matchers, target_matchers, equal_labels, enabled, created_by, created_at, updated_at
		FROM tbl_alert_inhibitions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertInhibition
	for rows.Next() {
		var r AlertInhibition
		var en int
		if err := rows.Scan(&r.ID, &r.Name, &r.SourceMatchers, &r.TargetMatchers, &r.EqualLabels, &en, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = en == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) ListEnabledAlertInhibitions() ([]AlertInhibition, error) {
	all, err := d.ListAlertInhibitions()
	if err != nil {
		return nil, err
	}
	out := make([]AlertInhibition, 0, len(all))
	for _, r := range all {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}

func (d *DB) GetAlertInhibition(id string) (*AlertInhibition, error) {
	row := d.QueryRow(`SELECT id, name, source_matchers, target_matchers, equal_labels, enabled, created_by, created_at, updated_at
		FROM tbl_alert_inhibitions WHERE id=?`, id)
	var r AlertInhibition
	var en int
	if err := row.Scan(&r.ID, &r.Name, &r.SourceMatchers, &r.TargetMatchers, &r.EqualLabels, &en, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.Enabled = en == 1
	return &r, nil
}

func (d *DB) CreateAlertInhibition(r *AlertInhibition) error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("请填写名称")
	}
	if strings.TrimSpace(r.SourceMatchers) == "" || strings.TrimSpace(r.TargetMatchers) == "" {
		return errors.New("请填写源/目标 matchers")
	}
	now := nowRFC3339(time.Now())
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt, r.UpdatedAt = now, now
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_alert_inhibitions (id, name, source_matchers, target_matchers, equal_labels, enabled, created_by, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.SourceMatchers, r.TargetMatchers, r.EqualLabels, en, r.CreatedBy, r.CreatedAt, r.UpdatedAt)
	return err
}

func (d *DB) SaveAlertInhibition(r *AlertInhibition) error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("请填写名称")
	}
	r.UpdatedAt = nowRFC3339(time.Now())
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := d.Exec(`UPDATE tbl_alert_inhibitions SET name=?, source_matchers=?, target_matchers=?, equal_labels=?, enabled=?, updated_at=? WHERE id=?`,
		r.Name, r.SourceMatchers, r.TargetMatchers, r.EqualLabels, en, r.UpdatedAt, r.ID)
	return err
}

func (d *DB) DeleteAlertInhibition(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_alert_inhibitions WHERE id=?`, id)
	return err
}

// AlertInhibitedBy 若目标告警被任一源告警抑制则返回 true。
func AlertInhibitedBy(target *Alert, sources []Alert, rules []AlertInhibition) bool {
	if target == nil || len(rules) == 0 || len(sources) == 0 {
		return false
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		tm := ParseAlertMatchers(rule.TargetMatchers)
		if !MatchersMatchLabels(tm, target.Labels) {
			continue
		}
		sm := ParseAlertMatchers(rule.SourceMatchers)
		equals := splitCSV(rule.EqualLabels)
		for _, src := range sources {
			if src.ID == target.ID || src.Status != AlertStatusFiring {
				continue
			}
			if !MatchersMatchLabels(sm, src.Labels) {
				continue
			}
			ok := true
			for _, k := range equals {
				if src.Labels[k] != target.Labels[k] {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
	}
	return false
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- Routes ---

func (d *DB) ListAlertRoutes() ([]AlertRoute, error) {
	rows, err := d.Query(`SELECT id, name, matchers, channels_json, group_by_json, cont, priority, enabled, created_by, created_at, updated_at
		FROM tbl_alert_routes ORDER BY priority ASC, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRoute
	for rows.Next() {
		var r AlertRoute
		var ch, gb string
		var cont, en int
		if err := rows.Scan(&r.ID, &r.Name, &r.Matchers, &ch, &gb, &cont, &r.Priority, &en, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Channels = decodeStringSlice(ch)
		r.GroupBy = decodeStringSlice(gb)
		r.Continue = cont == 1
		r.Enabled = en == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) ListEnabledAlertRoutes() ([]AlertRoute, error) {
	all, err := d.ListAlertRoutes()
	if err != nil {
		return nil, err
	}
	out := make([]AlertRoute, 0, len(all))
	for _, r := range all {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}

func (d *DB) GetAlertRoute(id string) (*AlertRoute, error) {
	row := d.QueryRow(`SELECT id, name, matchers, channels_json, group_by_json, cont, priority, enabled, created_by, created_at, updated_at
		FROM tbl_alert_routes WHERE id=?`, id)
	var r AlertRoute
	var ch, gb string
	var cont, en int
	if err := row.Scan(&r.ID, &r.Name, &r.Matchers, &ch, &gb, &cont, &r.Priority, &en, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.Channels = decodeStringSlice(ch)
	r.GroupBy = decodeStringSlice(gb)
	r.Continue = cont == 1
	r.Enabled = en == 1
	return &r, nil
}

func (d *DB) CreateAlertRoute(r *AlertRoute) error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("请填写名称")
	}
	if len(r.Channels) == 0 {
		return errors.New("请至少选择一个渠道")
	}
	now := nowRFC3339(time.Now())
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt, r.UpdatedAt = now, now
	en, cont := 0, 0
	if r.Enabled {
		en = 1
	}
	if r.Continue {
		cont = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_alert_routes (id, name, matchers, channels_json, group_by_json, cont, priority, enabled, created_by, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.Matchers, encodeJSON(r.Channels), encodeJSON(r.GroupBy),
		cont, r.Priority, en, r.CreatedBy, r.CreatedAt, r.UpdatedAt)
	return err
}

func (d *DB) SaveAlertRoute(r *AlertRoute) error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("请填写名称")
	}
	r.UpdatedAt = nowRFC3339(time.Now())
	en, cont := 0, 0
	if r.Enabled {
		en = 1
	}
	if r.Continue {
		cont = 1
	}
	_, err := d.Exec(`UPDATE tbl_alert_routes SET name=?, matchers=?, channels_json=?, group_by_json=?, cont=?, priority=?, enabled=?, updated_at=? WHERE id=?`,
		r.Name, r.Matchers, encodeJSON(r.Channels), encodeJSON(r.GroupBy), cont, r.Priority, en, r.UpdatedAt, r.ID)
	return err
}

func (d *DB) DeleteAlertRoute(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_alert_routes WHERE id=?`, id)
	return err
}

// MatchAlertRoutes 按 priority 返回命中路由（考虑 continue）。
func MatchAlertRoutes(labels map[string]string, routes []AlertRoute) []AlertRoute {
	var hit []AlertRoute
	for _, r := range routes {
		if !r.Enabled {
			continue
		}
		m := ParseAlertMatchers(r.Matchers)
		if len(m) > 0 && !MatchersMatchLabels(m, labels) {
			continue
		}
		// 空 matchers = 默认路由
		hit = append(hit, r)
		if !r.Continue {
			break
		}
	}
	return hit
}

// AlertGroupKey 按 group_by 标签生成分组键。
func AlertGroupKey(labels map[string]string, groupBy []string) string {
	if len(groupBy) == 0 {
		return ""
	}
	parts := make([]string, 0, len(groupBy))
	for _, k := range groupBy {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, "|")
}

// MergeRouteChannels 合并多条路由渠道（去重保序）。
func MergeRouteChannels(routes []AlertRoute) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range routes {
		for _, c := range r.Channels {
			c = strings.ToLower(strings.TrimSpace(c))
			if c == "" || seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// --- Oncall ---

func (d *DB) ListAlertOncall() ([]AlertOncallSlot, error) {
	rows, err := d.Query(`SELECT id, name, weekday, start_hm, end_hm, usernames_json, enabled, created_by, created_at, updated_at
		FROM tbl_alert_oncall ORDER BY weekday ASC, start_hm ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertOncallSlot
	for rows.Next() {
		var r AlertOncallSlot
		var uj string
		var en int
		if err := rows.Scan(&r.ID, &r.Name, &r.Weekday, &r.StartHM, &r.EndHM, &uj, &en, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Usernames = decodeStringSlice(uj)
		r.Enabled = en == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) GetAlertOncall(id string) (*AlertOncallSlot, error) {
	row := d.QueryRow(`SELECT id, name, weekday, start_hm, end_hm, usernames_json, enabled, created_by, created_at, updated_at
		FROM tbl_alert_oncall WHERE id=?`, id)
	var r AlertOncallSlot
	var uj string
	var en int
	if err := row.Scan(&r.ID, &r.Name, &r.Weekday, &r.StartHM, &r.EndHM, &uj, &en, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.Usernames = decodeStringSlice(uj)
	r.Enabled = en == 1
	return &r, nil
}

func (d *DB) CreateAlertOncall(r *AlertOncallSlot) error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("请填写名称")
	}
	if r.Weekday < 0 || r.Weekday > 6 {
		return errors.New("weekday 须为 0-6")
	}
	if len(r.Usernames) == 0 {
		return errors.New("请指定值班人")
	}
	if _, err := parseHM(r.StartHM); err != nil {
		return fmt.Errorf("开始时间无效：%w", err)
	}
	if _, err := parseHM(r.EndHM); err != nil {
		return fmt.Errorf("结束时间无效：%w", err)
	}
	now := nowRFC3339(time.Now())
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt, r.UpdatedAt = now, now
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_alert_oncall (id, name, weekday, start_hm, end_hm, usernames_json, enabled, created_by, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.Weekday, r.StartHM, r.EndHM, encodeJSON(r.Usernames), en, r.CreatedBy, r.CreatedAt, r.UpdatedAt)
	return err
}

func (d *DB) SaveAlertOncall(r *AlertOncallSlot) error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("请填写名称")
	}
	r.UpdatedAt = nowRFC3339(time.Now())
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := d.Exec(`UPDATE tbl_alert_oncall SET name=?, weekday=?, start_hm=?, end_hm=?, usernames_json=?, enabled=?, updated_at=? WHERE id=?`,
		r.Name, r.Weekday, r.StartHM, r.EndHM, encodeJSON(r.Usernames), en, r.UpdatedAt, r.ID)
	return err
}

func (d *DB) DeleteAlertOncall(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_alert_oncall WHERE id=?`, id)
	return err
}

func parseHM(s string) (int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, errors.New("格式须为 HH:MM")
	}
	var h, m int
	if _, err := fmt.Sscanf(parts[0]+":"+parts[1], "%d:%d", &h, &m); err != nil {
		return 0, err
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, errors.New("时间超出范围")
	}
	return h*60 + m, nil
}

// CurrentAlertOncallUsernames 返回当前时刻值班用户名。
func (d *DB) CurrentAlertOncallUsernames(now time.Time) ([]string, error) {
	slots, err := d.ListAlertOncall()
	if err != nil {
		return nil, err
	}
	wd := int(now.Weekday())
	mins := now.Hour()*60 + now.Minute()
	seen := map[string]bool{}
	var out []string
	for _, s := range slots {
		if !s.Enabled || s.Weekday != wd {
			continue
		}
		start, err1 := parseHM(s.StartHM)
		end, err2 := parseHM(s.EndHM)
		if err1 != nil || err2 != nil {
			continue
		}
		in := false
		if start <= end {
			in = mins >= start && mins <= end
		} else {
			// 跨午夜
			in = mins >= start || mins <= end
		}
		if !in {
			continue
		}
		for _, u := range s.Usernames {
			u = strings.TrimSpace(u)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, u)
		}
	}
	return out, nil
}

// BuildAlertPostmortem 生成复盘 Markdown。
func (d *DB) BuildAlertPostmortem(since, until string) (string, error) {
	f := AlertFilter{Tab: "history", Since: since}
	list, err := d.ListAlerts(f)
	if err != nil {
		return "", err
	}
	untilT := time.Time{}
	if strings.TrimSpace(until) != "" {
		untilT, _ = time.Parse(time.RFC3339, until)
	}
	var b strings.Builder
	b.WriteString("# 告警复盘报告\n\n")
	b.WriteString(fmt.Sprintf("- 生成时间：%s\n", time.Now().Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("- 范围：%s ~ %s\n\n", nzStr(since, "不限"), nzStr(until, "现在")))
	bySev := map[string]int{}
	bySvc := map[string]int{}
	filtered := make([]Alert, 0, len(list))
	for _, a := range list {
		if !untilT.IsZero() && a.ResolvedAt != "" {
			if t, err := time.Parse(time.RFC3339, a.ResolvedAt); err == nil && t.After(untilT) {
				continue
			}
		}
		filtered = append(filtered, a)
		bySev[a.Severity]++
		svc := a.Service
		if svc == "" {
			svc = "(unknown)"
		}
		bySvc[svc]++
	}
	b.WriteString(fmt.Sprintf("## 概览\n\n共 **%d** 条已恢复告警。\n\n### 按级别\n\n", len(filtered)))
	for _, s := range []string{AlertSeverityP0, AlertSeverityP1, AlertSeverityP2, AlertSeverityP3} {
		if n := bySev[s]; n > 0 {
			b.WriteString(fmt.Sprintf("- %s: %d\n", s, n))
		}
	}
	b.WriteString("\n### 按服务\n\n")
	for svc, n := range bySvc {
		b.WriteString(fmt.Sprintf("- %s: %d\n", svc, n))
	}
	b.WriteString("\n## 明细\n\n| 级别 | 标题 | 服务 | 开始 | 恢复 | 负责人 |\n|------|------|------|------|------|--------|\n")
	for _, a := range filtered {
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
			a.Severity, escapeMD(a.Title), nzStr(a.Service, "-"),
			shortTime(a.StartedAt), shortTime(a.ResolvedAt), nzStr(a.Owner, "-")))
	}
	return b.String(), nil
}

func nzStr(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func escapeMD(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "/"), "\n", " ")
}

func shortTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format("01-02 15:04")
}
