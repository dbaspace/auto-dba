package handler

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"auto-dba/internal/store"
)

// hostDedupeResult 是一次合并重复主机的结果。
type hostDedupeResult struct {
	Merged int      `json:"merged"`
	Kept   []string `json:"kept,omitempty"`
}

// hostKeepScore 云实例 ID、环境、区域越全越优先保留。
func hostKeepScore(it *store.Instance) int {
	n := 0
	if strings.TrimSpace(it.CloudInstanceID) != "" {
		n += 4
	}
	if strings.TrimSpace(it.EnvID) != "" {
		n += 2
	}
	if strings.TrimSpace(it.RegionID) != "" {
		n++
	}
	return n
}

// pickHostKeeper 返回保留行下标：分高者优先，同分取创建更早的。
func pickHostKeeper(group []store.Instance) int {
	best := 0
	for i := 1; i < len(group); i++ {
		a, b := hostKeepScore(&group[i]), hostKeepScore(&group[best])
		if a > b || (a == b && group[i].CreatedAt < group[best].CreatedAt) {
			best = i
		}
	}
	return best
}

// fillHostBlanks 用 src 补 dst 的空字段，返回是否有变化。不动连接主机、账号和密码。
func fillHostBlanks(dst, src *store.Instance) bool {
	changed := false
	fill := func(cur *string, v string) {
		if strings.TrimSpace(*cur) == "" && strings.TrimSpace(v) != "" {
			*cur = v
			changed = true
		}
	}
	fill(&dst.RegionID, src.RegionID)
	if strings.TrimSpace(dst.EnvID) == "" && strings.TrimSpace(src.EnvID) != "" {
		dst.EnvID = src.EnvID
		dst.IDC = src.IDC
		changed = true
	}
	fill(&dst.CloudVendor, src.CloudVendor)
	fill(&dst.CloudInstanceID, src.CloudInstanceID)
	fill(&dst.CloudRegion, src.CloudRegion)
	fill(&dst.CloudAccountID, src.CloudAccountID)
	fill(&dst.Description, src.Description)
	if len(dst.Tags) == 0 && len(src.Tags) > 0 {
		dst.Tags = append([]string(nil), src.Tags...)
		changed = true
	}
	return changed
}

// hostIDRefs 只看按实例 ID 绑定的引用。同地址的保留行还在，按地址的引用不受合并影响。
func hostIDRefs(db *store.DB, it store.Instance) ([]hostRef, error) {
	it.Host, it.DomainName, it.Domains = "", "", nil
	return collectHostRefs(db, &it)
}

// MergeDuplicateHosts 供云主机同步前调用，失败只记日志。
func MergeDuplicateHosts(db *store.DB, actor string) {
	res, err := mergeDuplicateHosts(db, actor)
	if err != nil {
		log.Printf("merge duplicate hosts: %v", err)
		return
	}
	if res.Merged > 0 || len(res.Kept) > 0 {
		log.Printf("merge duplicate hosts: merged=%d kept=%v", res.Merged, res.Kept)
	}
}

// mergeDuplicateHosts 把连接地址相同的主机合并成一条。被 ID 引用的行留着并说明。
func mergeDuplicateHosts(db *store.DB, actor string) (hostDedupeResult, error) {
	var res hostDedupeResult
	list, err := db.ListHostInstances()
	if err != nil {
		return res, err
	}
	groups := map[string][]store.Instance{}
	var keys []string
	for _, it := range list {
		k := store.HostAddrKey(it.Host)
		if k == "" {
			continue
		}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], it)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g := groups[k]
		if len(g) < 2 {
			continue
		}
		ki := pickHostKeeper(g)
		keep := g[ki]
		before := keep
		changed := false
		for i := range g {
			if i != ki && fillHostBlanks(&keep, &g[i]) {
				changed = true
			}
		}
		if changed {
			if err := db.UpdateInstance(&keep); err != nil {
				return res, err
			}
			_ = db.AppendCmdbAudit(actor, "update", "instance", keep.ID, "host-dedupe", redactInstanceCopy(before), redactInstanceCopy(keep))
		}
		for i := range g {
			if i == ki {
				continue
			}
			dup := g[i]
			if err := db.RepointCloudResources(dup.ID, keep.ID); err != nil {
				return res, err
			}
			refs, err := hostIDRefs(db, dup)
			if err != nil {
				return res, err
			}
			if len(refs) > 0 {
				res.Kept = append(res.Kept, fmt.Sprintf("%s（%s）仍被 %s 引用，未删除", dup.Name, k, formatHostRefs(refs)))
				continue
			}
			if err := db.DeleteInstance(dup.ID); err != nil {
				res.Kept = append(res.Kept, fmt.Sprintf("%s（%s）删除失败：%v", dup.Name, k, err))
				continue
			}
			_ = db.AppendCmdbAudit(actor, "delete", "instance", dup.ID, "host-dedupe", redactInstanceCopy(dup), nil)
			res.Merged++
		}
	}
	return res, nil
}
