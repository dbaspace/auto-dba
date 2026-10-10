package store

import (
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DbDeployConfTemplateAnyMajor 引擎通用模板（未按大版本区分时使用）。
const DbDeployConfTemplateAnyMajor = "*"

// DbDeployConfTemplate 平台参数模板（引擎 + 大版本）。
type DbDeployConfTemplate struct {
	ID        string `json:"id"`
	Engine    string `json:"engine"`
	Major     string `json:"major"`
	Content   string `json:"content"`
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// DbDeployClusterConf 集群参数覆盖（只写 key = value 行）。
type DbDeployClusterConf struct {
	ClusterID string `json:"cluster_id"`
	Content   string `json:"content"`
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ensureDbDeployConfTables 建表失败只记日志，不阻断启动（缺表时部署回退内置模板）。
func (d *DB) ensureDbDeployConfTables() {
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS tbl_db_deploy_conf_templates (
			id TEXT PRIMARY KEY,
			engine TEXT NOT NULL,
			major TEXT NOT NULL DEFAULT '*',
			content TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_db_deploy_conf_tpl_eng_major ON tbl_db_deploy_conf_templates(engine, major)`,
		`CREATE TABLE IF NOT EXISTS tbl_db_deploy_cluster_confs (
			cluster_id TEXT PRIMARY KEY,
			content TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
	} {
		if _, err := d.Exec(s); err != nil {
			log.Printf("[store] ensure db-deploy conf tables: %v", err)
			return
		}
	}
}

func normConfMajor(m string) string {
	m = strings.TrimSpace(m)
	if m == "" {
		return DbDeployConfTemplateAnyMajor
	}
	return m
}

func (d *DB) ListDbDeployConfTemplates(engine string) ([]DbDeployConfTemplate, error) {
	rows, err := d.Query(`SELECT id, engine, major, content, updated_by, updated_at
		FROM tbl_db_deploy_conf_templates WHERE engine=? ORDER BY major`, normalizeDbEngine(engine))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DbDeployConfTemplate{}
	for rows.Next() {
		var t DbDeployConfTemplate
		if err := rows.Scan(&t.ID, &t.Engine, &t.Major, &t.Content, &t.UpdatedBy, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetDbDeployConfTemplate 不存在返回 nil, nil。
func (d *DB) GetDbDeployConfTemplate(engine, major string) (*DbDeployConfTemplate, error) {
	var t DbDeployConfTemplate
	err := d.QueryRow(`SELECT id, engine, major, content, updated_by, updated_at
		FROM tbl_db_deploy_conf_templates WHERE engine=? AND major=?`, normalizeDbEngine(engine), normConfMajor(major)).
		Scan(&t.ID, &t.Engine, &t.Major, &t.Content, &t.UpdatedBy, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (d *DB) UpsertDbDeployConfTemplate(t *DbDeployConfTemplate) error {
	if t == nil {
		return errors.New("nil template")
	}
	t.Engine = normalizeDbEngine(t.Engine)
	t.Major = normConfMajor(t.Major)
	if t.Engine == "" {
		return errors.New("engine 必填")
	}
	if t.ID == "" {
		if cur, err := d.GetDbDeployConfTemplate(t.Engine, t.Major); err == nil && cur != nil {
			t.ID = cur.ID
		} else {
			t.ID = uuid.NewString()
		}
	}
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO tbl_db_deploy_conf_templates(id, engine, major, content, updated_by, updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET content=EXCLUDED.content, updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		t.ID, t.Engine, t.Major, t.Content, t.UpdatedBy, t.UpdatedAt)
	return err
}

func (d *DB) DeleteDbDeployConfTemplate(engine, major string) error {
	_, err := d.Exec(`DELETE FROM tbl_db_deploy_conf_templates WHERE engine=? AND major=?`,
		normalizeDbEngine(engine), normConfMajor(major))
	return err
}

// GetDbDeployClusterConf 不存在返回 nil, nil。
func (d *DB) GetDbDeployClusterConf(clusterID string) (*DbDeployClusterConf, error) {
	var c DbDeployClusterConf
	err := d.QueryRow(`SELECT cluster_id, content, updated_by, updated_at FROM tbl_db_deploy_cluster_confs WHERE cluster_id=?`,
		strings.TrimSpace(clusterID)).Scan(&c.ClusterID, &c.Content, &c.UpdatedBy, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertDbDeployClusterConf 内容为空即删除覆盖。
func (d *DB) UpsertDbDeployClusterConf(c *DbDeployClusterConf) error {
	if c == nil || strings.TrimSpace(c.ClusterID) == "" {
		return errors.New("cluster_id 必填")
	}
	if strings.TrimSpace(c.Content) == "" {
		return d.DeleteDbDeployClusterConf(c.ClusterID)
	}
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO tbl_db_deploy_cluster_confs(cluster_id, content, updated_by, updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(cluster_id) DO UPDATE SET content=EXCLUDED.content, updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		c.ClusterID, c.Content, c.UpdatedBy, c.UpdatedAt)
	return err
}

func (d *DB) DeleteDbDeployClusterConf(clusterID string) error {
	_, err := d.Exec(`DELETE FROM tbl_db_deploy_cluster_confs WHERE cluster_id=?`, strings.TrimSpace(clusterID))
	return err
}
