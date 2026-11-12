package store

import (
	"net"
	"strconv"
	"strings"
	"time"
)

// HostAddrKey 是主机连接地址的比较键：取逗号分隔的第一段，去掉端口，忽略大小写。
func HostAddrKey(host string) string {
	s := strings.TrimSpace(host)
	s = strings.ReplaceAll(s, "，", ",")
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	} else if i := strings.LastIndexByte(s, ':'); i > 0 && strings.Count(s, ":") == 1 {
		if _, err := strconv.Atoi(s[i+1:]); err == nil {
			s = s[:i]
		}
	}
	s = strings.TrimPrefix(strings.TrimSuffix(s, "]"), "[")
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return strings.TrimSuffix(s, ".")
}

// ListHostInstances 列出全部租户的主机实例。
func (d *DB) ListHostInstances() ([]Instance, error) {
	all, err := d.ListInstances("", "")
	if err != nil {
		return nil, err
	}
	out := make([]Instance, 0, len(all))
	for _, it := range all {
		if IsHostInstanceType(it.DBType) {
			out = append(out, it)
		}
	}
	return out, nil
}

// FindHostByAddr 按连接地址找主机实例，跳过 excludeID。没有时返回 nil。
func (d *DB) FindHostByAddr(host, excludeID string) (*Instance, error) {
	key := HostAddrKey(host)
	if key == "" {
		return nil, nil
	}
	list, err := d.ListHostInstances()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == excludeID {
			continue
		}
		if HostAddrKey(list[i].Host) == key {
			it := list[i]
			return &it, nil
		}
	}
	return nil, nil
}

// RepointCloudResources 把挂在 from 实例上的云清单改挂到 to。
func (d *DB) RepointCloudResources(from, to string) error {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || from == to {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_cloud_instances SET instance_id=?, updated_at=? WHERE instance_id=?`, to, now, from)
	return err
}

// DeleteCloudResourcesNotIn 删掉某账号某引擎在 regions 内、云实例 ID 不在 keep 里的云清单行。
// 未成功拉取的地域不在 regions 里，其行保留。
func (d *DB) DeleteCloudResourcesNotIn(accountID, engine string, regions, keep []string) (int, error) {
	rows, err := d.Query(`SELECT id, cloud_instance_id, region FROM tbl_cloud_instances WHERE account_id=? AND COALESCE(engine,'postgres')=?`, accountID, engine)
	if err != nil {
		return 0, err
	}
	okRegion := make(map[string]bool, len(regions))
	for _, r := range regions {
		okRegion[r] = true
	}
	keepSet := make(map[string]bool, len(keep))
	for _, id := range keep {
		keepSet[strings.ToLower(strings.TrimSpace(id))] = true
	}
	var drop []string
	for rows.Next() {
		var id, cloudID, region string
		if rows.Scan(&id, &cloudID, &region) != nil || !okRegion[region] {
			continue
		}
		if !keepSet[strings.ToLower(strings.TrimSpace(cloudID))] {
			drop = append(drop, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, id := range drop {
		if _, err := d.Exec(`DELETE FROM tbl_cloud_instances WHERE id=?`, id); err != nil {
			return 0, err
		}
	}
	return len(drop), nil
}
