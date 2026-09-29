package backup

import (
	"strings"
	"testing"
)

func TestNormalizeGroupsAndMissing(t *testing.T) {
	g := NormalizeGroups([]string{"Backup", "rdb", "ha", "x"})
	if len(g) != 3 {
		t.Fatalf("groups=%v", g)
	}
	probe := map[string]ProbeResult{
		"logical_pg":    {OK: true, Path: "/usr/bin/pg_dump"},
		"logical_mysql": {OK: false},
		"redis_rdb":     {OK: true, Path: "/usr/bin/redis-cli"},
		"ha_mha":        {OK: false},
		"runtime_disk":  {OK: true, Path: "10240000KB"},
	}
	rep := BuildReportFromProbeEngines([]string{GroupBackup, GroupRDB}, []string{BackupEnginePostgres, BackupEngineMySQL}, probe)
	miss := MissingRequired(rep)
	if len(miss) != 1 || miss[0].Key != "logical_mysql" {
		t.Fatalf("missing=%v", miss)
	}
	if miss[0].MissingCmd != "mysqldump" {
		t.Fatalf("missing_cmd=%q", miss[0].MissingCmd)
	}
	if !strings.Contains(miss[0].Detail, "mysqldump") {
		t.Fatalf("detail=%q", miss[0].Detail)
	}
	line := FormatMissingLine(miss[0])
	if !strings.Contains(line, "缺少命令：mysqldump") {
		t.Fatalf("line=%q", line)
	}
	sum := FormatPreflightSummary(rep)
	if !strings.Contains(sum, "已检测到") || !strings.Contains(sum, "pg_dump") {
		t.Fatalf("summary=%q", sum)
	}
	if !HasCapability(rep, "redis_rdb") {
		t.Fatal("redis_rdb should be ok")
	}
}

func TestBackupEnginesOnlyPostgres(t *testing.T) {
	probe := map[string]ProbeResult{
		"logical_pg":   {OK: true, Path: "/usr/bin/pg_dump"},
		"runtime_disk": {OK: true},
	}
	rep := BuildReportFromProbeEngines([]string{GroupBackup}, []string{BackupEnginePostgres}, probe)
	if miss := MissingRequired(rep); len(miss) != 0 {
		t.Fatalf("should pass with only pg: %v", miss)
	}
	for _, it := range rep.Items {
		if it.Key == "logical_mysql" && it.Status != CapSkipped {
			t.Fatalf("mysql should skip, got %s", it.Status)
		}
	}
}

func TestBackupEnginesTiDB(t *testing.T) {
	probe := map[string]ProbeResult{
		"logical_tidb":  {OK: false},
		"logical_mysql": {OK: true, Path: "/usr/bin/mysqldump"},
		"runtime_disk":  {OK: true},
	}
	rep := BuildReportFromProbeEngines([]string{GroupBackup}, []string{BackupEngineTiDB}, probe)
	if miss := MissingRequired(rep); len(miss) != 0 {
		t.Fatalf("tidb should fallback to mysqldump: %v", miss)
	}
}

func TestParsePreflightOutput(t *testing.T) {
	raw := "logical_pg=1|/usr/bin/pg_dump\nlogical_mysql=0|\nredis_rdb=1|/usr/local/bin/redis-cli\n"
	p := parsePreflightOutput(raw)
	if !p["logical_pg"].OK || p["logical_pg"].Path != "/usr/bin/pg_dump" {
		t.Fatalf("pg=%v", p["logical_pg"])
	}
	if p["logical_mysql"].OK {
		t.Fatal("mysql should miss")
	}
}
