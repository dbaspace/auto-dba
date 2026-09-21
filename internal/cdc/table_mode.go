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
