package handler

import (
	"encoding/json"
	"strings"

	"auto-dba/internal/store"
)

// dbNameFamilyPrefix 与前端 ticketEnv.dbNameFamilyPrefix 对齐：db_marketing_064 → db_marketing_
func dbNameFamilyPrefix(dbName string) string {
	s := strings.TrimSpace(dbName)
	if s == "" {
		return ""
	}
	i := strings.LastIndex(s, "_")
	if i <= 0 {
		return s
	}
	return s[:i+1]
}

func dbHasApprovers(d store.Database) bool {
	for _, a := range d.Approvers {
		if strings.TrimSpace(a) != "" {
			return true
		}
	}
	return false
}

// resolveDbNameForApprover 为某实例解析用于取库级审核人的库名（与前端 resolveDbNameForApprover 对齐）。
// 1) 同实例上库名与逻辑库一致；2) 分库时按前缀/族前缀匹配；3) 否则空串，调用方回退实例审批人。
func resolveDbNameForApprover(instanceID, formDBName string, records []store.Database, shardDB bool, shardPrefix string) string {
	iid := strings.TrimSpace(instanceID)
	want := strings.TrimSpace(formDBName)
	if iid == "" {
		return want
	}
	var onInst []store.Database
	for _, d := range records {
		name := strings.TrimSpace(d.DBName)
		if name == "" {
			continue
		}
		inst := strings.TrimSpace(d.InstanceID)
		if inst != "" && inst != iid {
			continue
		}
		onInst = append(onInst, d)
	}
	if want != "" {
		for _, d := range onInst {
			if strings.TrimSpace(d.DBName) == want {
				return want
			}
		}
	}
	if !shardDB {
		return ""
	}
	prefix := strings.TrimSpace(shardPrefix)
	candidates := []string{prefix, dbNameFamilyPrefix(prefix), dbNameFamilyPrefix(want)}
	seen := map[string]struct{}{}
	for _, p := range candidates {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		var matches []store.Database
		for _, d := range onInst {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(d.DBName)), key) {
				matches = append(matches, d)
			}
		}
		for _, d := range matches {
			if dbHasApprovers(d) {
				return strings.TrimSpace(d.DBName)
			}
		}
		if len(matches) > 0 {
			return strings.TrimSpace(matches[0].DBName)
		}
	}
	return ""
}

func ddlShardDBEnabled(needRollback string) bool {
	raw := strings.TrimSpace(needRollback)
	if raw == "" {
		return false
	}
	var extra struct {
		ShardDB bool `json:"shard_db"`
	}
	if json.Unmarshal([]byte(raw), &extra) != nil {
		return false
	}
	return extra.ShardDB
}
