package dns

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// ZonePolicy 按域的审批与编辑权限。EditorRoleIDs 为空表示有编辑权限的人都能改。
type ZonePolicy struct {
	ZoneID          string   `json:"zone_id"`
	RequireApproval bool     `json:"require_approval"`
	EditorRoleIDs   []string `json:"editor_role_ids"`
	Env             string   `json:"env"`
	UpdatedBy       string   `json:"updated_by"`
	UpdatedTime     int64    `json:"updated_time"`
}

func (s *Store) ListPolicies(ctx context.Context) (map[string]ZonePolicy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT zone_id, require_approval, editor_role_ids, env, updated_by, updated_time FROM tbl_dns_zone_policies`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ZonePolicy{}
	for rows.Next() {
		var p ZonePolicy
		var raw []byte
		if err := rows.Scan(&p.ZoneID, &p.RequireApproval, &raw, &p.Env, &p.UpdatedBy, &p.UpdatedTime); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &p.EditorRoleIDs)
		if p.EditorRoleIDs == nil {
			p.EditorRoleIDs = []string{}
		}
		out[p.ZoneID] = p
	}
	return out, rows.Err()
}

func (s *Store) SavePolicy(ctx context.Context, p ZonePolicy, actor string) error {
	clean := []string{}
	for _, id := range p.EditorRoleIDs {
		if id = strings.TrimSpace(id); id != "" {
			clean = append(clean, id)
		}
	}
	raw, _ := json.Marshal(clean)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tbl_dns_zone_policies(zone_id, require_approval, editor_role_ids, env, updated_by, updated_time)
VALUES($1,$2,$3::jsonb,$4,$5,$6)
ON CONFLICT (zone_id) DO UPDATE SET require_approval=EXCLUDED.require_approval, editor_role_ids=EXCLUDED.editor_role_ids,
  env=EXCLUDED.env, updated_by=EXCLUDED.updated_by, updated_time=EXCLUDED.updated_time`,
		p.ZoneID, p.RequireApproval, string(raw), strings.TrimSpace(p.Env), actor, time.Now().Unix())
	return err
}

// AffectedZones 一组变更涉及的解析域。解析范围不属于任何域，不计入。
func (s *Store) AffectedZones(ctx context.Context, ops []Op) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, op := range ops {
		switch op.Object {
		case ObjectZone:
			add(op.ID)
		case ObjectRecord:
			if len(op.After) > 0 {
				var r RecordSnap
				if json.Unmarshal(op.After, &r) == nil {
					add(r.ZoneID)
				}
			}
			if op.ID != "" {
				cur, err := getRecordSnap(ctx, s.db, op.ID)
				if err != nil {
					return nil, err
				}
				if cur != nil {
					add(cur.ZoneID)
				}
			}
		}
	}
	return out, nil
}
