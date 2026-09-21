package handler

import (
	"encoding/json"
	"strings"

	"auto-dba/internal/cdc"
	"auto-dba/internal/store"
)

func hydrateCDCFromMap(raw map[string]any) store.CDCTask {
	if raw == nil {
		raw = map[string]any{}
	}
	desc := jsonAnyString(raw["description"])
	text, extra := splitCDCDescMap(desc)
	if extra == nil {
		extra = map[string]any{}
	}
	mergeFlatCDCFields(raw, extra)
	t := store.CDCTask{
		ID:          jsonAnyString(raw["id"]),
		Name:        firstNonEmpty(jsonAnyString(raw["name"]), jsonAnyString(raw["task_name"])),
		TaskType:    jsonAnyString(raw["task_type"]),
		SourceID:    firstNonEmpty(jsonAnyString(raw["source_instance_id"]), jsonAnyString(extra["source_instance_id"])),
		TargetID:    firstNonEmpty(jsonAnyString(raw["target_instance_id"]), jsonAnyString(extra["target_instance_id"])),
		SourceDB:    firstNonEmpty(jsonAnyString(raw["source_db"]), jsonAnyString(raw["source_db_name"]), jsonAnyString(extra["source_db"]), jsonAnyString(extra["representative_db"])),
		TargetDB:    firstNonEmpty(jsonAnyString(raw["target_db"]), jsonAnyString(raw["target_db_name"]), jsonAnyString(extra["target_db"])),
		ParentID:    firstNonEmpty(jsonAnyString(raw["parent_id"]), jsonAnyString(raw["parent_task_id"])),
		Description: encodeCDCExtraJSON(text, extra),
	}
	return t
}

func splitCDCDescMap(desc string) (text string, extra map[string]any) {
	idx := strings.Index(desc, cdcExtraPrefix)
	if idx < 0 {
		return strings.TrimSpace(desc), map[string]any{}
	}
	text = strings.TrimSpace(desc[:idx])
	extra = parseCDCExtraMap(desc)
	if extra == nil {
		extra = map[string]any{}
	}
	return text, extra
}

func mergeFlatCDCFields(raw, extra map[string]any) {
	if raw == nil || extra == nil {
		return
	}
	copyKey := func(dest string, aliases ...string) {
		for _, k := range aliases {
			if k == "description" {
				continue
			}
			v, ok := raw[k]
			if !ok || v == nil {
				continue
			}
			if s, isStr := v.(string); isStr && strings.TrimSpace(s) == "" {
				continue
			}
			extra[dest] = v
			return
		}
	}
	copyKey("env", "env")
	copyKey("source_instance_id", "source_instance_id")
	copyKey("target_instance_id", "target_instance_id")
	copyKey("source_schema", "source_schema")
	copyKey("source_scope_mode", "source_scope_mode")
	copyKey("source_db_prefix", "source_db_prefix")
	copyKey("source_db_names", "source_db_names")
	copyKey("shard_db", "shard_db")
	copyKey("representative_db", "representative_db", "source_db", "source_db_name")
	copyKey("connector_id", "connector_id")
	copyKey("connect_base_url", "connect_base_url")
	copyKey("message_schema_mode", "message_schema_mode")
	copyKey("debezium_mode", "debezium_mode")
	copyKey("snapshot_mode", "snapshot_mode")
	copyKey("filter_delete_events", "filter_delete_events")
	copyKey("kafka_bootstrap_servers", "kafka_bootstrap_servers")
	copyKey("routine_load_parallelism", "routine_load_parallelism")
	copyKey("kafka_topic_partitions", "kafka_topic_partitions")
	copyKey("kafka_topic_replication_factor", "kafka_topic_replication_factor")
	copyKey("sr_auto_buckets", "sr_auto_buckets")
	copyKey("sr_replication_num", "sr_replication_num")
	copyKey("sr_dynamic_partition_enable", "sr_dynamic_partition_enable")
	copyKey("sr_partition_column", "sr_partition_column")
	copyKey("sr_dynamic_partition", "sr_dynamic_partition")
	copyKey("auto_create_table", "auto_create_table")
	copyKey("key_model", "key_model", "table_model")
	copyKey("publication_name", "publication_name")
	copyKey("slot_name", "slot_name")
	copyKey("connector_name", "connector_name")
	copyKey("topic_prefix", "topic_prefix")
	copyKey("debezium_template", "debezium_template")
	copyKey("debezium_config", "debezium_config")
	copyKey("alter_schema_tables", "alter_schema_tables")
	copyKey("alter_schema_exec_mode", "alter_schema_exec_mode")
	copyKey("alter_columns", "alter_columns")
	copyKey("table_map", "table_map")
	copyKey("sql_overrides", "sql_overrides")
	copyKey("kafka_default_offsets", "kafka_default_offsets")

	if maps := cdcMappingsFromAny(raw["mappings"]); len(maps) > 0 {
		extra["mappings"] = maps
	} else if looksLikeMappingList(raw["tables"]) {
		maps = cdcMappingsFromAny(raw["tables"])
		if len(maps) > 0 {
			extra["mappings"] = maps
		}
	}
	if names := jsonAnyStringSlice(raw["tables"]); len(names) > 0 && !looksLikeMappingList(raw["tables"]) {
		extra["tables"] = names
	} else if maps := cdc.ParseMappingsJSON(mustJSON(extra["mappings"])); len(maps) > 0 {
		var tables []string
		for _, m := range maps {
			tables = append(tables, m.SourceTables...)
		}
		if len(tables) > 0 && extra["tables"] == nil {
			extra["tables"] = tables
		}
	}
}

func looksLikeMappingList(v any) bool {
	if v == nil {
		return false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	var maps []map[string]any
	if json.Unmarshal(b, &maps) != nil || len(maps) == 0 {
		return false
	}
	_, hasTarget := maps[0]["target_table"]
	_, hasSrc := maps[0]["source_tables"]
	_, hasTgtName := maps[0]["target_table_name"]
	return hasTarget || hasSrc || hasTgtName
}

func mustJSON(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func cdcReviewersFromMap(raw map[string]any) []string {
	return []string{
		jsonAnyString(raw["reviewer"]),
		jsonAnyString(raw["approver"]),
		jsonAnyString(raw["assigned_to"]),
	}
}

func cdcParentIDsFromMap(raw map[string]any) []string {
	ids := uniqueNonEmpty(append(jsonAnyStringSlice(raw["parent_ids"]), jsonAnyStringSlice(raw["parent_task_ids"])...))
	if id := firstNonEmpty(jsonAnyString(raw["parent_id"]), jsonAnyString(raw["parent_task_id"])); id != "" {
		ids = uniqueNonEmpty(append([]string{id}, ids...))
	}
	return ids
}
