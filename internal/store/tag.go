package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MonitorLabelTag 监控目标注册用的标签名定义（含可选候选值）。
type MonitorLabelTag struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Values      []string `json:"values"`
	Description string   `json:"description,omitempty"`
	SortOrder   int      `json:"sort_order"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

func encodeTagValues(vals []string) string {
	clean := make([]string, 0, len(vals))
	seen := map[string]struct{}{}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		clean = append(clean, v)
	}
	b, _ := json.Marshal(clean)
	return string(b)
}

func decodeTagValues(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []string{}
	}
	clean := make([]string, 0, len(out))
	for _, v := range out {
		v = strings.TrimSpace(v)
		if v != "" {
			clean = append(clean, v)
		}
	}
	return clean
}

func (d *DB) ListMonitorLabelTags() ([]MonitorLabelTag, error) {
	rows, err := d.Query(`SELECT id, key, name, values_json, description, sort_order, created_at, updated_at
		FROM tbl_monitor_label_tags ORDER BY sort_order ASC, key ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MonitorLabelTag, 0)
	for rows.Next() {
		var t MonitorLabelTag
		var valuesJSON string
		if err := rows.Scan(&t.ID, &t.Key, &t.Name, &valuesJSON, &t.Description, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Values = decodeTagValues(valuesJSON)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) GetMonitorLabelTag(id string) (*MonitorLabelTag, error) {
	var t MonitorLabelTag
	var valuesJSON string
	err := d.QueryRow(`SELECT id, key, name, values_json, description, sort_order, created_at, updated_at
		FROM tbl_monitor_label_tags WHERE id=?`, id).
		Scan(&t.ID, &t.Key, &t.Name, &valuesJSON, &t.Description, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Values = decodeTagValues(valuesJSON)
	return &t, nil
}

func (d *DB) CreateMonitorLabelTag(t *MonitorLabelTag) error {
	t.Key = strings.TrimSpace(t.Key)
	t.Name = strings.TrimSpace(t.Name)
	if t.Key == "" {
		return errors.New("标签名 key 必填")
	}
	if t.Name == "" {
		t.Name = t.Key
	}
	if t.Values == nil {
		t.Values = []string{}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := d.Exec(
		`INSERT INTO tbl_monitor_label_tags(id, key, name, values_json, description, sort_order, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		t.ID, t.Key, t.Name, encodeTagValues(t.Values), t.Description, t.SortOrder, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (d *DB) UpdateMonitorLabelTag(t *MonitorLabelTag) error {
	t.Key = strings.TrimSpace(t.Key)
	t.Name = strings.TrimSpace(t.Name)
	if t.Key == "" {
		return errors.New("标签名 key 必填")
	}
	if t.Name == "" {
		t.Name = t.Key
	}
	if t.Values == nil {
		t.Values = []string{}
	}
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`UPDATE tbl_monitor_label_tags SET key=?, name=?, values_json=?, description=?, sort_order=?, updated_at=? WHERE id=?`,
		t.Key, t.Name, encodeTagValues(t.Values), t.Description, t.SortOrder, t.UpdatedAt, t.ID,
	)
	return err
}

func (d *DB) DeleteMonitorLabelTag(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_monitor_label_tags WHERE id=?`, id)
	return err
}

// EnsureDefaultMonitorLabelTags 空表时写入与原先前端硬编码一致的默认标签。
func (d *DB) EnsureDefaultMonitorLabelTags() error {
	var n int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_monitor_label_tags`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	defaults := []MonitorLabelTag{
		{ID: "mlt-sc-tmp", Key: "sc_tmp", Name: "采集模板", Values: []string{"direct", "proxy", "k8s"}, Description: "监控采集类型", SortOrder: 10},
		{ID: "mlt-domain", Key: "domain", Name: "域名", Values: []string{}, Description: "业务域名", SortOrder: 20},
		{ID: "mlt-idc", Key: "idc", Name: "IDC/环境", Values: []string{}, Description: "候选值为空时回退环境管理编码", SortOrder: 30},
		{ID: "mlt-app", Key: "app", Name: "应用组件", Values: []string{"fe-StarRocks", "be-StarRocks", "master-server", "redis"}, Description: "组件类型", SortOrder: 40},
		{ID: "mlt-env", Key: "env", Name: "环境", Values: []string{}, Description: "候选值为空时回退环境管理编码", SortOrder: 50},
	}
	for i := range defaults {
		t := defaults[i]
		t.CreatedAt, t.UpdatedAt = now, now
		if err := d.CreateMonitorLabelTag(&t); err != nil {
			return err
		}
	}
	return nil
}
