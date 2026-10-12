package store

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// 启动引导版本：修改 migrate.go / seed.go 后请递增对应常量，否则已初始化库会跳过重跑。
const (
	SchemaVersion = "2026-10-12.copilot-session-title"
	SeedVersion   = "2026-10-12.kb-learn-align"
)

const (
	metaSchemaVersion = "schema_version"
	metaSeedVersion   = "seed_version"
)

func (d *DB) ensureSchemaMetaTable() error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS tbl_schema_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	return err
}

func (d *DB) schemaMetaGet(key string) (string, error) {
	var v string
	err := d.QueryRow(`SELECT value FROM tbl_schema_meta WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (d *DB) schemaMetaSet(key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`
		INSERT INTO tbl_schema_meta(key, value, updated_at) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=EXCLUDED.updated_at`,
		key, value, now,
	)
	return err
}

func forceBootstrap() bool {
	v := strings.TrimSpace(os.Getenv("AUTO_DBA_FORCE_BOOTSTRAP"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// migrateFast 在 schema 已是当前版本时跳过全量 DDL（CREATE IF NOT EXISTS 数百条），显著缩短启动。
func (d *DB) migrateFast() error {
	t0 := time.Now()
	if err := d.ensureSchemaMetaTable(); err != nil {
		return err
	}
	if !forceBootstrap() {
		cur, err := d.schemaMetaGet(metaSchemaVersion)
		if err != nil {
			return err
		}
		if cur == SchemaVersion {
			// 轻量幂等：租户内唯一约束 / AIOps 案例列 / Copilot title 不依赖升版本，避免 skip 路径漏跑。
			if err := d.ensureTenantScopedUniques(); err != nil {
				return err
			}
			if err := d.ensureSqlInsightCaseColumns(); err != nil {
				return err
			}
			if err := d.ensureCopilotSessionColumns(); err != nil {
				return err
			}
			log.Printf("migrate: skip (schema_version=%s) in %s", SchemaVersion, time.Since(t0).Round(time.Millisecond))
			return nil
		}
	}
	if err := d.migrate(); err != nil {
		return err
	}
	if err := d.schemaMetaSet(metaSchemaVersion, SchemaVersion); err != nil {
		return fmt.Errorf("set schema_version: %w", err)
	}
	log.Printf("migrate: applied schema_version=%s in %s", SchemaVersion, time.Since(t0).Round(time.Millisecond))
	return nil
}

// seedFast 在 seed 已是当前版本时跳过菜单/RBAC 全量 ensure（远程 PG 上可达数十秒）。
// 轻量幂等种子（如默认告警规则）在 skip 路径仍会执行，避免仅靠升版本才写入。
func (d *DB) seedFast() error {
	t0 := time.Now()
	if err := d.ensureSchemaMetaTable(); err != nil {
		return err
	}
	if !forceBootstrap() {
		cur, err := d.schemaMetaGet(metaSeedVersion)
		if err != nil {
			return err
		}
		if cur == SeedVersion {
			if err := d.EnsureDefaultAlertRules(); err != nil {
				return err
			}
			now := time.Now().UTC().Format(time.RFC3339)
			if err := d.ensureAlertMenus(now); err != nil {
				return err
			}
			if err := d.SyncDefaultTenantMemberships(); err != nil {
				return err
			}
			log.Printf("seed: skip (seed_version=%s) in %s", SeedVersion, time.Since(t0).Round(time.Millisecond))
			return nil
		}
	}
	if err := d.seed(); err != nil {
		return err
	}
	if err := d.SyncDefaultTenantMemberships(); err != nil {
		return err
	}
	if err := d.schemaMetaSet(metaSeedVersion, SeedVersion); err != nil {
		return fmt.Errorf("set seed_version: %w", err)
	}
	log.Printf("seed: applied seed_version=%s in %s", SeedVersion, time.Since(t0).Round(time.Millisecond))
	return nil
}
