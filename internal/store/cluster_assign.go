package store

import (
	"fmt"
	"strings"
	"time"
)

func (d *DB) requireActiveTenant(toTenantID string) error {
	toTenantID = strings.TrimSpace(toTenantID)
	if toTenantID == "" {
		return fmt.Errorf("目标租户必填")
	}
	t, err := d.GetTenant(toTenantID)
	if err != nil {
		return err
	}
	if t == nil || t.Status != TenantStatusActive {
		return fmt.Errorf("目标租户不存在或已停用")
	}
	return nil
}

func (d *DB) instancesLinkedToSrCluster(clusterID string) ([]Instance, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return nil, nil
	}
	list, err := d.ListInstances("", "")
	if err != nil {
		return nil, err
	}
	var out []Instance
	for i := range list {
		if srClusterIDFromTags(list[i].Tags) == clusterID {
			out = append(out, list[i])
		}
	}
	return out, nil
}

func (d *DB) assignMonitorJobsForSrCluster(clusterID, toTenantID string) error {
	marker := "sr-cluster=" + strings.TrimSpace(clusterID)
	jobs, err := d.ListMonitorJobs("")
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, j := range jobs {
		if !strings.Contains(j.Description, marker) {
			continue
		}
		if strings.TrimSpace(j.TenantID) == toTenantID {
			continue
		}
		if _, err := d.Exec(`UPDATE tbl_monitor_job SET tenant_id=?, updated_at=? WHERE id=?`, toTenantID, now, j.ID); err != nil {
			return err
		}
	}
	return nil
}

// AssignSrClusterTenant 将 SR 集群划拨到目标租户，并带走 tagged 实例（含库/账号）与匹配的监控任务。
func (d *DB) AssignSrClusterTenant(id, toTenantID string) error {
	id = strings.TrimSpace(id)
	toTenantID = strings.TrimSpace(toTenantID)
	if id == "" {
		return fmt.Errorf("集群必填")
	}
	if err := d.requireActiveTenant(toTenantID); err != nil {
		return err
	}
	cl, err := d.GetSrCluster(id)
	if err != nil {
		return err
	}
	if cl == nil {
		return fmt.Errorf("集群不存在")
	}
	if strings.TrimSpace(cl.Name) != "" {
		var n int
		err = d.QueryRow(`SELECT COUNT(1) FROM tbl_sr_clusters WHERE COALESCE(tenant_id,'')=? AND name=? AND id<>?`,
			toTenantID, cl.Name, id).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("目标租户已存在同名集群 %s", cl.Name)
		}
	}
	if strings.TrimSpace(cl.FeHost) != "" {
		var n int
		err = d.QueryRow(`SELECT COUNT(1) FROM tbl_sr_clusters WHERE COALESCE(tenant_id,'')=? AND LOWER(TRIM(fe_host))=LOWER(TRIM(?)) AND fe_query_port=? AND id<>?`,
			toTenantID, cl.FeHost, cl.FeQueryPort, id).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("目标租户已存在相同 FE 入口（%s:%d）的集群", cl.FeHost, cl.FeQueryPort)
		}
	}
	linked, err := d.instancesLinkedToSrCluster(id)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(cl.TenantID) != toTenantID {
		if _, err := d.Exec(`UPDATE tbl_sr_clusters SET tenant_id=?, updated_at=? WHERE id=?`, toTenantID, now, id); err != nil {
			return err
		}
	}
	for i := range linked {
		if err := d.assignInstanceTenantOnly(linked[i].ID, toTenantID); err != nil {
			return fmt.Errorf("关联实例 %s: %w", linked[i].Name, err)
		}
	}
	return d.assignMonitorJobsForSrCluster(id, toTenantID)
}

func (d *DB) instancesLinkedToDbDeploy(cl *DbDeployCluster) ([]Instance, error) {
	if cl == nil {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []Instance
	add := func(it *Instance) {
		if it == nil || it.ID == "" || seen[it.ID] {
			return
		}
		seen[it.ID] = true
		out = append(out, *it)
	}
	if lid := strings.TrimSpace(cl.LinkedInstanceID); lid != "" {
		it, err := d.GetInstance(lid)
		if err != nil {
			return nil, err
		}
		add(it)
	}
	nodes, err := d.ListDbDeployNodes(cl.ID)
	if err != nil {
		return nil, err
	}
	eng := strings.ToLower(strings.TrimSpace(cl.Engine))
	for _, n := range nodes {
		port := n.Port
		if port <= 0 {
			port = 3306
			if eng == DbDeployEnginePostgres {
				port = 5432
			}
		}
		it, err := d.FindInstanceByHostPortInTenant(strings.TrimSpace(cl.TenantID), eng, n.Host, port)
		if err != nil {
			return nil, err
		}
		if it != nil && (hasHAFromDbDeployTag(it.Tags) || strings.TrimSpace(cl.LinkedInstanceID) == it.ID) {
			add(it)
		}
		// 全局按 host 再找一次带 ha-from-dbdeploy 的（错位实例）
		if it == nil {
			all, err := d.FindInstanceByHostPort(eng, n.Host, port)
			if err != nil {
				return nil, err
			}
			if all != nil && hasHAFromDbDeployTag(all.Tags) {
				add(all)
			}
		}
	}
	return out, nil
}

// requireDbDeployEndpointFreeInTenant 云库 endpoint 在目标租户内不可与其它集群撞车（同引擎+host+port）。
func (d *DB) requireDbDeployEndpointFreeInTenant(cl *DbDeployCluster, toTenantID string) error {
	if cl == nil {
		return nil
	}
	host := strings.TrimSpace(cl.EndpointHost)
	if host == "" {
		return nil
	}
	port := cl.EndpointPort
	if port <= 0 {
		port = defaultDbDeployEndpointPort(cl.Engine)
	}
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_db_deploy_clusters
		WHERE COALESCE(tenant_id,'')=? AND engine=? AND id<>?
		  AND LOWER(TRIM(endpoint_host))=LOWER(TRIM(?)) AND COALESCE(endpoint_port,0)=?`,
		toTenantID, cl.Engine, cl.ID, host, port).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("目标租户已存在相同入口的集群（%s:%d）", host, port)
	}
	return nil
}

// AssignDbDeployClusterTenant 将 db-deploy 集群划拨到目标租户，并带走关联实例。
// 允许目标租户内已有同引擎同展示名集群（与 Upsert 一致）；冲突仅拦截云 endpoint / 关联实例 domain+port。
func (d *DB) AssignDbDeployClusterTenant(id, toTenantID string) error {
	id = strings.TrimSpace(id)
	toTenantID = strings.TrimSpace(toTenantID)
	if id == "" {
		return fmt.Errorf("集群必填")
	}
	if err := d.requireActiveTenant(toTenantID); err != nil {
		return err
	}
	cl, err := d.GetDbDeployCluster(id)
	if err != nil {
		return err
	}
	if cl == nil {
		return fmt.Errorf("集群不存在")
	}
	// 展示名允许同租户重复（创建路径亦未强制唯一）；真实冲突看云 endpoint / 关联实例 domain:port
	if err := d.requireDbDeployEndpointFreeInTenant(cl, toTenantID); err != nil {
		return err
	}
	linked, err := d.instancesLinkedToDbDeploy(cl)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(cl.TenantID) != toTenantID {
		if _, err := d.Exec(`UPDATE tbl_db_deploy_clusters SET tenant_id=?, updated_at=? WHERE id=?`, toTenantID, now, id); err != nil {
			return err
		}
	}
	for i := range linked {
		if err := d.assignInstanceTenantOnly(linked[i].ID, toTenantID); err != nil {
			return fmt.Errorf("关联实例 %s: %w", linked[i].Name, err)
		}
	}
	return nil
}

// AssignProxyServiceTenant 将 HAProxy/Nginx/LVS 服务划拨到目标租户。
func (d *DB) AssignProxyServiceTenant(id, toTenantID string) error {
	id = strings.TrimSpace(id)
	toTenantID = strings.TrimSpace(toTenantID)
	if id == "" {
		return fmt.Errorf("服务必填")
	}
	if err := d.requireActiveTenant(toTenantID); err != nil {
		return err
	}
	s, err := d.GetProxyService(id)
	if err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("服务不存在")
	}
	if strings.TrimSpace(s.Name) != "" {
		var n int
		err = d.QueryRow(`SELECT COUNT(1) FROM tbl_proxy_services WHERE COALESCE(tenant_id,'')=? AND kind=? AND name=? AND id<>?`,
			toTenantID, s.Kind, s.Name, id).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("目标租户已存在同类型同名代理 %s", s.Name)
		}
	}
	if vip := strings.TrimSpace(s.VIP); vip != "" {
		hit, err := d.FindProxyByVIP(vip, id)
		if err != nil {
			return err
		}
		if hit != nil && strings.TrimSpace(hit.TenantID) == toTenantID {
			return fmt.Errorf("目标租户 VIP %s 已被 %s 占用", vip, hit.Name)
		}
	}
	if strings.TrimSpace(s.TenantID) == toTenantID {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.Exec(`UPDATE tbl_proxy_services SET tenant_id=?, updated_at=? WHERE id=?`, toTenantID, now, id)
	return err
}
