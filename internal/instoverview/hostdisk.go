package instoverview

import (
	"regexp"
	"sort"
	"strings"

	"auto-dba/internal/cloud"
	"auto-dba/internal/store"
)

// HostInfo 是主机行的云规格和磁盘。容量来自云清单，数据盘使用率来自 node_exporter。
type HostInfo struct {
	CloudMatched  bool                 `json:"cloud_matched,omitempty"`
	CloudStatus   string               `json:"cloud_status,omitempty"`
	CPU           int                  `json:"cpu,omitempty"`
	MemoryMB      int                  `json:"memory_mb,omitempty"`
	SystemDiskGB  int                  `json:"system_disk_gb,omitempty"`
	DataDiskGB    int                  `json:"data_disk_gb,omitempty"`
	DataDisks     []cloud.DataDiskSpec `json:"data_disks,omitempty"`
	DataUsedRate  float64              `json:"data_used_rate,omitempty"`
	DataUsedKnown bool                 `json:"data_used_known,omitempty"`
	DataUsedBytes float64              `json:"data_used_bytes,omitempty"`
	DataSizeBytes float64              `json:"data_size_bytes,omitempty"`
	SysUsedRate   float64              `json:"sys_used_rate,omitempty"`
	SysUsedKnown  bool                 `json:"sys_used_known,omitempty"`
	SysUsedBytes  float64              `json:"sys_used_bytes,omitempty"`
	SysSizeBytes  float64              `json:"sys_size_bytes,omitempty"`
	CollectedAt   string               `json:"collected_at,omitempty"`
}

// CloudHostInfo 从一条云主机清单取规格和两类盘的容量。
func CloudHostInfo(it store.CloudInstance) HostInfo {
	sys, _ := cloud.SystemDiskFromExtras(it.Extras)
	disks := cloud.DataDisksFromExtras(it.Extras)
	total := 0
	for _, d := range disks {
		total += d.GB
	}
	return HostInfo{
		CloudMatched: true,
		CloudStatus:  strings.TrimSpace(it.Status),
		CPU:          it.CPU,
		MemoryMB:     it.MemoryMB,
		SystemDiskGB: sys,
		DataDiskGB:   total,
		DataDisks:    disks,
	}
}

// HostDisplayDomain 优先取标签里第一条非空 domain=，否则用域名列表，再否则用 domain_name。
func HostDisplayDomain(tags, domains []string, domainName string) string {
	for _, raw := range tags {
		k, v, ok := splitTag(raw)
		if !ok || !strings.EqualFold(k, "domain") || v == "" {
			continue
		}
		return v
	}
	var parts []string
	for _, d := range domains {
		if s := strings.TrimSpace(d); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "，")
	}
	return strings.TrimSpace(domainName)
}

// HostAppName 取标签里第一条非空 app=，键不区分大小写。没有时为空。
func HostAppName(tags []string) string {
	for _, raw := range tags {
		k, v, ok := splitTag(raw)
		if ok && strings.EqualFold(k, "app") && v != "" {
			return v
		}
	}
	return ""
}

func splitTag(raw string) (key, value string, ok bool) {
	raw = strings.TrimSpace(raw)
	i := strings.Index(raw, "=")
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+1:]), true
}

// HostCloudKey 是平台主机用来对照云清单的字段。
type HostCloudKey struct {
	ID      string
	CloudID string
	Host    string
}

// MatchCloudHosts 依次按云实例 ID、云行登记的平台实例 ID、内网 IP 对照。同一键取同步时间最新的一条。
func MatchCloudHosts(keys []HostCloudKey, rows []store.CloudInstance) map[string]store.CloudInstance {
	byCloud := map[string]store.CloudInstance{}
	byInst := map[string]store.CloudInstance{}
	byIP := map[string]store.CloudInstance{}
	keep := func(m map[string]store.CloudInstance, k string, it store.CloudInstance) {
		if k == "" {
			return
		}
		if cur, ok := m[k]; !ok || it.SyncedAt > cur.SyncedAt {
			m[k] = it
		}
	}
	for _, it := range rows {
		keep(byCloud, strings.ToLower(strings.TrimSpace(it.CloudID)), it)
		keep(byInst, strings.TrimSpace(it.InstanceID), it)
		ip, _ := cloud.NetFromExtras(cloud.EngineHost, it.Extras)
		keep(byIP, store.HostAddrKey(ip), it)
	}
	out := map[string]store.CloudInstance{}
	for _, k := range keys {
		if it, ok := byCloud[strings.ToLower(strings.TrimSpace(k.CloudID))]; ok && k.CloudID != "" {
			out[k.ID] = it
			continue
		}
		if it, ok := byInst[k.ID]; ok {
			out[k.ID] = it
			continue
		}
		if it, ok := byIP[store.HostAddrKey(k.Host)]; ok && store.HostAddrKey(k.Host) != "" {
			out[k.ID] = it
		}
	}
	return out
}

// HostAddrs 把平台主机地址规整成去重排序后的 IP 或主机名。
func HostAddrs(hosts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range hosts {
		k := store.HostAddrKey(h)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// HostFSExprs 返回只覆盖这些地址的 node_filesystem size / avail 查询。instance 允许带端口。
func HostFSExprs(addrs []string) (size, avail string) {
	if len(addrs) == 0 {
		return "", ""
	}
	match := hostInstanceMatcher(addrs)
	if match == "" {
		return "", ""
	}
	sel := `{fstype=~"ext4|xfs",` + match + `}`
	return "node_filesystem_size_bytes" + sel, "node_filesystem_avail_bytes" + sel
}

func hostInstanceMatcher(addrs []string) string {
	if len(addrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		parts = append(parts, strings.ReplaceAll(regexp.QuoteMeta(a), `\`, `\\`))
	}
	return `instance=~"(` + strings.Join(parts, "|") + `)(:[0-9]+)?"`
}

// FSSample 是一条 node_filesystem 即时样本。
type FSSample struct {
	Instance   string
	Mountpoint string
	Device     string
	Size       float64
	Avail      float64
}

// DiskUsage 是一台主机数据盘挂载的合计容量和已用。
type DiskUsage struct {
	Size float64
	Used float64
}

// Rate 是已用占合计容量的百分比。
func (u DiskUsage) Rate() float64 {
	if u.Size <= 0 {
		return 0
	}
	return u.Used / u.Size * 100
}

// systemMount 是系统盘上的挂载，不算数据盘。
func systemMount(mp string) bool {
	switch strings.TrimSpace(mp) {
	case "/", "/boot", "/boot/efi":
		return true
	}
	return false
}

// HostDisk 是一台主机的数据盘和系统盘（根分区）用量。
type HostDisk struct {
	Data   DiskUsage
	System DiskUsage
}

// HostDiskUsage 把同一批样本拆成数据盘和根分区两份，按主机 IP 归并。
func HostDiskUsage(samples []FSSample) map[string]HostDisk {
	out := map[string]HostDisk{}
	for ip, u := range HostDataUsage(samples) {
		d := out[ip]
		d.Data = u
		out[ip] = d
	}
	for ip, u := range HostSystemUsage(samples) {
		d := out[ip]
		d.System = u
		out[ip] = d
	}
	return out
}

// HostSystemUsage 只取挂载点 /，与主机监控和告警里的根分区一致。/boot 不计入。
func HostSystemUsage(samples []FSSample) map[string]DiskUsage {
	out := map[string]DiskUsage{}
	for _, s := range samples {
		if strings.TrimSpace(s.Mountpoint) != "/" || s.Size <= 0 {
			continue
		}
		ip := store.HostAddrKey(s.Instance)
		if ip == "" {
			continue
		}
		if _, ok := out[ip]; ok {
			continue
		}
		used := s.Size - s.Avail
		if used < 0 {
			used = 0
		}
		out[ip] = DiskUsage{Size: s.Size, Used: used}
	}
	return out
}

// HostDataUsage 把 node_filesystem 样本按主机 IP 归并成数据盘用量。同一设备只算一次，避免 bind mount 重复。
func HostDataUsage(samples []FSSample) map[string]DiskUsage {
	type devKey struct{ ip, dev string }
	seen := map[devKey]bool{}
	out := map[string]DiskUsage{}
	for _, s := range samples {
		if systemMount(s.Mountpoint) || s.Size <= 0 {
			continue
		}
		ip := store.HostAddrKey(s.Instance)
		if ip == "" {
			continue
		}
		dev := strings.TrimSpace(s.Device)
		if dev == "" {
			dev = s.Mountpoint
		}
		k := devKey{ip, dev}
		if seen[k] {
			continue
		}
		seen[k] = true
		u := out[ip]
		u.Size += s.Size
		used := s.Size - s.Avail
		if used > 0 {
			u.Used += used
		}
		out[ip] = u
	}
	return out
}
