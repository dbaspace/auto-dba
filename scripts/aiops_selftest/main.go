package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"auto-dba/internal/aiops"
	"auto-dba/internal/config"
	"auto-dba/internal/sqlinsight"
	"auto-dba/internal/store"
)

// 不经过 HTTP 登录：直接验证知识库 seed + Context Bundle 案例对齐。
func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	si := sqlinsight.New(db, sqlinsight.StoreAWRLoader{DB: db})
	if err := si.SeedSystemCases(context.Background()); err != nil {
		log.Fatalf("SeedSystemCases: %v", err)
	}
	fmt.Println("PASS SeedSystemCases")

	cases, total, err := db.ListSqlInsightCases(context.Background(), 0, 50)
	if err != nil {
		log.Fatalf("ListSqlInsightCases: %v", err)
	}
	if total < 10 {
		log.Fatalf("cases total=%d want >=10", total)
	}
	var hasPGConn bool
	for _, c := range cases {
		if strings.Contains(c.AlertNames, "PGConnHigh") {
			hasPGConn = true
			break
		}
	}
	if !hasPGConn {
		log.Fatal("no case with alert_names PGConnHigh")
	}
	fmt.Println("PASS db cases", total, "with PGConnHigh alert_names")

	svc := aiops.New(db)
	fake := &store.Alert{
		ID: "selftest-alert", TenantID: store.DefaultTenantID,
		Fingerprint: "selftest-fp", RuleID: "rule-seed-pg-cache-hit",
		Severity: "P1", Title: "PostgreSQL 缓冲命中率过低", Service: "postgres",
		Labels: map[string]string{"alertname": "PGCacheHitLow", "service": "postgres"},
		Status: store.AlertStatusFiring, Handle: store.AlertHandleUnclaimed,
	}
	hints := aiops.MatchCasesForAlert(mustKB(db), fake, &store.AlertRule{
		ID: "rule-seed-pg-cache-hit",
		Labels: map[string]string{"alertname": "PGCacheHitLow"},
	})
	if len(hints) == 0 {
		log.Fatal("MatchCasesForAlert empty for PGCacheHitLow")
	}
	fmt.Println("PASS MatchCasesForAlert", hints[0].Title, hints[0].MatchReason)

	// 仅标题「缓冲」无 alertname 时也应命中（启发式）
	hints2 := aiops.MatchCasesForAlert(mustKB(db), &store.Alert{
		Title: "PostgreSQL 缓冲命中率过低", Service: "postgres",
	}, nil)
	if len(hints2) == 0 {
		log.Fatal("title heuristic 缓冲 miss")
	}
	fmt.Println("PASS title heuristic", hints2[0].MetricKey)

	// 若库内有真实告警，再测 Bundle
	alerts, err := db.ListAlerts(store.AlertFilter{Tab: "all", TenantID: store.DefaultTenantID})
	if err != nil {
		log.Fatal(err)
	}
	if len(alerts) > 0 {
		var pick *store.Alert
		for i := range alerts {
			a := &alerts[i]
			if strings.Contains(a.Title, "缓冲") || strings.Contains(a.Title, "缓存") ||
				a.Labels["alertname"] == "PGCacheHitLow" || a.Labels["alertname"] == "PGConnHigh" {
				pick = a
				break
			}
		}
		if pick == nil {
			pick = &alerts[0]
		}
		b, err := svc.BuildAlertContextBundle(context.Background(), pick.ID, store.DefaultTenantID)
		if err != nil {
			log.Fatalf("bundle: %v", err)
		}
		if b == nil {
			log.Fatal("bundle nil")
		}
		if len(b.Cases) == 0 && (pick.Labels["alertname"] != "" || strings.Contains(pick.Title, "缓冲") || strings.Contains(pick.Title, "连接")) {
			log.Fatalf("bundle cases empty for alert %s title=%s labels=%v", pick.ID, pick.Title, pick.Labels)
		}
		fmt.Printf("PASS context-bundle id=%s cases=%d sources=%v summary=%q\n",
			pick.ID, len(b.Cases), b.Sources, trim(b.Summary, 100))
	} else {
		fmt.Println("SKIP context-bundle (no alerts in db)")
	}

	// 种子告警规则
	rules, err := db.ListAlertRules()
	if err != nil {
		log.Fatal(err)
	}
	need := map[string]bool{"PGCacheHitLow": false, "PGReplicationLag": false, "PGDeadlocks": false}
	for _, r := range rules {
		an := r.Labels["alertname"]
		if _, ok := need[an]; ok {
			need[an] = true
			if strings.TrimSpace(r.TenantID) == "" {
				// list may not fill tenant; check via Get
			}
		}
	}
	for an, ok := range need {
		if !ok {
			log.Fatalf("missing seed rule alertname=%s", an)
		}
	}
	fmt.Println("PASS seed PG alert rules")
	fmt.Println("ALL_DB_SELFTEST_OK")
	os.Exit(0)
}

func mustKB(db *store.DB) []*store.SqlInsightCaseRow {
	rows, _, err := db.ListSqlInsightCases(context.Background(), 0, 200)
	if err != nil {
		log.Fatal(err)
	}
	return rows
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
