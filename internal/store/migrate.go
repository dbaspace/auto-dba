package store

func (d *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			role TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_instances (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			domain_name TEXT NOT NULL,
			port INTEGER NOT NULL,
			db_type TEXT NOT NULL,
			idc TEXT NOT NULL DEFAULT 'default',
			region_id TEXT,
			env_id TEXT,
			host TEXT NOT NULL,
			username TEXT,
			password TEXT,
			approvers TEXT NOT NULL DEFAULT '[]',
			description TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(domain_name, port)
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_regions (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_environments (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_region_envs (
			region_id TEXT NOT NULL,
			env_id TEXT NOT NULL,
			PRIMARY KEY (region_id, env_id)
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_databases (
			id TEXT PRIMARY KEY,
			instance_id TEXT NOT NULL,
			db_name TEXT NOT NULL,
			approvers TEXT NOT NULL DEFAULT '[]',
			visibility TEXT NOT NULL DEFAULT 'global',
			access_users TEXT NOT NULL DEFAULT '[]',
			role_ids TEXT NOT NULL DEFAULT '[]',
			tags TEXT NOT NULL DEFAULT '[]',
			domains TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(instance_id, db_name)
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_global_args (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			description TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_issues (
			id TEXT PRIMARY KEY,
			ticket_no TEXT NOT NULL UNIQUE,
			issue_type TEXT NOT NULL,
			instance_id TEXT,
			database_id TEXT,
			db_name TEXT,
			sql_content TEXT,
			description TEXT,
			status TEXT NOT NULL,
			submitter TEXT NOT NULL,
			assigned_to TEXT,
			reject_reason TEXT,
			need_rollback TEXT,
			scheduled_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_type ON tbl_issues(issue_type)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_status ON tbl_issues(status)`,
		`CREATE TABLE IF NOT EXISTS tbl_cdc_tasks (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			task_type TEXT NOT NULL,
			status TEXT NOT NULL,
			source_instance_id TEXT,
			target_instance_id TEXT,
			source_db TEXT,
			target_db TEXT,
			parent_id TEXT,
			approval_status TEXT NOT NULL DEFAULT 'none',
			issue_id TEXT,
			description TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sync_slots (
			id TEXT PRIMARY KEY,
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			slot_name TEXT NOT NULL,
			database_name TEXT,
			active INTEGER NOT NULL DEFAULT 0,
			restart_lsn TEXT,
			plugin TEXT,
			updated_at TEXT NOT NULL,
			UNIQUE(host, port, slot_name)
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_migration_tasks (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			status TEXT NOT NULL,
			source_instance_id TEXT,
			target_instance_id TEXT,
			source_db TEXT,
			target_db TEXT,
			description TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_schema_compare_tasks (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			left_instance_id TEXT NOT NULL,
			right_instance_id TEXT NOT NULL,
			left_db TEXT NOT NULL,
			right_db TEXT NOT NULL,
			left_schema TEXT NOT NULL DEFAULT 'public',
			right_schema TEXT NOT NULL DEFAULT 'public',
			tables_json TEXT NOT NULL DEFAULT '[]',
			engine TEXT NOT NULL DEFAULT '',
			summary_json TEXT NOT NULL DEFAULT '{}',
			result_json TEXT NOT NULL DEFAULT '',
			fix_sql TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			progress INTEGER NOT NULL DEFAULT 0,
			progress_message TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_flashback_tasks (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			status TEXT NOT NULL,
			instance_id TEXT,
			db_name TEXT,
			start_time TEXT,
			end_time TEXT,
			sql_preview TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_archive_tasks (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			status TEXT NOT NULL,
			source_instance_id TEXT,
			source_db TEXT,
			table_name TEXT,
			condition_sql TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_kafka_orders (
			id TEXT PRIMARY KEY,
			ticket_no TEXT NOT NULL,
			cluster TEXT,
			topic TEXT,
			action TEXT,
			status TEXT NOT NULL,
			submitter TEXT,
			assigned_to TEXT,
			description TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_multilang_tasks (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			status TEXT NOT NULL,
			instance_id TEXT,
			template TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_insight_jobs (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			instance_id TEXT,
			db_name TEXT,
			status TEXT NOT NULL,
			summary TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_roles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			code TEXT NOT NULL UNIQUE,
			description TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_menus (
			id TEXT PRIMARY KEY,
			parent_id TEXT,
			title TEXT NOT NULL,
			urn TEXT,
			path TEXT,
			permission_desc TEXT,
			icon TEXT,
			sort_order INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_role_menus (
			role_id TEXT NOT NULL,
			menu_id TEXT NOT NULL,
			PRIMARY KEY (role_id, menu_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := d.db.Exec(s); err != nil {
			return err
		}
	}
	return d.ensureUserRoleID()
}

func (d *DB) ensureUserRoleID() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_users' AND column_name = 'role_id'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_users ADD COLUMN role_id TEXT`); err != nil {
			return err
		}
	}
	return d.ensureUserRolesTable()
}

func (d *DB) ensureUserRolesTable() error {
	if _, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS tbl_user_roles (
			user_id TEXT NOT NULL,
			role_id TEXT NOT NULL,
			PRIMARY KEY (user_id, role_id)
		)`); err != nil {
		return err
	}
	// 回填：把历史单角色写入关联表（仅补缺失行）
	_, err := d.db.Exec(`
		INSERT INTO tbl_user_roles(user_id, role_id)
		SELECT id, role_id FROM tbl_users
		WHERE role_id IS NOT NULL AND TRIM(role_id) <> ''
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	return d.ensureMenuExtraColumns()
}

func (d *DB) ensureMenuExtraColumns() error {
	for _, col := range []string{"urn", "permission_desc", "icon"} {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.columns
			WHERE table_name = 'tbl_menus' AND column_name = $1
		`, col).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := d.db.Exec(`ALTER TABLE tbl_menus ADD COLUMN ` + col + ` TEXT`); err != nil {
				return err
			}
		}
	}
	return d.ensureInstanceRegionColumns()
}

func (d *DB) ensureInstanceRegionColumns() error {
	for _, col := range []string{"region_id", "env_id"} {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.columns
			WHERE table_name = 'tbl_instances' AND column_name = $1
		`, col).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := d.db.Exec(`ALTER TABLE tbl_instances ADD COLUMN ` + col + ` TEXT`); err != nil {
				return err
			}
		}
	}
	return d.ensureInstanceDomainsColumn()
}

func (d *DB) ensureInstanceDomainsColumn() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_instances' AND column_name = 'domains'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_instances ADD COLUMN domains TEXT`); err != nil {
			return err
		}
	}
	return d.ensureInstanceTagsColumn()
}

func (d *DB) ensureInstanceTagsColumn() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_instances' AND column_name = 'tags'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_instances ADD COLUMN tags TEXT`); err != nil {
			return err
		}
	}
	if err := d.ensureInstanceACLColumns(); err != nil {
		return err
	}
	return d.ensureDatabaseMetaColumns()
}

func (d *DB) ensureInstanceACLColumns() error {
	for _, spec := range []struct {
		col, def string
	}{
		{"visibility", "TEXT NOT NULL DEFAULT 'global'"},
		{"access_users", "TEXT NOT NULL DEFAULT '[]'"},
		{"role_ids", "TEXT NOT NULL DEFAULT '[]'"},
	} {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.columns
			WHERE table_name = 'tbl_instances' AND column_name = $1
		`, spec.col).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := d.db.Exec(`ALTER TABLE tbl_instances ADD COLUMN ` + spec.col + ` ` + spec.def); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *DB) ensureDatabaseMetaColumns() error {
	for _, spec := range []struct {
		col, def string
	}{
		{"visibility", "TEXT NOT NULL DEFAULT 'global'"},
		{"access_users", "TEXT NOT NULL DEFAULT '[]'"},
		{"role_ids", "TEXT NOT NULL DEFAULT '[]'"},
		{"tags", "TEXT NOT NULL DEFAULT '[]'"},
		{"domains", "TEXT NOT NULL DEFAULT '[]'"},
	} {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.columns
			WHERE table_name = 'tbl_databases' AND column_name = $1
		`, spec.col).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := d.db.Exec(`ALTER TABLE tbl_databases ADD COLUMN ` + spec.col + ` ` + spec.def); err != nil {
				return err
			}
		}
	}
	return d.ensureConnectorsTable()
}

func (d *DB) ensureConnectorsTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_connectors (
		id TEXT PRIMARY KEY,
		dbz_host TEXT NOT NULL,
		env TEXT NOT NULL,
		kafka_host TEXT NOT NULL,
		message_schema_mode TEXT NOT NULL DEFAULT 'without_schema',
		remark TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	return d.ensureAccountsTable()
}

func (d *DB) ensureAccountsTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_db_accounts (
		id TEXT PRIMARY KEY,
		instance_id TEXT NOT NULL,
		database_id TEXT,
		db_name TEXT NOT NULL,
		db_type TEXT NOT NULL,
		idc TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL,
		password TEXT NOT NULL DEFAULT '',
		permissions TEXT NOT NULL DEFAULT '[]',
		description TEXT,
		ticket_id TEXT,
		created_by TEXT,
		updated_by TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(instance_id, username, db_name)
	)`)
	if err != nil {
		return err
	}
	return d.ensureReviewRulesTable()
}

func (d *DB) ensureReviewRulesTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_review_rules (
		id TEXT PRIMARY KEY,
		engine TEXT NOT NULL,
		category TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL,
		label TEXT NOT NULL DEFAULT '',
		value TEXT NOT NULL DEFAULT '',
		sort_order INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(engine, name)
	)`)
	if err != nil {
		return err
	}
	if err := d.ensureReviewRulesDefaults(); err != nil {
		return err
	}
	return d.ensureIssueExecResultColumn()
}

func (d *DB) ensureIssueExecResultColumn() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_issues' AND column_name = 'execute_result'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_issues ADD COLUMN execute_result TEXT`); err != nil {
			return err
		}
	}
	if err := d.ensureIssueAssigneeTransferHistoryColumn(); err != nil {
		return err
	}
	if err := d.ensureIssueDomainColumn(); err != nil {
		return err
	}
	if err := d.ensureLoginLogsTable(); err != nil {
		return err
	}
	return d.ensureUserSessionsTable()
}

func (d *DB) ensureIssueDomainColumn() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_issues' AND column_name = 'domain'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_issues ADD COLUMN domain TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureIssueAssigneeTransferHistoryColumn() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_issues' AND column_name = 'assignee_transfer_history'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_issues ADD COLUMN assignee_transfer_history TEXT NOT NULL DEFAULT '[]'`); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureLoginLogsTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_login_logs (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		username TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		success INTEGER NOT NULL DEFAULT 0,
		message TEXT NOT NULL DEFAULT '',
		ip TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT '',
		logged_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_login_logs_logged_at ON tbl_login_logs(logged_at DESC)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_login_logs_username ON tbl_login_logs(username)`)
	if err != nil {
		return err
	}
	if err := d.ensureUserSourceColumn(); err != nil {
		return err
	}
	if err := d.ensureUserProfileColumn(); err != nil {
		return err
	}
	if err := d.ensureUserLoginStatusColumns(); err != nil {
		return err
	}
	if err := d.ensurePGActivitySnapshotsTable(); err != nil {
		return err
	}
	if err := d.ensureMigrationSchema(); err != nil {
		return err
	}
	if err := d.ensureArchiveSchema(); err != nil {
		return err
	}
	if err := d.ensureFlashbackSchema(); err != nil {
		return err
	}
	if err := d.ensureQueryAuditsTable(); err != nil {
		return err
	}
	if err := d.ensureQueryFavoritesTable(); err != nil {
		return err
	}
	if err := d.ensureRolesUpdatedAt(); err != nil {
		return err
	}
	if err := d.ensureLoginAttemptTable(); err != nil {
		return err
	}
	if err := d.ensureTerminalSessionsTable(); err != nil {
		return err
	}
	if err := d.ensurePGSpaceTables(); err != nil {
		return err
	}
	if err := d.ensureAWRTables(); err != nil {
		return err
	}
	if err := d.ensureSqlInsightTables(); err != nil {
		return err
	}
	if err := d.ensureHATables(); err != nil {
		return err
	}
	if err := d.ensureBackupTables(); err != nil {
		return err
	}
	if err := d.ensureSrClusterTables(); err != nil {
		return err
	}
	if err := d.ensureDbDeployTables(); err != nil {
		return err
	}
	if err := d.ensureMonitorRegistryTables(); err != nil {
		return err
	}
	if err := d.ensureOSSSettingsTable(); err != nil {
		return err
	}
	if err := d.ensureOSSUploadTables(); err != nil {
		return err
	}
	if err := d.ensureTodoTables(); err != nil {
		return err
	}
	if err := d.ensureAlertTables(); err != nil {
		return err
	}
	if err := d.ensureCmdbTables(); err != nil {
		return err
	}
	if err := d.ensureCopilotTables(); err != nil {
		return err
	}
	return d.ensureTenancy()
}

func (d *DB) ensureOSSSettingsTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_oss_settings (
		id TEXT PRIMARY KEY,
		environment TEXT NOT NULL DEFAULT '',
		cloud_product TEXT NOT NULL DEFAULT '',
		endpoint TEXT NOT NULL DEFAULT '',
		bucket TEXT NOT NULL DEFAULT '',
		prefix TEXT NOT NULL DEFAULT 'migration',
		access_key TEXT NOT NULL DEFAULT '',
		secret_enc TEXT NOT NULL DEFAULT '',
		retention_days INTEGER NOT NULL DEFAULT 7,
		chunk_size_mb INTEGER NOT NULL DEFAULT 100,
		created_by TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		UNIQUE (environment)
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`ALTER TABLE tbl_oss_settings ADD COLUMN IF NOT EXISTS cloud_product TEXT NOT NULL DEFAULT ''`)
	return err
}

func (d *DB) ensureMonitorRegistryTables() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_monitor_job (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		template TEXT NOT NULL DEFAULT '直连类型监控模板',
		collector TEXT NOT NULL DEFAULT '',
		scheme TEXT NOT NULL DEFAULT 'http',
		status TEXT NOT NULL DEFAULT 'active',
		monitor_type TEXT NOT NULL DEFAULT 'direct',
		metrics_path TEXT NOT NULL DEFAULT '/metrics',
		scrape_interval TEXT NOT NULL DEFAULT '1m',
		scrape_timeout TEXT NOT NULL DEFAULT '10s',
		labels_json TEXT NOT NULL DEFAULT '{}',
		description TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		UNIQUE (name)
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_monitor_target_group (
		id TEXT PRIMARY KEY,
		job_id TEXT NOT NULL,
		endpoints TEXT NOT NULL DEFAULT '',
		labels_json TEXT NOT NULL DEFAULT '{}',
		description TEXT NOT NULL DEFAULT '',
		sort_order INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_monitor_tg_job ON tbl_monitor_target_group(job_id)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_fe_admin (
		id TEXT PRIMARY KEY,
		task_name TEXT NOT NULL DEFAULT '',
		environment TEXT NOT NULL DEFAULT '',
		domain TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 0,
		fe_node TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		password_enc TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		UNIQUE (environment, domain, username)
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_ds_admin (
		id TEXT PRIMARY KEY,
		environment TEXT NOT NULL DEFAULT '',
		login_url TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		password_enc TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		UNIQUE (environment)
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_monitor_label_tags (
		id TEXT PRIMARY KEY,
		key TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL DEFAULT '',
		values_json TEXT NOT NULL DEFAULT '[]',
		description TEXT NOT NULL DEFAULT '',
		sort_order INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	return err
}

func (d *DB) ensureAWRTables() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_db_awr_reports (
		id            BIGSERIAL     PRIMARY KEY,
		report_date   DATE          NOT NULL,
		instance_id   VARCHAR(255),
		instance_name VARCHAR(255),
		host          VARCHAR(255)  NOT NULL,
		port          INTEGER       NOT NULL,
		source        VARCHAR(32)   NOT NULL DEFAULT 'unknown',
		period_start  TIMESTAMPTZ   NOT NULL,
		period_end    TIMESTAMPTZ   NOT NULL,
		status        VARCHAR(32)   NOT NULL DEFAULT 'success',
		severity      VARCHAR(16)   NOT NULL DEFAULT 'none',
		anomaly_count INTEGER       NOT NULL DEFAULT 0,
		anomalies     JSONB         NOT NULL DEFAULT '[]'::jsonb,
		error_message TEXT,
		sections      JSONB         NOT NULL DEFAULT '{}'::jsonb,
		created_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
		updated_at    TIMESTAMPTZ,
		UNIQUE (report_date, host, port)
	)`)
	if err != nil {
		return err
	}
	for _, alt := range []string{
		`ALTER TABLE tbl_db_awr_reports ADD COLUMN IF NOT EXISTS source        VARCHAR(32)  NOT NULL DEFAULT 'unknown'`,
		`ALTER TABLE tbl_db_awr_reports ADD COLUMN IF NOT EXISTS severity      VARCHAR(16)  NOT NULL DEFAULT 'none'`,
		`ALTER TABLE tbl_db_awr_reports ADD COLUMN IF NOT EXISTS anomaly_count INTEGER      NOT NULL DEFAULT 0`,
		`ALTER TABLE tbl_db_awr_reports ADD COLUMN IF NOT EXISTS anomalies     JSONB        NOT NULL DEFAULT '[]'::jsonb`,
	} {
		if _, err := d.db.Exec(alt); err != nil {
			return err
		}
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_awr_report_date     ON tbl_db_awr_reports(report_date)`,
		`CREATE INDEX IF NOT EXISTS idx_awr_report_instance ON tbl_db_awr_reports(instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_awr_report_hostport ON tbl_db_awr_reports(host, port)`,
		`CREATE INDEX IF NOT EXISTS idx_awr_report_severity ON tbl_db_awr_reports(report_date, severity)`,
	} {
		if _, err := d.db.Exec(idx); err != nil {
			return err
		}
	}

	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_db_awr_generate_jobs (
		id               VARCHAR(255) PRIMARY KEY,
		scope            VARCHAR(16)  NOT NULL,
		report_date      DATE         NOT NULL,
		instance_id      VARCHAR(255),
		host             VARCHAR(255),
		port             INTEGER,
		state            VARCHAR(16)  NOT NULL,
		instances_total  INTEGER      NOT NULL DEFAULT 0,
		instances_done   INTEGER      NOT NULL DEFAULT 0,
		current_instance VARCHAR(255),
		current_stage    VARCHAR(32),
		reports_written  INTEGER      NOT NULL DEFAULT 0,
		failed_count     INTEGER      NOT NULL DEFAULT 0,
		error            TEXT,
		created_by       VARCHAR(255),
		started_at       TIMESTAMPTZ,
		updated_at       TIMESTAMPTZ,
		finished_at      TIMESTAMPTZ,
		created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		return err
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_awr_gen_job_state   ON tbl_db_awr_generate_jobs(state)`,
		`CREATE INDEX IF NOT EXISTS idx_awr_gen_job_date    ON tbl_db_awr_generate_jobs(report_date)`,
		`CREATE INDEX IF NOT EXISTS idx_awr_gen_job_created ON tbl_db_awr_generate_jobs(created_at DESC)`,
	} {
		if _, err := d.db.Exec(idx); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureSqlInsightTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_sql_insight_metrics (
			id            VARCHAR(64)   PRIMARY KEY,
			metric_key    VARCHAR(128)  NOT NULL UNIQUE,
			name          VARCHAR(255)  NOT NULL,
			description   TEXT          NOT NULL DEFAULT '',
			source        VARCHAR(32)   NOT NULL,
			expr_type     VARCHAR(32)   NOT NULL DEFAULT 'builtin_field',
			builtin_field VARCHAR(128)  NOT NULL DEFAULT '',
			unit          VARCHAR(32)   NOT NULL DEFAULT '',
			stat          VARCHAR(16)   NOT NULL DEFAULT 'avg',
			scope_host    VARCHAR(255)  NOT NULL DEFAULT '',
			scope_port    INTEGER       NOT NULL DEFAULT 0,
			enabled       BOOLEAN       NOT NULL DEFAULT TRUE,
			system        BOOLEAN       NOT NULL DEFAULT FALSE,
			created_by    VARCHAR(64)   NOT NULL DEFAULT '',
			updated_by    VARCHAR(64)   NOT NULL DEFAULT '',
			created_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sql_insight_rules (
			id              VARCHAR(64)   PRIMARY KEY,
			name            VARCHAR(255)  NOT NULL,
			description     TEXT          NOT NULL DEFAULT '',
			metric_key      VARCHAR(128)  NOT NULL,
			rule_kind       VARCHAR(32)   NOT NULL,
			direction       VARCHAR(16)   NOT NULL DEFAULT 'above',
			warn_threshold  DOUBLE PRECISION,
			crit_threshold  DOUBLE PRECISION,
			baseline_window VARCHAR(16)   NOT NULL DEFAULT '30d',
			zscore_k        DOUBLE PRECISION NOT NULL DEFAULT 3,
			enabled         BOOLEAN       NOT NULL DEFAULT TRUE,
			system          BOOLEAN       NOT NULL DEFAULT FALSE,
			created_by      VARCHAR(64)   NOT NULL DEFAULT '',
			updated_by      VARCHAR(64)   NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sql_insight_analysis_runs (
			id               VARCHAR(64)   PRIMARY KEY,
			analysis_date    DATE          NOT NULL,
			host             VARCHAR(255)  NOT NULL DEFAULT '',
			port             INTEGER       NOT NULL DEFAULT 0,
			status           VARCHAR(32)   NOT NULL DEFAULT 'running',
			severity         VARCHAR(16)   NOT NULL DEFAULT 'none',
			finding_count    INTEGER       NOT NULL DEFAULT 0,
			critical_count   INTEGER       NOT NULL DEFAULT 0,
			warning_count    INTEGER       NOT NULL DEFAULT 0,
			info_count       INTEGER       NOT NULL DEFAULT 0,
			error_message    TEXT          NOT NULL DEFAULT '',
			user_scenario    TEXT          NOT NULL DEFAULT '',
			scenario_tags    JSONB         NOT NULL DEFAULT '[]'::jsonb,
			narrative_status VARCHAR(32)   NOT NULL DEFAULT 'skipped',
			narrative_json   JSONB         NOT NULL DEFAULT '{}'::jsonb,
			narrative_text   TEXT          NOT NULL DEFAULT '',
			narrative_source VARCHAR(32)   NOT NULL DEFAULT '',
			trigger_type     VARCHAR(32)   NOT NULL DEFAULT 'manual',
			created_by       VARCHAR(64)   NOT NULL DEFAULT '',
			started_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
			finished_at      TIMESTAMPTZ,
			created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sql_insight_findings (
			id             VARCHAR(64)   PRIMARY KEY,
			run_id         VARCHAR(64)   NOT NULL REFERENCES tbl_sql_insight_analysis_runs(id) ON DELETE CASCADE,
			analysis_date  DATE          NOT NULL,
			host           VARCHAR(255)  NOT NULL,
			port           INTEGER       NOT NULL,
			instance_id    VARCHAR(255)  NOT NULL DEFAULT '',
			instance_name  VARCHAR(255)  NOT NULL DEFAULT '',
			metric_key     VARCHAR(128)  NOT NULL,
			rule_id        VARCHAR(64)   NOT NULL DEFAULT '',
			rule_kind      VARCHAR(32)   NOT NULL DEFAULT '',
			severity       VARCHAR(16)   NOT NULL,
			title          VARCHAR(512)  NOT NULL DEFAULT '',
			message        TEXT          NOT NULL DEFAULT '',
			value          DOUBLE PRECISION,
			threshold      DOUBLE PRECISION,
			baseline_value DOUBLE PRECISION,
			unit           VARCHAR(32)   NOT NULL DEFAULT '',
			evidence       JSONB         NOT NULL DEFAULT '{}'::jsonb,
			suggestion     TEXT          NOT NULL DEFAULT '',
			fingerprint    VARCHAR(255)  NOT NULL DEFAULT '',
			feedback       VARCHAR(16)   NOT NULL DEFAULT '',
			feedback_note  TEXT          NOT NULL DEFAULT '',
			feedback_by    VARCHAR(64)   NOT NULL DEFAULT '',
			feedback_at    TIMESTAMPTZ,
			created_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sql_insight_observations (
			id            BIGSERIAL     PRIMARY KEY,
			run_id        VARCHAR(64)   NOT NULL DEFAULT '',
			obs_date      DATE          NOT NULL,
			host          VARCHAR(255)  NOT NULL,
			port          INTEGER       NOT NULL,
			metric_key    VARCHAR(128)  NOT NULL,
			value         DOUBLE PRECISION NOT NULL,
			unit          VARCHAR(32)   NOT NULL DEFAULT '',
			created_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
			UNIQUE (obs_date, host, port, metric_key)
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sql_insight_metric_baselines (
			id            BIGSERIAL     PRIMARY KEY,
			host          VARCHAR(255)  NOT NULL,
			port          INTEGER       NOT NULL,
			metric_key    VARCHAR(128)  NOT NULL,
			window_key    VARCHAR(16)   NOT NULL DEFAULT '30d',
			sample_count  INTEGER       NOT NULL DEFAULT 0,
			mean_val      DOUBLE PRECISION,
			stddev_val    DOUBLE PRECISION,
			p50           DOUBLE PRECISION,
			p95           DOUBLE PRECISION,
			p99           DOUBLE PRECISION,
			min_val       DOUBLE PRECISION,
			max_val       DOUBLE PRECISION,
			last_value    DOUBLE PRECISION,
			updated_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
			UNIQUE (host, port, metric_key, window_key)
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sql_insight_cases (
			id                   VARCHAR(64)   PRIMARY KEY,
			fingerprint          VARCHAR(255)  NOT NULL,
			metric_key           VARCHAR(128)  NOT NULL DEFAULT '',
			severity             VARCHAR(16)   NOT NULL DEFAULT '',
			direction            VARCHAR(16)   NOT NULL DEFAULT '',
			title                VARCHAR(512)  NOT NULL DEFAULT '',
			root_cause_template  TEXT          NOT NULL DEFAULT '',
			action_template      TEXT          NOT NULL DEFAULT '',
			alert_names          TEXT          NOT NULL DEFAULT '',
			hit_count            INTEGER       NOT NULL DEFAULT 0,
			confirm_count        INTEGER       NOT NULL DEFAULT 0,
			dismiss_count        INTEGER       NOT NULL DEFAULT 0,
			system               BOOLEAN       NOT NULL DEFAULT FALSE,
			last_seen_at         TIMESTAMPTZ,
			created_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
			updated_at           TIMESTAMPTZ
		)`,
	}
	for _, s := range stmts {
		if _, err := d.db.Exec(s); err != nil {
			return err
		}
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_si_metrics_source ON tbl_sql_insight_metrics(source)`,
		`CREATE INDEX IF NOT EXISTS idx_si_rules_metric ON tbl_sql_insight_rules(metric_key)`,
		`CREATE INDEX IF NOT EXISTS idx_si_runs_date ON tbl_sql_insight_analysis_runs(analysis_date)`,
		`CREATE INDEX IF NOT EXISTS idx_si_findings_run ON tbl_sql_insight_findings(run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_si_obs_hostport_key ON tbl_sql_insight_observations(host, port, metric_key)`,
		`CREATE INDEX IF NOT EXISTS idx_si_baseline_hostport ON tbl_sql_insight_metric_baselines(host, port)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_si_cases_fp ON tbl_sql_insight_cases(fingerprint)`,
	} {
		if _, err := d.db.Exec(idx); err != nil {
			return err
		}
	}
	return d.ensureSqlInsightCaseColumns()
}

// ensureSqlInsightCaseColumns AIOps：案例表补齐 alert_names / updated_at（幂等，含旧库）。
func (d *DB) ensureSqlInsightCaseColumns() error {
	type col struct {
		withIF   string
		fallback string
	}
	for _, c := range []col{
		{
			`ALTER TABLE tbl_sql_insight_cases ADD COLUMN IF NOT EXISTS alert_names TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE tbl_sql_insight_cases ADD COLUMN alert_names TEXT NOT NULL DEFAULT ''`,
		},
		{
			`ALTER TABLE tbl_sql_insight_cases ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ`,
			`ALTER TABLE tbl_sql_insight_cases ADD COLUMN updated_at TIMESTAMPTZ`,
		},
	} {
		if _, err := d.db.Exec(c.withIF); err != nil {
			_, _ = d.db.Exec(c.fallback)
		}
	}
	return nil
}

// ensureSqlInsightCaseAlertNamesColumn 兼容旧调用名。
func (d *DB) ensureSqlInsightCaseAlertNamesColumn() error {
	return d.ensureSqlInsightCaseColumns()
}

func (d *DB) ensurePGSpaceTables() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_pg_space_snapshots (
		id              BIGSERIAL     PRIMARY KEY,
		collected_at    TIMESTAMPTZ   NOT NULL,
		instance_id     VARCHAR(255),
		instance_name   VARCHAR(255),
		host            VARCHAR(255)  NOT NULL,
		port            INTEGER       NOT NULL,
		object_type     VARCHAR(16)   NOT NULL,
		db_name         VARCHAR(255),
		schema_name     VARCHAR(255),
		object_name     VARCHAR(512)  NOT NULL,
		tablespace_name VARCHAR(255),
		shard_count     INTEGER       NOT NULL DEFAULT 1,
		size_bytes      BIGINT,
		table_bytes     BIGINT,
		index_bytes     BIGINT,
		toast_bytes     BIGINT,
		n_live_tup      BIGINT,
		n_dead_tup      BIGINT,
		dead_ratio      DOUBLE PRECISION,
		bloat_bytes     BIGINT,
		bloat_ratio     DOUBLE PRECISION,
		idx_scan        BIGINT,
		is_unused       BOOLEAN,
		seq_scan        BIGINT,
		tbl_idx_scan    BIGINT,
		heap_blks_read  BIGINT,
		heap_blks_hit   BIGINT,
		heap_cache_hit_ratio DOUBLE PRECISION,
		n_tup_ins       BIGINT,
		n_tup_upd       BIGINT,
		n_tup_del       BIGINT,
		last_vacuum     TIMESTAMPTZ,
		last_autovacuum TIMESTAMPTZ,
		last_analyze    TIMESTAMPTZ,
		last_autoanalyze TIMESTAMPTZ,
		relfrozenxid_age BIGINT,
		datfrozenxid_age BIGINT,
		seq_last_value  BIGINT,
		seq_max_value   BIGINT,
		seq_min_value   BIGINT,
		seq_used_ratio  DOUBLE PRECISION,
		seq_remaining   BIGINT,
		seq_cycle       BOOLEAN,
		created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		return err
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_pg_space_snap_collected ON tbl_pg_space_snapshots(collected_at)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_space_snap_instance  ON tbl_pg_space_snapshots(instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_space_snap_hostport  ON tbl_pg_space_snapshots(host, port)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_space_snap_type_db   ON tbl_pg_space_snapshots(object_type, db_name)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_space_snap_trend ON tbl_pg_space_snapshots(instance_id, object_type, db_name, object_name, collected_at)`,
	} {
		if _, err := d.db.Exec(idx); err != nil {
			return err
		}
	}

	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_pg_space_collect_jobs (
		id               VARCHAR(255) PRIMARY KEY,
		scope            VARCHAR(16)  NOT NULL,
		instance_id      VARCHAR(255),
		host             VARCHAR(255),
		port             INTEGER,
		state            VARCHAR(16)  NOT NULL,
		instances_total  INTEGER      NOT NULL DEFAULT 0,
		instances_done   INTEGER      NOT NULL DEFAULT 0,
		current_instance VARCHAR(255),
		current_stage    VARCHAR(32),
		db_total         INTEGER      NOT NULL DEFAULT 0,
		db_done          INTEGER      NOT NULL DEFAULT 0,
		current_db       VARCHAR(255),
		rows_written     INTEGER      NOT NULL DEFAULT 0,
		error            TEXT,
		created_by       VARCHAR(255),
		started_at       TIMESTAMPTZ,
		updated_at       TIMESTAMPTZ,
		finished_at      TIMESTAMPTZ,
		created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		return err
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_pg_space_job_state    ON tbl_pg_space_collect_jobs(state)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_space_job_instance ON tbl_pg_space_collect_jobs(instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_space_job_created  ON tbl_pg_space_collect_jobs(created_at DESC)`,
	} {
		if _, err := d.db.Exec(idx); err != nil {
			return err
		}
	}

	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_pg_space_advice_latest (
		id                   BIGSERIAL     PRIMARY KEY,
		instance_id          VARCHAR(255)  NOT NULL DEFAULT '',
		instance_name        VARCHAR(255),
		host                 VARCHAR(255)  NOT NULL,
		port                 INTEGER       NOT NULL,
		collected_at         TIMESTAMPTZ   NOT NULL,
		severity             VARCHAR(16)   NOT NULL DEFAULT 'none',
		advice_count         INTEGER       NOT NULL DEFAULT 0,
		reclaimable_bytes    BIGINT        NOT NULL DEFAULT 0,
		xid_risk_count       INTEGER       NOT NULL DEFAULT 0,
		sequence_risk_count  INTEGER       NOT NULL DEFAULT 0,
		vacuum_risk_count    INTEGER       NOT NULL DEFAULT 0,
		access_risk_count    INTEGER       NOT NULL DEFAULT 0,
		cache_risk_count     INTEGER       NOT NULL DEFAULT 0,
		hot_table_count      INTEGER       NOT NULL DEFAULT 0,
		report               JSONB         NOT NULL,
		updated_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
		CONSTRAINT uq_pg_space_advice_host_port UNIQUE (host, port)
	)`)
	if err != nil {
		return err
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_pg_space_advice_instance  ON tbl_pg_space_advice_latest(instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_space_advice_severity  ON tbl_pg_space_advice_latest(severity)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_space_advice_collected ON tbl_pg_space_advice_latest(collected_at DESC)`,
	} {
		if _, err := d.db.Exec(idx); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureLoginAttemptTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_login_attempt (
		username TEXT PRIMARY KEY,
		fail_count INTEGER NOT NULL DEFAULT 0,
		cooldown_until TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	return err
}

func (d *DB) ensureRolesUpdatedAt() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_roles' AND column_name = 'updated_at'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_roles ADD COLUMN updated_at TEXT`); err != nil {
			return err
		}
		if _, err := d.db.Exec(`UPDATE tbl_roles SET updated_at = created_at WHERE updated_at IS NULL OR TRIM(updated_at) = ''`); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureQueryAuditsTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_query_audits (
		id TEXT PRIMARY KEY,
		kind TEXT NOT NULL,
		user_id TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		display_name TEXT NOT NULL DEFAULT '',
		ip TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT '',
		instance_id TEXT NOT NULL DEFAULT '',
		instance_name TEXT NOT NULL DEFAULT '',
		db_type TEXT NOT NULL DEFAULT '',
		target TEXT NOT NULL DEFAULT '',
		statement TEXT NOT NULL DEFAULT '',
		success INTEGER NOT NULL DEFAULT 0,
		message TEXT NOT NULL DEFAULT '',
		row_count INTEGER NOT NULL DEFAULT 0,
		elapsed_ms BIGINT NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	if _, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_query_audits_created_at ON tbl_query_audits(created_at DESC)`); err != nil {
		return err
	}
	if _, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_query_audits_username ON tbl_query_audits(username)`); err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_query_audits_kind ON tbl_query_audits(kind)`)
	return err
}

func (d *DB) ensureArchiveSchema() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_archive_runs (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		status TEXT NOT NULL,
		strategy TEXT NOT NULL DEFAULT '',
		deleted_rows BIGINT NOT NULL DEFAULT 0,
		rounds INTEGER NOT NULL DEFAULT 0,
		message TEXT NOT NULL DEFAULT '',
		sql_preview TEXT NOT NULL DEFAULT '',
		started_at TEXT NOT NULL DEFAULT '',
		finished_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_archive_runs_task ON tbl_archive_runs(task_id, created_at DESC)`)
	return err
}

func (d *DB) ensurePGActivitySnapshotsTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_pg_activity_snapshots (
		id TEXT PRIMARY KEY,
		collected_at TEXT NOT NULL,
		instance_id TEXT NOT NULL DEFAULT '',
		instance_name TEXT NOT NULL DEFAULT '',
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 0,
		pid BIGINT,
		p_user TEXT NOT NULL DEFAULT '',
		p_host TEXT NOT NULL DEFAULT '',
		p_db TEXT NOT NULL DEFAULT '',
		command TEXT NOT NULL DEFAULT '',
		xact_duration DOUBLE PRECISION,
		state TEXT NOT NULL DEFAULT '',
		info TEXT NOT NULL DEFAULT '',
		query_normalized TEXT NOT NULL DEFAULT '',
		query_hash TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_pg_act_snap_inst_time ON tbl_pg_activity_snapshots(instance_id, collected_at DESC)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_pg_act_snap_collected ON tbl_pg_activity_snapshots(collected_at)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_pg_act_snap_qhash ON tbl_pg_activity_snapshots(query_hash)`)
	if err != nil {
		return err
	}
	return nil
}

func (d *DB) ensureMigrationSchema() error {
	for _, spec := range []struct{ col, def string }{
		{"created_by", "TEXT NOT NULL DEFAULT ''"},
		{"db_type", "TEXT NOT NULL DEFAULT ''"},
		{"env", "TEXT NOT NULL DEFAULT ''"},
		{"source_host", "TEXT NOT NULL DEFAULT ''"},
		{"source_port", "INTEGER NOT NULL DEFAULT 0"},
		{"target_host", "TEXT NOT NULL DEFAULT ''"},
		{"target_port", "INTEGER NOT NULL DEFAULT 0"},
		{"migration_mode", "TEXT NOT NULL DEFAULT 'full_data'"},
		{"schema_strategy", "TEXT NOT NULL DEFAULT 'init'"},
		{"conflict_strategy", "TEXT NOT NULL DEFAULT 'abort'"},
		{"transfer_mode", "TEXT NOT NULL DEFAULT 'direct'"},
		{"start_mode", "TEXT NOT NULL DEFAULT 'immediate'"},
		{"scheduled_at", "TEXT NOT NULL DEFAULT ''"},
		{"approval_status", "TEXT NOT NULL DEFAULT 'approved'"},
		{"issue_id", "TEXT NOT NULL DEFAULT ''"},
		{"assigned_approver", "TEXT NOT NULL DEFAULT ''"},
		{"approved_by", "TEXT NOT NULL DEFAULT ''"},
		{"approval_comment", "TEXT NOT NULL DEFAULT ''"},
		{"batch_size", "INTEGER NOT NULL DEFAULT 1000"},
		{"speed_limit", "INTEGER NOT NULL DEFAULT 0"},
		{"total_objects", "INTEGER NOT NULL DEFAULT 0"},
		{"completed_objects", "INTEGER NOT NULL DEFAULT 0"},
		{"total_rows", "BIGINT NOT NULL DEFAULT 0"},
		{"completed_rows", "BIGINT NOT NULL DEFAULT 0"},
		{"error_message", "TEXT NOT NULL DEFAULT ''"},
		{"started_at", "TEXT NOT NULL DEFAULT ''"},
		{"finished_at", "TEXT NOT NULL DEFAULT ''"},
		{"tags", "TEXT NOT NULL DEFAULT '{}'"},
	} {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.columns
			WHERE table_name = 'tbl_migration_tasks' AND column_name = $1
		`, spec.col).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := d.db.Exec(`ALTER TABLE tbl_migration_tasks ADD COLUMN ` + spec.col + ` ` + spec.def); err != nil {
				return err
			}
		}
	}
	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_migration_objects (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		source_db_name TEXT NOT NULL,
		source_schema TEXT NOT NULL DEFAULT '',
		source_table_name TEXT NOT NULL,
		target_db_name TEXT NOT NULL,
		target_schema TEXT NOT NULL DEFAULT '',
		target_table_name TEXT NOT NULL,
		object_type TEXT NOT NULL DEFAULT 'table',
		status TEXT NOT NULL DEFAULT 'pending',
		total_rows BIGINT NOT NULL DEFAULT 0,
		completed_rows BIGINT NOT NULL DEFAULT 0,
		error_message TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_mig_obj_task ON tbl_migration_objects(task_id)`); err != nil {
		return err
	}
	for _, spec := range []struct{ col, def string }{
		{"total_bytes", "BIGINT NOT NULL DEFAULT 0"},
		{"completed_bytes", "BIGINT NOT NULL DEFAULT 0"},
	} {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.columns
			WHERE table_name = 'tbl_migration_objects' AND column_name = $1
		`, spec.col).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := d.db.Exec(`ALTER TABLE tbl_migration_objects ADD COLUMN ` + spec.col + ` ` + spec.def); err != nil {
				return err
			}
		}
	}
	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_migration_logs (
		id BIGSERIAL PRIMARY KEY,
		task_id TEXT NOT NULL,
		log_level TEXT NOT NULL DEFAULT 'INFO',
		message TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_mig_log_task ON tbl_migration_logs(task_id, id)`); err != nil {
		return err
	}
	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_migration_prechecks (
		id BIGSERIAL PRIMARY KEY,
		task_id TEXT NOT NULL,
		check_item TEXT NOT NULL,
		check_level TEXT NOT NULL DEFAULT 'required',
		check_status TEXT NOT NULL DEFAULT 'pending',
		message TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	_, err := d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_mig_pre_task ON tbl_migration_prechecks(task_id)`)
	return err
}

func (d *DB) ensureUserProfileColumn() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_users' AND column_name = 'profile'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_users ADD COLUMN profile TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureUserSourceColumn() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_users' AND column_name = 'source'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_users ADD COLUMN source TEXT NOT NULL DEFAULT 'platform'`); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureUserLoginStatusColumns() error {
	for _, col := range []struct{ name, def string }{
		{"login_disabled", "INTEGER NOT NULL DEFAULT 0"},
		{"failed_login_count", "INTEGER NOT NULL DEFAULT 0"},
		{"locked_at", "TEXT NOT NULL DEFAULT ''"},
	} {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.columns
			WHERE table_name = 'tbl_users' AND column_name = $1
		`, col.name).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := d.db.Exec(`ALTER TABLE tbl_users ADD COLUMN ` + col.name + ` ` + col.def); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *DB) ensureFlashbackSchema() error {
	for _, spec := range []struct{ table, col, def string }{
		{"tbl_instances", "cloud_vendor", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_instances", "cloud_instance_id", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_instances", "cloud_region", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "host", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "port", "INTEGER NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "tables", "TEXT NOT NULL DEFAULT '[]'"},
		{"tbl_flashback_tasks", "sql_type", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "output_kind", "TEXT NOT NULL DEFAULT 'flashback'"},
		{"tbl_flashback_tasks", "reviewer", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "error_message", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "warning", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "work_dir", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "wal_bytes", "BIGINT NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "wal_files", "INTEGER NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "change_count", "INTEGER NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "log_total", "INTEGER NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "log_done", "INTEGER NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "parse_total", "INTEGER NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "parse_done", "INTEGER NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "dml_ticket_id", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "issue_id", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "approval_status", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_archive_tasks", "issue_id", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_archive_tasks", "approval_status", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_archive_tasks", "assigned_to", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_archive_tasks", "schedule_expr", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_archive_tasks", "issue_id", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_archive_tasks", "approval_status", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_archive_tasks", "assigned_to", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_archive_tasks", "schedule_expr", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "created_by", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "start_xid", "BIGINT NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "stop_xid", "BIGINT NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "start_file", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "start_pos", "BIGINT NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "stop_file", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "stop_pos", "BIGINT NOT NULL DEFAULT 0"},
		{"tbl_flashback_tasks", "started_at", "TEXT NOT NULL DEFAULT ''"},
		{"tbl_flashback_tasks", "finished_at", "TEXT NOT NULL DEFAULT ''"},
	} {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.columns
			WHERE table_name = $1 AND column_name = $2
		`, spec.table, spec.col).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := d.db.Exec(`ALTER TABLE ` + spec.table + ` ADD COLUMN ` + spec.col + ` ` + spec.def); err != nil {
				return err
			}
		}
	}
	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_flashback_logs (
		id BIGSERIAL PRIMARY KEY,
		task_id TEXT NOT NULL,
		level TEXT NOT NULL DEFAULT 'INFO',
		message TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_fb_log_task ON tbl_flashback_logs(task_id, id)`); err != nil {
		return err
	}
	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_flashback_sqls (
		id BIGSERIAL PRIMARY KEY,
		task_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		kind TEXT NOT NULL DEFAULT '',
		schema_name TEXT NOT NULL DEFAULT '',
		table_name TEXT NOT NULL DEFAULT '',
		op TEXT NOT NULL DEFAULT '',
		xid BIGINT NOT NULL DEFAULT 0,
		ts TEXT NOT NULL DEFAULT '',
		statement TEXT NOT NULL,
		risk TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return err
	}
	_, err := d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_fb_sql_task ON tbl_flashback_sqls(task_id, seq)`)
	if err != nil {
		return err
	}
	if err := d.ensureCDCConnectorVersionsTable(); err != nil {
		return err
	}
	if err := d.ensureCDCSyncLogsTable(); err != nil {
		return err
	}
	if err := d.ensureSchemaCompareFixSQLColumn(); err != nil {
		return err
	}
	if err := d.ensureAccessDomainsTable(); err != nil {
		return err
	}
	if _, err := d.BackfillAccessDomainsFromInstances(); err != nil {
		return err
	}
	return nil
}

func (d *DB) ensureCDCSyncLogsTable() error {
	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_cdc_sync_logs (
		id BIGSERIAL PRIMARY KEY,
		task_id TEXT NOT NULL,
		mapping_id TEXT NOT NULL DEFAULT '',
		log_level TEXT NOT NULL DEFAULT 'INFO',
		step TEXT NOT NULL DEFAULT '',
		message TEXT NOT NULL DEFAULT '',
		detail TEXT NOT NULL DEFAULT '',
		operator TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cdc_sync_logs_task ON tbl_cdc_sync_logs(task_id, id DESC)`); err != nil {
		return err
	}
	_, err := d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cdc_sync_logs_created ON tbl_cdc_sync_logs(created_at)`)
	return err
}

func (d *DB) ensureSchemaCompareFixSQLColumn() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_schema_compare_tasks' AND column_name = 'fix_sql'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_schema_compare_tasks ADD COLUMN fix_sql TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureCDCConnectorVersionsTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_cdc_connector_config_versions (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		version_no INTEGER NOT NULL,
		config_json TEXT NOT NULL,
		change_source TEXT NOT NULL DEFAULT '',
		operator TEXT NOT NULL DEFAULT '',
		operator_ip TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		UNIQUE (task_id, version_no)
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cdc_conn_ver_task ON tbl_cdc_connector_config_versions(task_id, version_no DESC)`)
	return err
}
