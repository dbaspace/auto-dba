package backup

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"auto-dba/internal/sshx"
)

// PreflightOpts SSH 预检参数。
type PreflightOpts struct {
	Host               string
	Port               int
	User               string
	IdentityFile       string
	RemoteDir          string
	Workdir            string
	EnabledGroups      []string
	BackupEngines      []string // 备份控制所选实例类型
	Escalate           string   // "" | sudo_su
	InsecureHostKey    bool
}

// RunPreflight 在目标机按勾选能力组做只读检查。
func RunPreflight(ctx context.Context, opts PreflightOpts) (*CapabilitiesReport, error) {
	groups := NormalizeGroups(opts.EnabledGroups)
	if len(groups) == 0 {
		groups = DefaultEnabledGroups()
	}
	engines := NormalizeBackupEngines(opts.BackupEngines)
	hasBackup := false
	for _, g := range groups {
		if g == GroupBackup {
			hasBackup = true
			break
		}
	}
	if hasBackup && len(engines) == 0 {
		engines = DefaultBackupEngines()
	}
	if strings.TrimSpace(opts.Host) == "" {
		return nil, fmt.Errorf("主机地址为空")
	}
	port := opts.Port
	if port <= 0 {
		port = 22
	}
	user := strings.TrimSpace(opts.User)
	if user == "" {
		user = "sysadmin"
	}
	workdir := strings.TrimSpace(opts.Workdir)
	if workdir == "" {
		rd := strings.TrimSpace(opts.RemoteDir)
		if rd == "" {
			rd = "/tmp/auto-dba-platform-agent"
		}
		workdir = path.Join(rd, "data")
	}
	escalate := sshx.NormalizeEscalate(opts.Escalate)

	client, err := sshx.Dial(opts.Host, port, sshx.Config{
		User:            user,
		IdentityFile:    opts.IdentityFile,
		Timeout:         30 * time.Second,
		InsecureHostKey: opts.InsecureHostKey,
	})
	if err != nil {
		return nil, err
	}
	defer client.Close()

	if escalate == sshx.EscalateSudoSu {
		if _, errOut, err := client.RunEscalated(ctx, escalate, "true"); err != nil {
			return nil, fmt.Errorf("提权探针失败（sudo su -）: %v; %s%s", err, strings.TrimSpace(errOut), sshx.EscalateHint(escalate))
		}
	}

	script := buildPreflightScript(workdir, groups, engines)
	out, errOut, err := client.RunEscalated(ctx, escalate, script)
	if err != nil {
		if strings.TrimSpace(out) == "" {
			return nil, fmt.Errorf("预检失败: %v; %s", err, strings.TrimSpace(errOut))
		}
	}
	probe := parsePreflightOutput(out)
	return BuildReportFromProbeEngines(groups, engines, probe), nil
}

func buildPreflightScript(workdir string, groups, backupEngines []string) string {
	var b strings.Builder
	b.WriteString("set +e; ")
	// 输出：key=1|/abs/path 或 key=0|
	b.WriteString(`check(){ p=$(command -v "$1" 2>/dev/null); if [ -n "$p" ]; then echo "$2=1|$p"; else echo "$2=0|"; fi; }; `)
	enabled := map[string]bool{}
	for _, g := range groups {
		enabled[g] = true
	}
	if enabled[GroupBackup] {
		engines := NormalizeBackupEngines(backupEngines)
		if len(engines) == 0 {
			engines = DefaultBackupEngines()
		}
		needMysqldump := false
		needXtra := false
		for _, e := range engines {
			switch e {
			case BackupEnginePostgres:
				b.WriteString("check pg_dump logical_pg; ")
				b.WriteString("check pg_basebackup physical_pg; ")
			case BackupEngineMySQL:
				needMysqldump = true
				needXtra = true
			case BackupEngineTiDB:
				needMysqldump = true
				needXtra = true
			case BackupEngineOceanBase:
				needMysqldump = true
			}
		}
		if needMysqldump {
			b.WriteString(`p=$(command -v mysqldump 2>/dev/null); if [ -n "$p" ]; then echo "logical_mysql=1|$p"; echo "logical_tidb=1|$p"; echo "logical_oceanbase=1|$p"; else echo "logical_mysql=0|"; echo "logical_tidb=0|"; echo "logical_oceanbase=0|"; fi; `)
		}
		if needXtra {
			b.WriteString(`p=$(command -v xtrabackup 2>/dev/null); if [ -z "$p" ]; then p=$(command -v mariabackup 2>/dev/null); fi; if [ -n "$p" ]; then echo "physical_mysql=1|$p"; else echo "physical_mysql=0|"; fi; `)
		}
	}
	if enabled[GroupRDB] {
		b.WriteString("check redis-cli redis_rdb; ")
	}
	if enabled[GroupHA] {
		b.WriteString("check masterha_check_status ha_mha; ")
	}
	b.WriteString(fmt.Sprintf(
		"mkdir -p %s 2>/dev/null; AVAIL=$(df -Pk %s 2>/dev/null | awk 'NR==2{print $4}'); "+
			`if [ -n "$AVAIL" ] && [ "$AVAIL" -ge 5242880 ]; then echo "runtime_disk=1|${AVAIL}KB"; else echo "runtime_disk=0|${AVAIL}"; fi; `,
		shellQuote(workdir), shellQuote(workdir),
	))
	b.WriteString("true")
	return b.String()
}

func parsePreflightOutput(out string) map[string]ProbeResult {
	probe := map[string]ProbeResult{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		okPart, pathPart, _ := strings.Cut(val, "|")
		ok := strings.TrimSpace(okPart) == "1"
		probe[key] = ProbeResult{OK: ok, Path: strings.TrimSpace(pathPart)}
	}
	return probe
}
