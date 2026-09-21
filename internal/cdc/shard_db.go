package cdc

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ShardDbBinding struct {
	DbName          string `json:"db_name"`
	PublicationName string `json:"publication_name"`
	SlotName        string `json:"slot_name"`
	ConnectorName   string `json:"connector_name"`
}

func ShardModeActive(extra map[string]string) bool {
	if extra == nil {
		return false
	}
	if extra["shard_db"] != "true" && extra["source_scope_mode"] != "multi_db" {
		return false
	}
	return len(ResolveShardDBs(extra, "")) >= 2
}

func ResolveShardDBs(extra map[string]string, representative string) []string {
	if extra == nil {
		if representative != "" {
			return []string{representative}
		}
		return nil
	}
	names := splitCSV(extra["source_db_names"])
	rep := strings.TrimSpace(representative)
	if rep == "" {
		rep = strings.TrimSpace(extra["representative_db"])
	}
	if extra["shard_db"] != "true" && extra["source_scope_mode"] != "multi_db" {
		if rep != "" {
			return []string{rep}
		}
		if len(names) == 1 {
			return names
		}
		return nil
	}
	if len(names) < 2 {
		if rep != "" {
			return []string{rep}
		}
		return names
	}
	if rep != "" && !containsFold(names, rep) {
		names = append([]string{rep}, names...)
	}
	return cleanStringSlice(names)
}

func ValidateShardCreate(extra map[string]string, sourceDB string) error {
	if extra == nil {
		return nil
	}
	if extra["shard_db"] != "true" && extra["source_scope_mode"] != "multi_db" {
		return nil
	}
	dbs := ResolveShardDBs(extra, sourceDB)
	if len(dbs) < 2 {
		return fmt.Errorf("多分库至少需要匹配 2 个库")
	}
	rep := strings.TrimSpace(sourceDB)
	if rep == "" {
		return fmt.Errorf("多分库时代表库必填")
	}
	if !containsFold(dbs, rep) {
		return fmt.Errorf("代表库 %q 必须包含在 source_db_names 中", rep)
	}
	if prefix := strings.TrimSpace(extra["source_db_prefix"]); prefix != "" {
		for _, db := range dbs {
			if !strings.HasPrefix(db, prefix) {
				return fmt.Errorf("分库 %q 不匹配前缀 %q", db, prefix)
			}
		}
	}
	return nil
}

func BuildShardBindings(extra map[string]string, representative, pub, slot, connector string) []ShardDbBinding {
	dbs := ResolveShardDBs(extra, representative)
	if len(dbs) == 0 && representative != "" {
		dbs = []string{representative}
	}
	out := make([]ShardDbBinding, 0, len(dbs))
	rep := strings.TrimSpace(representative)
	env := ""
	if extra != nil {
		env = extra["env"]
	}
	for _, db := range dbs {
		if strings.EqualFold(db, rep) || len(out) == 0 && rep == "" {
			out = append(out, ShardDbBinding{
				DbName:          db,
				PublicationName: defaultStr(pub, generatePublicationName(env, db)),
				SlotName:        defaultStr(slot, generateSlotName(env, db)),
				ConnectorName:   defaultStr(connector, generateConnectorName(env, db)),
			})
			continue
		}
		out = append(out, ShardDbBinding{
			DbName:          db,
			PublicationName: generatePublicationName(env, db),
			SlotName:        generateSlotName(env, db),
			ConnectorName:   generateConnectorName(env, db),
		})
	}
	return out
}

func ParseShardBindings(extra map[string]string) []ShardDbBinding {
	if extra == nil {
		return nil
	}
	raw := strings.TrimSpace(extra["shard_db_bindings"])
	if raw == "" || raw == "null" {
		return nil
	}
	var out []ShardDbBinding
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func PersistShardBindings(extra map[string]string, bindings []ShardDbBinding) {
	if extra == nil {
		return
	}
	if len(bindings) == 0 {
		delete(extra, "shard_db_bindings")
		return
	}
	b, err := json.Marshal(bindings)
	if err != nil {
		return
	}
	extra["shard_db_bindings"] = string(b)
	if len(bindings) >= 2 {
		extra["shard_db"] = "true"
		extra["source_scope_mode"] = "multi_db"
		names := make([]string, 0, len(bindings))
		for _, x := range bindings {
			names = append(names, x.DbName)
		}
		extra["source_db_names"] = strings.Join(names, ",")
	}
}

func ResolveOrBuildShardBindings(extra map[string]string, representative, pub, slot, connector string) []ShardDbBinding {
	if existing := ParseShardBindings(extra); len(existing) > 0 {
		return existing
	}
	return BuildShardBindings(extra, representative, pub, slot, connector)
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func MappingAppliesToDB(m TableMapping, db string) bool {
	names := cleanStringSlice(m.SourceDbNames)
	if len(names) == 0 || !strings.EqualFold(strings.TrimSpace(m.SourceDbScope), "explicit") {
		return true
	}
	return containsFold(names, db)
}

func TablesForShardDB(maps []TableMapping, fallback []string, db string) []string {
	if len(maps) == 0 {
		return cleanStringSlice(fallback)
	}
	var out []string
	for _, m := range maps {
		if !MappingAppliesToDB(m, db) {
			continue
		}
		src := cleanStringSlice(m.SourceTables)
		if len(src) == 0 && strings.TrimSpace(m.TargetTable) != "" {
			src = []string{m.TargetTable}
		}
		out = append(out, src...)
	}
	return cleanStringSlice(out)
}

func ValidateMappingShardScopes(maps []TableMapping, taskDBs []string) error {
	taskDBs = cleanStringSlice(taskDBs)
	if len(taskDBs) == 0 {
		return nil
	}
	for _, m := range maps {
		names := cleanStringSlice(m.SourceDbNames)
		if len(names) == 0 {
			continue
		}
		label := strings.TrimSpace(m.TargetTable)
		if label == "" && len(m.SourceTables) > 0 {
			label = m.SourceTables[0]
		}
		for _, db := range names {
			if !containsFold(taskDBs, db) {
				return fmt.Errorf("映射 %q 的 source_db_names 含非法库 %q（须为任务级分库子集）", label, db)
			}
		}
	}
	return nil
}

// FormatShardPrecheckRepOnlyMessage 分库预检通过文案：仅代表库假定表结构一致，跳过其余分库连库抽检。
func FormatShardPrecheckRepOnlyMessage(shardDBs []string, representative string) string {
	nonRep := len(shardDBs) - 1
	if nonRep < 0 {
		nonRep = 0
	}
	rep := strings.TrimSpace(representative)
	return fmt.Sprintf("共 %d 库（%s）；源表按代表库 %s 校验（假定各分库表结构一致，已跳过其余 %d 个分库连库抽检）",
		len(shardDBs), JoinSample(shardDBs, 20), rep, nonRep)
}
