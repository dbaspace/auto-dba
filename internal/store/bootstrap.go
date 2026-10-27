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
	SchemaVersion = "2026-10-20.k8s-mgmt"
	SeedVersion   = "2026-10-20.k8s-menus"
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
			// 轻量幂等：租户内唯一约束 / AIOps 案例列 / Copilot title / 工单 domain 不依赖升版本，避免 skip 路径漏跑。
			if err := d.ensureTenantScopedUniques(); err != nil {
				return err
			}
			if err := d.ensureSqlInsightCaseColumns(); err != nil {
				return err
			}
			if err := d.ensureCopilotSessionColumns(); err != nil {
				return err
			}
			if err := d.ensureIssueDomainColumn(); err != nil {
				return err
			}
			if err := d.ensureIssueExecProgressColumn(); err != nil {
				return err
			}
			if err := d.ensureCloudSchema(); err != nil {
				return err
			}
			if err := d.ensureSQLExecutorTable(); err != nil {
				return err
			}
			if err := d.ensureSQLExecutorDeployColumns(); err != nil {
				return err
			}
			if err := d.ensureAccessDomainsTable(); err != nil {
				return err
			}
			if _, err := d.BackfillAccessDomainsFromInstances(); err != nil {
				return err
			}
			if err := d.ensureUserSessionsTable(); err != nil {
				return err
			}
			if err := d.ensureJobRegTables(); err != nil {
				return err
			}
			// Nacos 控制台凭据表须在 skip 路径幂等补齐
			if err := d.ensureNacosAdminTable(); err != nil {
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
			// 租户动作菜单（含新增 delete）须在 skip 路径幂等补齐，否则仅升 SeedVersion 才写入
			if err := d.ensureTenantMenus(now); err != nil {
				return err
			}
			if err := d.ensureAccessDomainsMenu(now); err != nil {
				return err
			}
			// 平台资源申请菜单须在 skip 路径幂等补齐（否则仅升 SeedVersion 才写入）
			if err := d.ensureResourceApplyMenu(now); err != nil {
				return err
			}
			// 登录会话菜单须在 skip 路径幂等补齐
			if err := d.ensureLoginSessionsMenu(now); err != nil {
				return err
			}
			if err := d.ensureScheduledJobsMenu(now); err != nil {
				return err
			}
			if err := d.ensureSQLExecutorMenu(now); err != nil {
				return err
			}
			if err := d.ensureScheduledJobsActionMenus(now); err != nil {
				return err
			}
			if err := d.ensureDNSMenus(now); err != nil {
				return err
			}
			// 实例总览须在 skip 路径幂等补齐，否则已有库不会写入菜单
			if err := d.ensureInstanceOverviewMenu(now); err != nil {
				return err
			}
			if err := d.ensureCloudMenus(now); err != nil {
				return err
			}
			if err := d.ensureSqlInsightActionMenus(now); err != nil {
				return err
			}
			// Nacos 配置 / Nacos 管理菜单须在 skip 路径幂等补齐，否则已有库看不到侧栏入口。
			// 先 Ensure 叶子（含 Nacos），再 ensure 配置中心分组把监控运维叶子统一挂到 m-cfg-obs。
			if err := d.ensureMonitorOpsMenus(now); err != nil {
				return err
			}
			if err := d.ensureConfigCenterGroups(now); err != nil {
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
