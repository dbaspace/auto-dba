package cdc

import (
	"encoding/json"
	"regexp"
	"strings"

	"auto-dba/internal/store"
)

const (
	VersionSourceUISave      = "ui_save"
	VersionSourceRestore     = "restore"
	VersionSourceAddTable    = "add_table"
	VersionSourceRemoveTable = "remove_table"
	VersionSourceReinit      = "reinit"
	VersionKeepN             = 50
	connectorMaskValue       = "********"
)

var sensitiveKeyRE = regexp.MustCompile(`(?i)password|secret|token|credential`)

func VersionTaskID(t *store.CDCTask) string {
	if t == nil {
		return ""
	}
	if p := strings.TrimSpace(t.ParentID); p != "" {
		return p
	}
	return t.ID
}

func IsSensitiveConfigKey(key string) bool {
	return sensitiveKeyRE.MatchString(key)
}

func ParseConfigMap(raw string) map[string]string {
	out := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) != nil {
		return out
	}
	for k, v := range obj {
		switch x := v.(type) {
		case string:
			out[k] = x
		case nil:
			out[k] = ""
		default:
			b, _ := json.Marshal(x)
			out[k] = string(b)
		}
	}
	return out
}

func CurrentConfigFromExtra(extra map[string]string) map[string]string {
	if extra == nil {
		return map[string]string{}
	}
	return ParseConfigMap(extra["debezium_config"])
}

func MarshalConfigMap(cfg map[string]string) string {
	if cfg == nil {
		cfg = map[string]string{}
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

func ConfigMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || va != vb {
			return false
		}
	}
	return true
}

func MaskConfigMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if IsSensitiveConfigKey(k) {
			out[k] = connectorMaskValue
			continue
		}
		out[k] = v
	}
	return out
}

func MergeConfigForUpdate(incoming, current map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range incoming {
		if IsSensitiveConfigKey(k) && (strings.TrimSpace(v) == connectorMaskValue || strings.TrimSpace(v) == MaskedPassword || strings.TrimSpace(v) == "") {
			if orig, ok := current[k]; ok {
				out[k] = orig
				continue
			}
		}
		out[k] = v
	}
	return out
}

func ConfigKeyCount(cfg map[string]string) int {
	return len(cfg)
}

type ConfigDiff struct {
	Added   []string            `json:"added"`
	Removed []string            `json:"removed"`
	Changed []ConfigDiffChanged `json:"changed"`
}

type ConfigDiffChanged struct {
	Key        string `json:"key"`
	Current    string `json:"current"`
	Historical string `json:"historical"`
}

func DiffConfigMaps(current, historical map[string]string) ConfigDiff {
	if current == nil {
		current = map[string]string{}
	}
	if historical == nil {
		historical = map[string]string{}
	}
	out := ConfigDiff{}
	for k := range current {
		hv, ok := historical[k]
		if !ok {
			out.Added = append(out.Added, k)
			continue
		}
		cv, hvShow := current[k], hv
		if IsSensitiveConfigKey(k) {
			cv, hvShow = connectorMaskValue, connectorMaskValue
		}
		if current[k] != hv {
			out.Changed = append(out.Changed, ConfigDiffChanged{Key: k, Current: cv, Historical: hvShow})
		}
	}
	for k := range historical {
		if _, ok := current[k]; !ok {
			out.Removed = append(out.Removed, k)
		}
	}
	return out
}

func QualifyTableInclude(schema, table string) string {
	table = strings.TrimSpace(table)
	if table == "" {
		return ""
	}
	if strings.Contains(table, ".") {
		return table
	}
	schema = strings.TrimSpace(schema)
	if schema == "" {
		schema = "public"
	}
	return schema + "." + table
}

func MergeTableIncludeList(current, schema string, tables []string) string {
	list := SplitTableIncludeList(current)
	seen := map[string]struct{}{}
	for _, t := range list {
		seen[strings.ToLower(t)] = struct{}{}
	}
	for _, t := range tables {
		q := QualifyTableInclude(schema, t)
		if q == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(q)]; ok {
			continue
		}
		seen[strings.ToLower(q)] = struct{}{}
		list = append(list, q)
	}
	return strings.Join(list, ",")
}

func RemoveFromTableIncludeList(current, schema string, tables []string) string {
	drop := map[string]struct{}{}
	for _, t := range tables {
		q := QualifyTableInclude(schema, t)
		if q != "" {
			drop[strings.ToLower(q)] = struct{}{}
		}
		if n := strings.TrimSpace(t); n != "" {
			drop[strings.ToLower(n)] = struct{}{}
		}
	}
	var out []string
	for _, t := range SplitTableIncludeList(current) {
		if _, ok := drop[strings.ToLower(t)]; ok {
			continue
		}
		out = append(out, t)
	}
	return strings.Join(out, ",")
}

func SplitTableIncludeList(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func TableIncludeDelta(current, historical map[string]string) (add, drop []string) {
	cur := SplitTableIncludeList(current["table.include.list"])
	hist := SplitTableIncludeList(historical["table.include.list"])
	curSet := map[string]bool{}
	for _, t := range cur {
		curSet[t] = true
	}
	histSet := map[string]bool{}
	for _, t := range hist {
		histSet[t] = true
	}
	for _, t := range hist {
		if !curSet[t] {
			add = append(add, t)
		}
	}
	for _, t := range cur {
		if !histSet[t] {
			drop = append(drop, t)
		}
	}
	return add, drop
}

func WriteTaskDebeziumConfig(st *store.DB, t *store.CDCTask, cfg map[string]string) error {
	if st == nil || t == nil {
		return nil
	}
	extra := parseTaskExtra(t.Description)
	extra["debezium_config"] = MarshalConfigMap(cfg)
	text, _ := splitCDCDesc(t.Description)
	b, _ := json.Marshal(extra)
	t.Description = strings.TrimSpace(text + "\n-- auto-dba-cdc:" + string(b))
	return st.UpdateCDCDescription(t.ID, t.Description)
}

func SaveConnectorConfigVersionIfChanged(st *store.DB, taskID string, before map[string]string, source, operator, ip string) (int, error) {
	if st == nil || strings.TrimSpace(taskID) == "" {
		return 0, nil
	}
	if before == nil {
		before = map[string]string{}
	}
	latest, err := st.GetLatestCDCConnectorVersion(taskID)
	if err != nil {
		return 0, err
	}
	if latest != nil && ConfigMapsEqual(before, ParseConfigMap(latest.ConfigJSON)) {
		return 0, nil
	}
	row := &store.CDCConnectorConfigVersion{
		TaskID:       taskID,
		ConfigJSON:   MarshalConfigMap(before),
		ChangeSource: source,
		Operator:     operator,
		OperatorIP:   ip,
	}
	if err := st.InsertCDCConnectorVersion(row); err != nil {
		return 0, err
	}
	_ = st.PruneCDCConnectorVersions(taskID, VersionKeepN)
	return row.VersionNo, nil
}
