package aiops

import "strings"

// InferCaseEngine 从指纹/标题/alertname 推断案例所属引擎（库内行无 engine 列时用）。
func InferCaseEngine(fingerprint, title, alertNames, metricKey string) string {
	fp := strings.TrimSpace(fingerprint)
	for _, kc := range AllKnowledgeCases() {
		if kc.Fingerprint == fp {
			return strings.ToLower(strings.TrimSpace(kc.Engine))
		}
	}
	// 指纹后缀约定：conn_usage_ratio|warning|above|mysql
	lowFP := strings.ToLower(fp)
	for _, eng := range []string{"mysql", "redis", "starrocks", "postgres", "host"} {
		if strings.HasSuffix(lowFP, "|"+eng) || strings.Contains(lowFP, "|"+eng+"|") {
			return eng
		}
	}

	for _, n := range ParseAlertNames(alertNames) {
		n = strings.ToLower(strings.TrimSpace(n))
		switch {
		case strings.HasPrefix(n, "mysql"):
			return "mysql"
		case strings.HasPrefix(n, "redis"):
			return "redis"
		case strings.HasPrefix(n, "sr"):
			return "starrocks"
		case strings.HasPrefix(n, "pg"):
			return "postgres"
		case strings.HasPrefix(n, "host"):
			return "host"
		}
	}

	t := strings.ToLower(strings.TrimSpace(title))
	t = strings.TrimPrefix(t, "学习补充：")
	t = strings.TrimPrefix(t, "学习补充:")
	t = strings.TrimSpace(t)
	switch {
	case strings.Contains(t, "mha") || strings.Contains(t, "proxysql") || strings.Contains(t, "orchestrator"):
		return "mysql"
	case strings.HasPrefix(t, "mysql") || strings.Contains(t, "mysql"):
		return "mysql"
	case strings.HasPrefix(t, "redis") || strings.Contains(t, "redis"):
		return "redis"
	case strings.HasPrefix(t, "starrocks") || strings.Contains(t, "starrocks") || strings.HasPrefix(t, "sr "):
		return "starrocks"
	case strings.HasPrefix(t, "主机") || strings.HasPrefix(t, "host"):
		return "host"
	case strings.HasPrefix(t, "pg ") || strings.HasPrefix(t, "pg") || strings.Contains(t, "postgres") || strings.Contains(t, "postgresql"):
		return "postgres"
	}

	mk := strings.ToLower(strings.TrimSpace(metricKey))
	switch {
	case strings.Contains(mk, "binlog") || strings.Contains(mk, "tmp_disk"):
		return "mysql"
	case strings.Contains(mk, "bigkey") || strings.Contains(mk, "hotkey") || strings.Contains(mk, "rdb_"):
		return "redis"
	case strings.Contains(mk, "tablet") || strings.Contains(mk, "be_") || strings.Contains(mk, "compaction") || strings.Contains(mk, "load_"):
		return "starrocks"
	case strings.Contains(mk, "wal_") || strings.Contains(mk, "xid") || strings.Contains(mk, "autovacuum"):
		return "postgres"
	}
	return ""
}

// CaseMatchesEngine engine 为空 / all 时全部匹配。
func CaseMatchesEngine(engine, fingerprint, title, alertNames, metricKey string) bool {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if engine == "" || engine == "all" {
		return true
	}
	got := InferCaseEngine(fingerprint, title, alertNames, metricKey)
	if got == "" {
		// 无法判断时：仅在「全部」展示，避免 mysql 下列出 pg
		return false
	}
	return got == engine
}
