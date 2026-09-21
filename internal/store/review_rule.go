package store

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	GlobalArgGoInceptionVariables = "ticket_goinception_variables"
	GlobalArgTicketReviewRules    = "ticket_review_rules"

	descGoInceptionVariables = "MySQL 审核规则（goInception）：JSON [{name,value,label}]"
	descTicketReviewRules    = "PG/StarRocks 工单审核规则：JSON {postgres:{ddl,dml},starrocks:{ddl,dml}}"
)

type reviewRuleSeed struct {
	Engine, Category, Name, Label, Value string
}

type reviewCatalogItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

type reviewCategoryBucket struct {
	DDL []reviewCatalogItem `json:"ddl"`
	DML []reviewCatalogItem `json:"dml"`
}

type reviewPartitioned struct {
	Postgres  []reviewCatalogItem
	StarRocks []reviewCatalogItem
}

func NormalizeReviewEngine(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "mysql", "tidb":
		return "mysql"
	case "postgres", "postgresql", "pg":
		return "postgres"
	case "starrocks", "sr":
		return "starrocks"
	default:
		return ""
	}
}

func ReviewRuleCategory(name, fallback string) string {
	if strings.EqualFold(strings.TrimSpace(fallback), "dml") || strings.EqualFold(strings.TrimSpace(fallback), "ddl") {
		return strings.ToLower(strings.TrimSpace(fallback))
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "dml") {
		return "dml"
	}
	return "ddl"
}

// DefaultTicketReviewRules 返回内置目录中指定引擎的规则，与启动时 merge 进
// ticket_review_rules 的缺省项一致（不含库内用户改过的值）。
func DefaultTicketReviewRules(engine string) []ReviewRule {
	engine = NormalizeReviewEngine(engine)
	if engine == "" {
		return nil
	}
	out := make([]ReviewRule, 0, 64)
	for _, it := range defaultReviewRules() {
		if it.Engine != engine {
			continue
		}
		out = append(out, ReviewRule{
			Engine:   it.Engine,
			Category: ReviewRuleCategory(it.Name, it.Category),
			Name:     it.Name,
			Label:    it.Label,
			Value:    it.Value,
		})
	}
	return out
}

func (d *DB) ListReviewRules(engine string) ([]ReviewRule, error) {
	engine = NormalizeReviewEngine(engine)
	if engine == "" {
		return nil, errors.New("engine 须为 mysql / postgres / starrocks")
	}
	if err := d.ensureReviewRulesDefaults(); err != nil {
		return nil, err
	}
	if engine == "mysql" {
		return d.loadGoInceptionVariables()
	}
	return d.loadTicketReviewRules(engine)
}

func (d *DB) CreateReviewRule(engine, category, name, label, value string) (*ReviewRule, error) {
	engine = NormalizeReviewEngine(engine)
	name = strings.TrimSpace(name)
	label = strings.TrimSpace(label)
	value = strings.TrimSpace(value)
	if engine == "" {
		return nil, errors.New("engine 须为 mysql / postgres / starrocks")
	}
	if name == "" {
		return nil, errors.New("参数名必填")
	}
	if label == "" {
		return nil, errors.New("中文名必填")
	}
	if value == "" {
		return nil, errors.New("当前值必填")
	}
	list, err := d.ListReviewRules(engine)
	if err != nil {
		return nil, err
	}
	for _, it := range list {
		if strings.EqualFold(it.Name, name) {
			return nil, errors.New("该参数已在列表中")
		}
	}
	if engine != "mysql" {
		category = ReviewRuleCategory(name, category)
	} else {
		category = ""
	}
	it := ReviewRule{Engine: engine, Category: category, Name: name, Label: label, Value: value}
	list = append(list, it)
	if err := d.saveReviewRules(engine, list); err != nil {
		return nil, err
	}
	d.syncReviewLinkedArgs(name, value)
	return &it, nil
}

func (d *DB) UpdateReviewRules(engine string, items []ReviewRule) error {
	engine = NormalizeReviewEngine(engine)
	if engine == "" {
		return errors.New("engine 须为 mysql / postgres / starrocks")
	}
	list, err := d.ListReviewRules(engine)
	if err != nil {
		return err
	}
	byName := map[string]int{}
	for i, it := range list {
		byName[strings.ToLower(it.Name)] = i
	}
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		idx, ok := byName[strings.ToLower(name)]
		if !ok {
			return errors.New("参数不存在: " + name)
		}
		list[idx].Value = item.Value
		if strings.TrimSpace(item.Label) != "" {
			list[idx].Label = strings.TrimSpace(item.Label)
		}
		d.syncReviewLinkedArgs(name, item.Value)
	}
	return d.saveReviewRules(engine, list)
}

func (d *DB) loadGoInceptionVariables() ([]ReviewRule, error) {
	raw, err := d.GetGlobalArg(GlobalArgGoInceptionVariables)
	if err != nil {
		return nil, err
	}
	items, err := parseGoInceptionCatalog(raw)
	if err != nil {
		return nil, err
	}
	out := make([]ReviewRule, 0, len(items))
	for _, it := range items {
		out = append(out, ReviewRule{Engine: "mysql", Name: it.Name, Label: it.Label, Value: it.Value})
	}
	return out, nil
}

func (d *DB) loadTicketReviewRules(engine string) ([]ReviewRule, error) {
	raw, err := d.GetGlobalArg(GlobalArgTicketReviewRules)
	if err != nil {
		return nil, err
	}
	p, err := parseTicketReviewPartitioned(raw)
	if err != nil {
		return nil, err
	}
	src := p.Postgres
	if engine == "starrocks" {
		src = p.StarRocks
	}
	out := make([]ReviewRule, 0, len(src))
	for _, it := range src {
		cat := ReviewRuleCategory(it.Name, "")
		out = append(out, ReviewRule{Engine: engine, Category: cat, Name: it.Name, Label: it.Label, Value: it.Value})
	}
	return out, nil
}

func (d *DB) saveReviewRules(engine string, list []ReviewRule) error {
	if engine == "mysql" {
		items := make([]reviewCatalogItem, 0, len(list))
		for _, it := range list {
			items = append(items, reviewCatalogItem{Name: it.Name, Value: it.Value, Label: it.Label})
		}
		b, err := json.Marshal(items)
		if err != nil {
			return err
		}
		return d.upsertGlobalArgValue(GlobalArgGoInceptionVariables, string(b), descGoInceptionVariables)
	}
	raw, err := d.GetGlobalArg(GlobalArgTicketReviewRules)
	if err != nil {
		return err
	}
	p, err := parseTicketReviewPartitioned(raw)
	if err != nil {
		p = &reviewPartitioned{}
	}
	items := make([]reviewCatalogItem, 0, len(list))
	for _, it := range list {
		items = append(items, reviewCatalogItem{Name: it.Name, Value: it.Value, Label: it.Label})
	}
	if engine == "starrocks" {
		p.StarRocks = items
	} else {
		p.Postgres = items
	}
	b, err := marshalTicketReviewPartitioned(p)
	if err != nil {
		return err
	}
	return d.upsertGlobalArgValue(GlobalArgTicketReviewRules, string(b), descTicketReviewRules)
}

func (d *DB) upsertGlobalArgValue(key, value, desc string) error {
	cur, err := d.GetGlobalArg(key)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cur) == "" && strings.TrimSpace(value) == "" {
		return d.SetGlobalArg(key, value, desc)
	}
	var existDesc string
	_ = d.QueryRow(`SELECT description FROM tbl_global_args WHERE key=?`, key).Scan(&existDesc)
	if strings.TrimSpace(existDesc) == "" {
		existDesc = desc
	}
	return d.SetGlobalArg(key, value, existDesc)
}

func (d *DB) ensureReviewRulesDefaults() error {
	if err := d.ensureGlobalArgJSON(GlobalArgGoInceptionVariables, descGoInceptionVariables, func() (string, error) {
		if migrated, err := d.catalogJSONFromTable("mysql"); err == nil && migrated != "" {
			return migrated, nil
		}
		return defaultGoInceptionVariablesJSON()
	}); err != nil {
		return err
	}
	if err := d.ensureGlobalArgJSON(GlobalArgTicketReviewRules, descTicketReviewRules, func() (string, error) {
		if migrated, err := d.ticketReviewJSONFromTable(); err == nil && migrated != "" {
			return migrated, nil
		}
		return defaultTicketReviewRulesJSON()
	}); err != nil {
		return err
	}
	return d.mergeMissingTicketReviewRules()
}

func (d *DB) mergeMissingTicketReviewRules() error {
	raw, err := d.GetGlobalArg(GlobalArgTicketReviewRules)
	if err != nil {
		return err
	}
	p, err := parseTicketReviewPartitioned(raw)
	if err != nil || p == nil {
		return err
	}
	defs := defaultReviewItemsByEngine()
	pg, pgChanged := mergeReviewCatalog(p.Postgres, defs["postgres"])
	sr, srChanged := mergeReviewCatalog(p.StarRocks, defs["starrocks"])
	if !pgChanged && !srChanged {
		return nil
	}
	p.Postgres, p.StarRocks = pg, sr
	b, err := marshalTicketReviewPartitioned(p)
	if err != nil {
		return err
	}
	return d.upsertGlobalArgValue(GlobalArgTicketReviewRules, string(b), descTicketReviewRules)
}

func defaultReviewItemsByEngine() map[string][]reviewCatalogItem {
	out := map[string][]reviewCatalogItem{"postgres": {}, "starrocks": {}}
	for _, it := range defaultReviewRules() {
		if it.Engine != "postgres" && it.Engine != "starrocks" {
			continue
		}
		out[it.Engine] = append(out[it.Engine], reviewCatalogItem{Name: it.Name, Value: it.Value, Label: it.Label})
	}
	return out
}

func mergeReviewCatalog(have, defs []reviewCatalogItem) ([]reviewCatalogItem, bool) {
	seen := map[string]bool{}
	for _, it := range have {
		seen[strings.ToLower(strings.TrimSpace(it.Name))] = true
	}
	out := append([]reviewCatalogItem{}, have...)
	changed := false
	for _, it := range defs {
		if seen[strings.ToLower(strings.TrimSpace(it.Name))] {
			continue
		}
		out = append(out, it)
		changed = true
	}
	return out, changed
}

func (d *DB) ensureGlobalArgJSON(key, desc string, build func() (string, error)) error {
	raw, err := d.GetGlobalArg(key)
	if err != nil {
		return err
	}
	if strings.TrimSpace(raw) != "" {
		return nil
	}
	val, err := build()
	if err != nil {
		return err
	}
	return d.SetGlobalArg(key, val, desc)
}

func (d *DB) catalogJSONFromTable(engine string) (string, error) {
	rows, err := d.Query(
		`SELECT name, label, value FROM tbl_review_rules WHERE engine=? ORDER BY sort_order, name`,
		engine,
	)
	if err != nil {
		return "", nil
	}
	defer rows.Close()
	items := []reviewCatalogItem{}
	for rows.Next() {
		var it reviewCatalogItem
		if err := rows.Scan(&it.Name, &it.Label, &it.Value); err != nil {
			return "", err
		}
		if strings.TrimSpace(it.Name) == "" {
			continue
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return "", nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (d *DB) ticketReviewJSONFromTable() (string, error) {
	pg, err := d.listTableCatalog("postgres")
	if err != nil {
		return "", nil
	}
	sr, err := d.listTableCatalog("starrocks")
	if err != nil {
		return "", nil
	}
	if len(pg) == 0 && len(sr) == 0 {
		return "", nil
	}
	b, err := marshalTicketReviewPartitioned(&reviewPartitioned{Postgres: pg, StarRocks: sr})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (d *DB) listTableCatalog(engine string) ([]reviewCatalogItem, error) {
	rows, err := d.Query(
		`SELECT name, label, value FROM tbl_review_rules WHERE engine=? ORDER BY sort_order, name`,
		engine,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []reviewCatalogItem{}
	for rows.Next() {
		var it reviewCatalogItem
		if err := rows.Scan(&it.Name, &it.Label, &it.Value); err != nil {
			return nil, err
		}
		if strings.TrimSpace(it.Name) == "" {
			continue
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func parseGoInceptionCatalog(raw string) ([]reviewCatalogItem, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []reviewCatalogItem{}, nil
	}
	items, err := parseReviewItemList([]byte(raw))
	if err != nil {
		return nil, errors.New("ticket_goinception_variables JSON 无效: " + err.Error())
	}
	return dedupeReviewItems(items), nil
}

func parseTicketReviewPartitioned(raw string) (*reviewPartitioned, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &reviewPartitioned{}, nil
	}
	if strings.HasPrefix(raw, "[") {
		items, err := parseReviewItemList([]byte(raw))
		if err != nil {
			return nil, errors.New("ticket_review_rules JSON 无效: " + err.Error())
		}
		items = dedupeReviewItems(items)
		return &reviewPartitioned{Postgres: cloneReviewItems(items), StarRocks: cloneReviewItems(items)}, nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return nil, errors.New("ticket_review_rules JSON 无效: " + err.Error())
	}
	if rawRules, ok := probe["rules"]; ok && !hasPartitionKeys(probe) {
		items, err := parseReviewItemList(rawRules)
		if err != nil {
			return nil, errors.New("ticket_review_rules JSON 无效: " + err.Error())
		}
		items = dedupeReviewItems(items)
		return &reviewPartitioned{Postgres: cloneReviewItems(items), StarRocks: cloneReviewItems(items)}, nil
	}
	p := &reviewPartitioned{}
	if v, ok := firstRaw(probe, "postgres", "postgresql", "pg"); ok {
		items, err := parseDBTypeBucket(v)
		if err != nil {
			return nil, err
		}
		p.Postgres = items
	}
	if v, ok := firstRaw(probe, "starrocks", "sr"); ok {
		items, err := parseDBTypeBucket(v)
		if err != nil {
			return nil, err
		}
		p.StarRocks = items
	}
	return p, nil
}

func hasPartitionKeys(probe map[string]json.RawMessage) bool {
	_, pg := probe["postgres"]
	_, postgresql := probe["postgresql"]
	_, sr := probe["starrocks"]
	return pg || postgresql || sr
}

func firstRaw(probe map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, k := range keys {
		if v, ok := probe[k]; ok {
			return v, true
		}
	}
	return nil, false
}

func parseDBTypeBucket(raw json.RawMessage) ([]reviewCatalogItem, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) == nil && (len(probe["ddl"]) > 0 || len(probe["dml"]) > 0 || hasKey(probe, "ddl") || hasKey(probe, "dml")) {
		ddl, err := parseReviewItemList(probe["ddl"])
		if err != nil {
			return nil, errors.New("ticket_review_rules JSON 无效: " + err.Error())
		}
		dml, err := parseReviewItemList(probe["dml"])
		if err != nil {
			return nil, errors.New("ticket_review_rules JSON 无效: " + err.Error())
		}
		return dedupeReviewItems(append(ddl, dml...)), nil
	}
	items, err := parseReviewItemList(raw)
	if err != nil {
		return nil, errors.New("ticket_review_rules JSON 无效: " + err.Error())
	}
	return dedupeReviewItems(items), nil
}

func hasKey(m map[string]json.RawMessage, key string) bool {
	_, ok := m[key]
	return ok
}

func parseReviewItemList(raw json.RawMessage) ([]reviewCatalogItem, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '[' {
		var arr []map[string]any
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, err
		}
		out := make([]reviewCatalogItem, 0, len(arr))
		for _, o := range arr {
			if it, ok := mapToReviewItem(o, ""); ok {
				out = append(out, it)
			}
		}
		return out, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if nested, ok := obj["items"].([]any); ok {
		return parseReviewItemList(mustJSON(nested))
	}
	if nested, ok := obj["list"].([]any); ok {
		return parseReviewItemList(mustJSON(nested))
	}
	out := make([]reviewCatalogItem, 0, len(obj))
	for name, v := range obj {
		switch t := v.(type) {
		case map[string]any:
			if it, ok := mapToReviewItem(t, name); ok {
				out = append(out, it)
			}
		default:
			out = append(out, reviewCatalogItem{Name: name, Value: asReviewString(v)})
		}
	}
	return out, nil
}

func mapToReviewItem(o map[string]any, fallbackName string) (reviewCatalogItem, bool) {
	name := firstString(o, "name", "Name", "id", "Id")
	if name == "" {
		name = strings.TrimSpace(fallbackName)
	}
	if name == "" {
		return reviewCatalogItem{}, false
	}
	label := firstString(o, "label", "Label", "name_zh", "nameZh", "comment", "Comment")
	value := firstString(o, "value", "Value")
	if value == "" {
		if payload, ok := o["payload"]; ok && payload != nil {
			value = asReviewString(payload)
		}
	}
	if value == "" {
		value = firstString(o, "enabled", "Enabled")
	}
	return reviewCatalogItem{Name: name, Value: value, Label: label}, true
}

func firstString(o map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := o[k]; ok {
			s := strings.TrimSpace(asReviewString(v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func dedupeReviewItems(items []reviewCatalogItem) []reviewCatalogItem {
	seen := map[string]struct{}{}
	out := make([]reviewCatalogItem, 0, len(items))
	for _, it := range items {
		name := strings.TrimSpace(it.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, reviewCatalogItem{Name: name, Value: strings.TrimSpace(it.Value), Label: strings.TrimSpace(it.Label)})
	}
	return out
}

func cloneReviewItems(in []reviewCatalogItem) []reviewCatalogItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]reviewCatalogItem, len(in))
	copy(out, in)
	return out
}

func marshalTicketReviewPartitioned(p *reviewPartitioned) ([]byte, error) {
	if p == nil {
		p = &reviewPartitioned{}
	}
	doc := map[string]reviewCategoryBucket{
		"postgres":  groupReviewItems(p.Postgres),
		"starrocks": groupReviewItems(p.StarRocks),
	}
	return json.Marshal(doc)
}

func groupReviewItems(items []reviewCatalogItem) reviewCategoryBucket {
	out := reviewCategoryBucket{DDL: []reviewCatalogItem{}, DML: []reviewCatalogItem{}}
	for _, it := range items {
		if ReviewRuleCategory(it.Name, "") == "dml" {
			out.DML = append(out.DML, it)
			continue
		}
		out.DDL = append(out.DDL, it)
	}
	return out
}

func defaultGoInceptionVariablesJSON() (string, error) {
	items := []reviewCatalogItem{}
	for _, it := range defaultReviewRules() {
		if it.Engine != "mysql" {
			continue
		}
		items = append(items, reviewCatalogItem{Name: it.Name, Value: it.Value, Label: it.Label})
	}
	b, err := json.Marshal(items)
	return string(b), err
}

func defaultTicketReviewRulesJSON() (string, error) {
	p := &reviewPartitioned{}
	for _, it := range defaultReviewRules() {
		item := reviewCatalogItem{Name: it.Name, Value: it.Value, Label: it.Label}
		switch it.Engine {
		case "postgres":
			p.Postgres = append(p.Postgres, item)
		case "starrocks":
			p.StarRocks = append(p.StarRocks, item)
		}
	}
	b, err := marshalTicketReviewPartitioned(p)
	return string(b), err
}

func defaultReviewRules() []reviewRuleSeed {
	mysql := []reviewRuleSeed{
		{Name: "check_dml_where", Label: "检查 DML WHERE 条件", Value: "true"},
		{Name: "check_dml_limit", Label: "检查 DML LIMIT", Value: "false"},
		{Name: "check_dml_orderby", Label: "检查 DML ORDER BY", Value: "false"},
		{Name: "check_insert_field", Label: "检查 INSERT 列清单", Value: "false"},
		{Name: "max_update_rows", Label: "单条 UPDATE 最大行数", Value: "10000"},
		{Name: "enable_blob_type", Label: "允许 BLOB 类型", Value: "true"},
		{Name: "enable_timestamp_type", Label: "允许 TIMESTAMP", Value: "true"},
		{Name: "enable_identifer_keyword", Label: "允许关键字作标识符", Value: "false"},
		{Name: "check_table_comment", Label: "检查表注释", Value: "false"},
		{Name: "check_column_comment", Label: "检查列注释", Value: "false"},
		{Name: "check_primary_key", Label: "检查主键", Value: "false"},
		{Name: "enable_nullable", Label: "允许空值", Value: "true"},
		{Name: "enable_drop_table", Label: "允许 DROP TABLE", Value: "true"},
		{Name: "disable_drop_database", Label: "禁止 DROP DATABASE", Value: "true"},
		{Name: "enable_set_engine", Label: "允许指定引擎", Value: "true"},
		{Name: "max_char_length", Label: "CHAR 最大长度", Value: "255"},
		{Name: "max_varchar_length", Label: "VARCHAR 最大长度", Value: "16383"},
		{Name: "max_key_parts", Label: "索引最大列数", Value: "5"},
		{Name: "check_identifier", Label: "检查标识符规范", Value: "false"},
		{Name: "enable_select_star", Label: "允许 SELECT *", Value: "true"},
		{Name: "check_index_prefix", Label: "检查索引前缀", Value: "false"},
		{Name: "enable_partition_table", Label: "允许分区表", Value: "true"},
	}
	// 对齐 hub reviewrule/catalog.go：PG/SR 工单 DDL/DML 可配置目录。
	type pgsrSeed struct {
		reviewRuleSeed
		Engines []string
	}
	both := []string{"postgres", "starrocks"}
	pgOnly := []string{"postgres"}
	srOnly := []string{"starrocks"}
	pgsr := []pgsrSeed{
		{reviewRuleSeed{Category: "ddl", Name: "DDLCheckColumnComment", Label: "列必须有注释", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLCheckTableComment", Label: "表必须有注释", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLCheckColumnNullable", Label: "列必须 NOT NULL", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLCheckColumnDefault", Label: "建表列必须有 DEFAULT", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLCheckPrimaryKeyExists", Label: "建表必须有主键", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLEnablePrimaryKey", Label: "建表必须声明 Key", Value: "true"}, srOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLForbidCreateDatabase", Label: "是否允许 CREATE DATABASE", Value: `{"allow":false}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLForbidDropDatabase", Label: "是否允许 DROP DATABASE", Value: `{"allow":false}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLForbidDropTable", Label: "禁止或限制 DROP TABLE", Value: `{"mode":"forbid","suffix":"_del"}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLIndexNameSpec", Label: "索引命名规范（idx_/uk_ 前缀）", Value: "false"}, both},
		{reviewRuleSeed{Category: "dml", Name: "DMLWhere", Label: "UPDATE/DELETE 必须有 WHERE，且禁止恒真条件", Value: `{"tautologies":["1=1","true"]}`}, both},
		{reviewRuleSeed{Category: "dml", Name: "DMLInsertMustSpecifyColumn", Label: "INSERT 必须指定列清单", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLForbidTruncate", Label: "禁止 TRUNCATE", Value: `{"allow":false}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLForbidDropSchema", Label: "禁止 DROP SCHEMA", Value: `{"allow":false}`}, pgOnly},
		{reviewRuleSeed{Category: "dml", Name: "DMLForbidDDL", Label: "DML 工单禁止提交 DDL", Value: "true"}, both},
		{reviewRuleSeed{Category: "dml", Name: "DMLUpdateSetAnd", Label: "UPDATE SET 禁止用 AND 连接赋值", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLMaxColumnCount", Label: "建表最大列数", Value: `{"max_columns":64}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLTableNameSpec", Label: "表名长度与大小写", Value: `{"max_len":63,"case":"any"}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLTableNamePrefix", Label: "表名必须以指定前缀开头", Value: `{"prefix":"tbl_"}`}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLDatabaseNamePrefix", Label: "库名必须以指定前缀开头", Value: `{"prefix":"db_"}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLColumnNameSpec", Label: "列名长度与大小写", Value: `{"max_len":63,"case":"any"}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLCharVarcharLength", Label: "CHAR / VARCHAR 长度上限", Value: `{"unlimited":false,"max_char":50,"max_varchar":5000}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLColumnTypeDisallow", Label: "列类型黑名单", Value: `{"disallow_types":["float","double","double precision","real"]}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLAllowDropColumn", Label: "是否允许 DROP COLUMN", Value: `{"allow":true}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLColumnTypeChange", Label: "列类型变更需确认兼容性", Value: "true"}, both},
		{reviewRuleSeed{Category: "dml", Name: "DMLAllowSubquery", Label: "是否允许 INSERT/UPDATE/DELETE 子查询", Value: `{"allow":false}`}, both},
		{reviewRuleSeed{Category: "dml", Name: "DMLAllowMultiTable", Label: "是否允许多表 UPDATE/DELETE", Value: `{"allow":false}`}, pgOnly},
		{reviewRuleSeed{Category: "dml", Name: "DMLInsertRowLimit", Label: "INSERT 行数上限", Value: `{"max_rows":1000}`}, pgOnly},
		{reviewRuleSeed{Category: "dml", Name: "DMLInsertDisallowOrderByRand", Label: "禁止 INSERT ... ORDER BY random()/rand()", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLDisallowAddColumnWithDefault", Label: "ADD COLUMN 禁止非常量 DEFAULT", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLDisallowAddNotNull", Label: "禁止给已有列加 NOT NULL", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLAddCheckNotValid", Label: "ADD CHECK 须带 NOT VALID", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLAddFKNotValid", Label: "ADD FOREIGN KEY 须带 NOT VALID", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLMergeAlterTable", Label: "同工单同表多条 ALTER 建议合成一条", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLCreateSpecifySchema", Label: "CREATE TABLE/VIEW/INDEX 建议指定 schema", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLIndexKeyNumberLimit", Label: "单索引列数上限", Value: `{"max_keys":5}`}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLIndexNoDuplicateColumn", Label: "禁止索引列重复", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLNamingNoKeyword", Label: "表/列名禁止使用保留字", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLNamingCharset", Label: "表/列名须小写字母开头，仅字母数字下划线", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLDisallowAddUniqueInline", Label: "已有表禁止直接 ADD UNIQUE", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLDisallowAddSerialPK", Label: "禁止 ALTER 加 serial/bigserial 主键列", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLIndexCreateConcurrently", Label: "索引 CREATE/DROP 建议 CONCURRENTLY", Value: `{"row_threshold":500000}`}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLReindexRefreshConcurrently", Label: "REINDEX / REFRESH MATERIALIZED VIEW 建议 CONCURRENTLY", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLAddColumnNotNullRequireDefault", Label: "ADD COLUMN NOT NULL 必须带 DEFAULT", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLForbidCluster", Label: "禁止 CLUSTER", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "dml", Name: "DMLInsertDisallowSelectStar", Label: "禁止 INSERT ... SELECT *", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLForbidGrant", Label: "禁止 GRANT", Value: "true"}, both},
		{reviewRuleSeed{Category: "ddl", Name: "DDLForbidLockVacuumFull", Label: "禁止 LOCK TABLE / VACUUM FULL", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLBackwardIncompatible", Label: "后向不兼容 DDL（DROP/RENAME）", Value: "true"}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLForeignKeyPolicy", Label: "外键策略", Value: `{"mode":"off"}`}, pgOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLAddColumnRequireDefault", Label: "ALTER 加列必须有 DEFAULT", Value: "true"}, srOnly},
		{reviewRuleSeed{Category: "ddl", Name: "DDLRequireUpdateTime", Label: "建表必须有时间字段", Value: `{"enabled":true,"column":"update_time","allowed_types":["timestamp","timestamptz","timestamp without time zone","timestamp with time zone"]}`}, pgOnly},
	}
	out := make([]reviewRuleSeed, 0, len(mysql)+len(pgsr)*2)
	for _, it := range mysql {
		it.Engine = "mysql"
		out = append(out, it)
	}
	for _, it := range pgsr {
		for _, engine := range it.Engines {
			item := it.reviewRuleSeed
			item.Engine = engine
			out = append(out, item)
		}
	}
	return out
}

func (d *DB) syncReviewLinkedArgs(name, value string) {
	key := strings.ToLower(strings.TrimSpace(name))
	switch key {
	case "check_dml_where", "dmlrequirewhere":
		_ = d.SetGlobalArg("dml_require_where", boolText(ruleFlag(value)), "UPDATE/DELETE 必须带 WHERE")
	case "disable_drop_database":
		_ = d.SetGlobalArg("ddl_forbid_drop_database", boolText(ruleFlag(value)), "DDL 禁止 DROP DATABASE")
	case "ddlforbiddropdatabase":
		_ = d.SetGlobalArg("ddl_forbid_drop_database", boolText(!ruleFlag(value)), "DDL 禁止 DROP DATABASE")
	}
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func ruleFlag(value string) bool {
	s := strings.TrimSpace(value)
	if strings.HasPrefix(s, "{") {
		var o map[string]any
		if json.Unmarshal([]byte(s), &o) == nil {
			if a, ok := o["allow"]; ok {
				return asReviewBool(a)
			}
			if e, ok := o["enabled"]; ok {
				return asReviewBool(e)
			}
		}
	}
	return asReviewBool(s)
}

func asReviewBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	default:
		s := strings.ToLower(strings.TrimSpace(strings.Trim(asReviewString(v), `"`)))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	}
}

func asReviewString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return boolText(t)
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		s := string(b)
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			var out string
			if json.Unmarshal(b, &out) == nil {
				return out
			}
		}
		return s
	}
}
