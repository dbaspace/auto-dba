package cdc

import (
	"encoding/json"
	"strings"
)

const (
	SyncTableModeIncremental    = "incremental"
	SyncTableModeInitialization = "initialization"
)

func NormalizeSyncTableMode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case SyncTableModeInitialization, "initial", "init":
		return SyncTableModeInitialization
	default:
		return SyncTableModeIncremental
	}
}

func MappingIsInitialization(m TableMapping) bool {
	return NormalizeSyncTableMode(m.SyncTableMode) == SyncTableModeInitialization
}

func FilterInitializationMappings(maps []TableMapping) []TableMapping {
	out := make([]TableMapping, 0, len(maps))
	for _, m := range maps {
		if MappingIsInitialization(m) {
			out = append(out, m)
		}
	}
	return out
}

// SnapshotMappingsForAddTable 决定加表任务要写入 execute-snapshot 的映射：
// 优先用 sync_table_mode=initialization；否则在 snapshot_mode 为 initial/空（详情默认显示 initial）时对本任务全部映射写信号；
// snapshot_mode=never 且无 initialization 映射时跳过。
func SnapshotMappingsForAddTable(taskType string, extra map[string]string) []TableMapping {
	if extra == nil {
		extra = map[string]string{}
	}
	maps := ParseMappingsJSON(extra["mappings"])
	initMaps := FilterInitializationMappings(maps)
	if len(initMaps) > 0 {
		return initMaps
	}
	if strings.TrimSpace(taskType) != "add_table" {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(extra["snapshot_mode"]))
	if mode == "never" {
		return nil
	}
	switch mode {
	case "", "initial", SyncTableModeInitialization, "init":
		return maps
	default:
		return nil
	}
}

func BuildSnapshotDataCollections(schema string, maps []TableMapping) []string {
	schema = defaultStr(schema, "public")
	seen := map[string]struct{}{}
	var out []string
	for _, m := range maps {
		for _, st := range cleanStringSlice(m.SourceTables) {
			coll := schema + "." + st
			key := strings.ToLower(coll)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, coll)
		}
	}
	return out
}

func KafkaOffsetForMapping(taskType string, m TableMapping) string {
	if strings.TrimSpace(taskType) != "add_table" {
		return "OFFSET_BEGINNING"
	}
	if MappingIsInitialization(m) {
		return "OFFSET_BEGINNING"
	}
	return "OFFSET_END"
}

func (m *TableMapping) UnmarshalJSON(data []byte) error {
	type alias TableMapping
	aux := struct {
		*alias
		SyncMode string `json:"sync_mode"`
	}{alias: (*alias)(m)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if strings.TrimSpace(m.SyncTableMode) == "" {
		m.SyncTableMode = aux.SyncMode
	}
	m.SyncTableMode = NormalizeSyncTableMode(m.SyncTableMode)
	return nil
}
