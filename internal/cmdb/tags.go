package cmdb

import (
	"fmt"
	"strings"

	"auto-dba/internal/store"
)

// EffectiveTags 合并 bindings + 真相源 JSON tags。
func (s *Service) EffectiveTags(ciType, ciID string) ([]string, map[string]string, error) {
	bindings, err := s.Store.ListCmdbTagBindings(ciType, ciID)
	if err != nil {
		return nil, nil, err
	}
	kv := map[string]string{}
	for _, b := range bindings {
		kv[b.Key] = b.Value
	}
	// 兼容 instance/database.tags
	switch ciType {
	case TypeInstance:
		it, _ := s.Store.GetInstance(ciID)
		if it != nil {
			mergeFreeTags(kv, it.Tags)
		}
	case TypeDatabase:
		db, _ := s.Store.GetDatabase(ciID)
		if db != nil {
			mergeFreeTags(kv, db.Tags)
		}
	}
	list := make([]string, 0, len(kv))
	for k, v := range kv {
		if v == "" {
			list = append(list, k)
		} else {
			list = append(list, k+"="+v)
		}
	}
	return list, kv, nil
}

func mergeFreeTags(kv map[string]string, tags []string) {
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if i := strings.IndexByte(t, '='); i > 0 {
			k, v := strings.TrimSpace(t[:i]), strings.TrimSpace(t[i+1:])
			if _, ok := kv[k]; !ok {
				kv[k] = v
			}
		} else if _, ok := kv[t]; !ok {
			kv["free"] = joinFree(kv["free"], t)
		}
	}
}

func joinFree(a, b string) string {
	if a == "" {
		return b
	}
	return a + "," + b
}

// SetTags 写 bindings，并回写 instance/database.tags JSON。
func (s *Service) SetTags(ciType, ciID string, pairs map[string]string, actor string) error {
	if err := s.Store.ReplaceCmdbTagBindings(ciType, ciID, pairs); err != nil {
		return err
	}
	tagList := make([]string, 0, len(pairs))
	for k, v := range pairs {
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "" {
			continue
		}
		if k == "free" {
			for _, p := range strings.Split(v, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					tagList = append(tagList, p)
				}
			}
			continue
		}
		if v == "" {
			tagList = append(tagList, k)
		} else {
			tagList = append(tagList, k+"="+v)
		}
	}
	switch ciType {
	case TypeInstance:
		it, err := s.Store.GetInstance(ciID)
		if err != nil || it == nil {
			return fmt.Errorf("实例不存在")
		}
		before := *it
		it.Tags = tagList
		if err := s.Store.UpdateInstance(it); err != nil {
			return err
		}
		_ = s.Store.AppendCmdbAudit(actor, "update", TypeInstance, ciID, "api", map[string]any{"tags": before.Tags}, map[string]any{"tags": it.Tags})
	case TypeDatabase:
		db, err := s.Store.GetDatabase(ciID)
		if err != nil || db == nil {
			return fmt.Errorf("库不存在")
		}
		before := *db
		db.Tags = tagList
		if err := s.Store.UpdateDatabase(db); err != nil {
			return err
		}
		_ = s.Store.AppendCmdbAudit(actor, "update", TypeDatabase, ciID, "api", map[string]any{"tags": before.Tags}, map[string]any{"tags": db.Tags})
	}
	return nil
}

// SyncMonitorTagsIntoCmdb 将监控标签字典同步为 CMDB 标签定义（scope=monitor）。
func (s *Service) SyncMonitorTagsIntoCmdb() error {
	mons, err := s.Store.ListMonitorLabelTags()
	if err != nil {
		return err
	}
	for _, m := range mons {
		def := &store.CmdbTagDef{
			Key: m.Key, Name: m.Name, Values: m.Values, Description: m.Description,
			Scope: "monitor", SortOrder: m.SortOrder,
			CITypes: []string{TypeMonitorJob, TypeMonitorGroup, TypeInstance},
		}
		// 查是否已有同 key
		exist, _ := s.Store.ListCmdbTagDefs("")
		for _, e := range exist {
			if e.Key == m.Key {
				def.ID = e.ID
				def.CreatedAt = e.CreatedAt
				break
			}
		}
		if err := s.Store.UpsertCmdbTagDef(def); err != nil {
			return err
		}
	}
	return nil
}
