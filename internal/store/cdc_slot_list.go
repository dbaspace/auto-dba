package store

import "strings"

// ensureCDCListExtra 在库内把 CDC 正文收成同步列表配对用的小 JSON。坏 JSON 返回 NULL，不让整次列表失败。
func (d *DB) ensureCDCListExtra() error {
	_, err := d.Exec(`
CREATE OR REPLACE FUNCTION cdc_list_extra(description text)
RETURNS jsonb
LANGUAGE plpgsql
IMMUTABLE
AS $cdc_list_extra$
DECLARE
  pos int;
  raw text;
  doc jsonb;
  cfg jsonb;
  host text;
  port text;
  slim jsonb;
BEGIN
  IF description IS NULL OR btrim(description) = '' THEN
    RETURN NULL;
  END IF;
  pos := position('-- auto-dba-cdc:' IN description);
  IF pos <= 0 THEN
    RETURN NULL;
  END IF;
  raw := btrim(substring(description FROM pos + 16));
  IF raw = '' THEN
    RETURN NULL;
  END IF;
  BEGIN
    doc := raw::jsonb;
  EXCEPTION WHEN others THEN
    RETURN NULL;
  END;
  IF doc IS NULL OR jsonb_typeof(doc) <> 'object' THEN
    RETURN NULL;
  END IF;
  cfg := doc->'debezium_config';
  IF jsonb_typeof(cfg) = 'string' THEN
    BEGIN
      cfg := (doc->>'debezium_config')::jsonb;
    EXCEPTION WHEN others THEN
      cfg := NULL;
    END;
  END IF;
  IF jsonb_typeof(cfg) = 'object' THEN
    host := NULLIF(btrim(cfg->>'database.hostname'), '');
    port := NULLIF(btrim(cfg->>'database.port'), '');
  END IF;
  slim := jsonb_strip_nulls(jsonb_build_object(
    'slot_name', NULLIF(btrim(doc->>'slot_name'), ''),
    'connector_name', NULLIF(btrim(doc->>'connector_name'), ''),
    'publication_name', NULLIF(btrim(doc->>'publication_name'), ''),
    'connect_base_url', NULLIF(btrim(doc->>'connect_base_url'), ''),
    'worker_url', NULLIF(btrim(doc->>'worker_url'), ''),
    'topic_prefix', NULLIF(btrim(doc->>'topic_prefix'), ''),
    'source_host', NULLIF(btrim(doc->>'source_host'), ''),
    'source_port', NULLIF(btrim(doc->>'source_port'), '')
  ));
  IF host IS NOT NULL OR port IS NOT NULL THEN
    slim := slim || jsonb_build_object('debezium_config', jsonb_strip_nulls(jsonb_build_object(
      'database.hostname', host,
      'database.port', port
    )));
  END IF;
  IF jsonb_typeof(doc->'shard_db_bindings') = 'array' THEN
    slim := slim || jsonb_build_object('shard_db_bindings', doc->'shard_db_bindings');
  END IF;
  RETURN slim;
END;
$cdc_list_extra$`)
	return err
}

// ListCDCSlotTasksInTenant 同步列表配对用。只带回小字段拼出的正文，不读取 debezium 配置和表清单。
func (d *DB) ListCDCSlotTasksInTenant(tenantID string) ([]CDCTask, []string, error) {
	q := `SELECT t.id, t.name, t.task_type, COALESCE(t.source_instance_id,''), COALESCE(t.source_db,''), COALESCE(cdc_list_extra(t.description)::text, '')
		FROM tbl_cdc_tasks t WHERE t.status<>?`
	args := []any{StatusSuperseded}
	q, args = appendTenantFilter(q, args, tenantID)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var tasks []CDCTask
	seen := map[string]struct{}{}
	var sourceIDs []string
	for rows.Next() {
		var t CDCTask
		var extra string
		if err := rows.Scan(&t.ID, &t.Name, &t.TaskType, &t.SourceID, &t.SourceDB, &extra); err != nil {
			return nil, nil, err
		}
		extra = strings.TrimSpace(extra)
		if extra != "" && extra != "null" && extra != "{}" {
			t.Description = "-- auto-dba-cdc:" + extra
		}
		tasks = append(tasks, t)
		id := strings.TrimSpace(t.SourceID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		sourceIDs = append(sourceIDs, id)
	}
	if tasks == nil {
		tasks = []CDCTask{}
	}
	return tasks, sourceIDs, rows.Err()
}

// ListPostgresInstanceEndpoints 只取同步列表配对需要的主机、端口和域名，不读密码。
func (d *DB) ListPostgresInstanceEndpoints(ids []string) ([]Instance, error) {
	if len(ids) == 0 {
		return []Instance{}, nil
	}
	var out []Instance
	const chunk = 400
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[start:end]
		ph := make([]string, len(part))
		args := make([]any, len(part))
		for i, id := range part {
			ph[i] = "?"
			args[i] = id
		}
		q := `SELECT id, COALESCE(host,''), port, COALESCE(domain_name,''), COALESCE(db_type,'')
			FROM tbl_instances WHERE id IN (` + strings.Join(ph, ",") + `)`
		rows, err := d.Query(q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var it Instance
			if err := rows.Scan(&it.ID, &it.Host, &it.Port, &it.DomainName, &it.DBType); err != nil {
				rows.Close()
				return nil, err
			}
			if NormalizeDBType(it.DBType) != "postgres" {
				continue
			}
			out = append(out, it)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	if out == nil {
		out = []Instance{}
	}
	return out, nil
}
