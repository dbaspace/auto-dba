package archive

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const pythonRuntime = `
def _hub_run():
    total = 0
    try:
        n = archive_delete()
        total = 0 if n is None else int(n)
    except Exception as e:
        print(str(e), file=_hub_sys.stderr, flush=True)
        print("affected_row={0}".format(int(total)), flush=True)
        _hub_sys.exit(1)
    print("affected_row={0}".format(int(total)), flush=True)
    _hub_sys.exit(0)

if __name__ == "__main__":
    _hub_run()
`

const goRuntime = `
func main() {
	n, err := archiveDelete()
	if n < 0 {
		n = 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		fmt.Printf("affected_row=%d\n", n)
		os.Exit(1)
	}
	fmt.Printf("affected_row=%d\n", n)
}
`

var (
	pyDeleteFnRe = regexp.MustCompile(`\bdef\s+archive_delete\s*\(`)
	goDeleteFnRe = regexp.MustCompile(`\bfunc\s+archiveDelete\s*\(`)
	goMainFnRe   = regexp.MustCompile(`\bfunc\s+main\s*\(`)
	affectedRe   = regexp.MustCompile(`affected_row=(-?\d+)`)
)

// ScriptParams 是按填写项生成 Go 删除脚本的参数。
type ScriptParams struct {
	Host       string
	Port       int
	User       string
	Password   string
	DBName     string
	Tables     []string
	RetainDays int
	BatchLimit int
}

func normalizeScriptKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "sql":
		return "sql"
	case "python", "py":
		return "python"
	case "go", "golang":
		return "go"
	case "shell", "sh", "bash":
		return "shell"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

// PrepareScript 检查脚本能否提交。Python、Go 会在本机编译；SQL 只要求内容不为空。
func PrepareScript(kind, body string) error {
	kind = normalizeScriptKind(kind)
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("脚本内容不能为空")
	}
	if len(body) > scriptMaxBytes {
		return fmt.Errorf("脚本超过 128KB")
	}
	switch kind {
	case "sql":
		return nil
	case "shell":
		return fmt.Errorf("当前不支持 Shell 脚本")
	case "python":
		wrapped, err := wrapPython(body)
		if err != nil {
			return err
		}
		return compilePython(wrapped)
	case "go":
		src, err := wrapGo(body)
		if err != nil {
			return err
		}
		dir, bin, err := buildGo(context.Background(), src)
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
		_ = bin
		return err
	default:
		return fmt.Errorf("当前不支持 %s 脚本", kind)
	}
}

func wrapPython(body string) (string, error) {
	if !pyDeleteFnRe.MatchString(body) {
		return "", fmt.Errorf("Python 脚本须定义 archive_delete()，并返回删除行数")
	}
	if strings.Contains(body, "def _hub_run") {
		return body, nil
	}
	var b strings.Builder
	if !strings.Contains(body, "import sys") {
		b.WriteString("import sys as _hub_sys\n")
	} else {
		b.WriteString("import sys as _hub_sys\n")
	}
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n")
	b.WriteString(pythonRuntime)
	return b.String(), nil
}

func wrapGo(body string) (string, error) {
	body = strings.TrimSpace(body)
	hasDelete := goDeleteFnRe.MatchString(body)
	hasMain := goMainFnRe.MatchString(body)
	if !hasDelete && !hasMain {
		return "", fmt.Errorf("Go 脚本须定义 archiveDelete() (int64, error)，或提供可编译的 main")
	}
	if !regexp.MustCompile(`(?m)^package\s+\w+`).MatchString(body) {
		body = "package main\n\n" + body
	}
	if hasDelete && !hasMain {
		if !strings.Contains(body, `"fmt"`) {
			body = addGoImport(body, "fmt")
		}
		if !strings.Contains(body, `"os"`) {
			body = addGoImport(body, "os")
		}
		body = strings.TrimRight(body, "\n") + "\n" + goRuntime
	}
	return body, nil
}

func addGoImport(src, path string) string {
	imp := "\t\"" + path + "\"\n"
	if strings.Contains(src, "import (\n") {
		return strings.Replace(src, "import (\n", "import (\n"+imp, 1)
	}
	if i := strings.Index(src, "\n"); i >= 0 {
		return src[:i] + "\n\nimport " + strconv.Quote(path) + src[i:]
	}
	return src + "\nimport " + strconv.Quote(path) + "\n"
}

func compilePython(src string) error {
	bin, err := exec.LookPath("python3")
	if err != nil {
		return fmt.Errorf("未找到 python3，无法在提交时检查脚本")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-c", "import sys; compile(sys.stdin.read(), 'archive.py', 'exec')")
	cmd.Stdin = strings.NewReader(src)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("Python 脚本检查超时")
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("Python 脚本不完整: %s", msg)
	}
	return nil
}

func buildGo(ctx context.Context, src string) (string, string, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return "", "", fmt.Errorf("未找到 go，无法编译脚本")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
	}
	dir, err := os.MkdirTemp("", "auto-dba-archive-go-*")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", err
	}
	mod := "module archive-script\n\ngo 1.21\n"
	if strings.Contains(src, "github.com/") || strings.Contains(src, "golang.org/") {
		mod += "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", err
	}
	bin := filepath.Join(dir, "archive-script")
	if err := goBuild(ctx, dir, bin); err != nil && (strings.Contains(src, "github.com/") || strings.Contains(src, "golang.org/")) {
		if tidyErr := goModTidy(ctx, dir); tidyErr != nil {
			return dir, "", fmt.Errorf("Go 依赖整理失败: %v", tidyErr)
		}
		if err = goBuild(ctx, dir, bin); err != nil {
			return dir, "", err
		}
		return dir, bin, nil
	} else if err != nil {
		return dir, "", err
	}
	return dir, bin, nil
}

func goBuild(ctx context.Context, dir, out string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("Go 编译超时")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("Go 编译失败: %s", msg)
	}
	return nil
}

func goModTidy(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func runScript(ctx context.Context, db *sql.DB, scriptType, body string, on func(Progress)) error {
	kind := normalizeScriptKind(scriptType)
	switch kind {
	case "sql":
		return runSQLScript(ctx, db, body, on)
	case "python":
		wrapped, err := wrapPython(body)
		if err != nil {
			return err
		}
		return runPython(ctx, wrapped, on)
	case "go":
		src, err := wrapGo(body)
		if err != nil {
			return err
		}
		return runGo(ctx, src, on)
	case "shell":
		return fmt.Errorf("当前不支持 Shell 脚本")
	default:
		return fmt.Errorf("当前不支持 %s 脚本", kind)
	}
}

func runSQLScript(ctx context.Context, db *sql.DB, body string, on func(Progress)) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("脚本内容不能为空")
	}
	var total int64
	var rounds int
	for _, stmt := range splitSQL(body) {
		if err := ctx.Err(); err != nil {
			return errStopped
		}
		n, err := execAffected(ctx, db, stmt)
		if err != nil {
			report(on, Progress{Rounds: rounds, DeletedRows: total, SQL: stmt})
			return err
		}
		total += n
		rounds++
		report(on, interim(rounds, total, stmt))
	}
	report(on, Progress{Rounds: rounds, DeletedRows: total, SQL: body, Message: fmt.Sprintf("脚本完成，删除 %d 行", total)})
	return nil
}

func runPython(ctx context.Context, src string, on func(Progress)) error {
	bin, err := exec.LookPath("python3")
	if err != nil {
		return fmt.Errorf("执行节点未安装 python3")
	}
	dir, err := os.MkdirTemp("", "auto-dba-archive-py-run-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "archive.py")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		return err
	}
	return runProgram(ctx, on, dir, bin, path)
}

func runGo(ctx context.Context, src string, on func(Progress)) error {
	dir, bin, err := buildGo(ctx, src)
	if dir != "" {
		defer os.RemoveAll(dir)
	}
	if err != nil {
		return err
	}
	return runProgram(ctx, on, dir, bin)
}

func runProgram(ctx context.Context, on func(Progress), dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	n := parseAffected(stdout.String())
	msg := fmt.Sprintf("脚本完成，删除 %d 行", n)
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if ctx.Err() != nil {
			return errStopped
		}
		if detail == "" {
			detail = err.Error()
		}
		if len(detail) > 2000 {
			detail = detail[:2000]
		}
		report(on, Progress{Rounds: 1, DeletedRows: n, Message: detail})
		return fmt.Errorf("脚本失败: %s", detail)
	}
	report(on, Progress{Rounds: 1, DeletedRows: n, Message: msg})
	return nil
}

func parseAffected(stdout string) int64 {
	matches := affectedRe.FindAllStringSubmatch(stdout, -1)
	if len(matches) == 0 {
		return 0
	}
	n, err := strconv.ParseInt(matches[len(matches)-1][1], 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// RenderGoScript 用填写项生成可编译的 Go 删除脚本。
func RenderGoScript(p ScriptParams) (string, error) {
	p = normalizeScriptParams(p)
	if err := validateScriptParams(p); err != nil {
		return "", err
	}
	tables := make([]string, 0, len(p.Tables))
	for _, t := range p.Tables {
		tables = append(tables, strconv.Quote(t))
	}
	return renderGoDeleteSource(p, strings.Join(tables, ", ")), nil
}

func renderGoDeleteSource(p ScriptParams, tableLiterals string) string {
	return fmt.Sprintf(`package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"
)

var (
	host       = %s
	port       = %d
	user       = %s
	password   = %s
	dbname     = %s
	tables     = []string{%s}
	retainDays = %d
	batchLimit = %d
)

func archiveDelete() (int64, error) {
	dsn := fmt.Sprintf("host=%%s port=%%d user=%%s password=%%s dbname=%%s sslmode=disable connect_timeout=10", host, port, user, password, dbname)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return 0, err
	}
	var total int64
	cond := fmt.Sprintf("create_time < CURRENT_DATE - INTERVAL '%%d days'", retainDays)
	for _, tbl := range tables {
		n, err := deleteTable(db, tbl, cond)
		total += n
		if err != nil {
			fmt.Printf("affected_row=%%d\n", total)
			return total, err
		}
	}
	return total, nil
}

func deleteTable(db *sql.DB, tbl, cond string) (int64, error) {
	var pk string
	err := db.QueryRow("SELECT a.attname FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey) WHERE i.indrelid = $1::regclass AND i.indisprimary ORDER BY a.attnum LIMIT 1", tbl).Scan(&pk)
	if err != nil {
		return 0, fmt.Errorf("表 %%s 无主键: %%w", tbl, err)
	}
	q := fmt.Sprintf("WITH cte1 AS (SELECT %%s FROM %%s WHERE %%s LIMIT %%d), cte2 AS (DELETE FROM %%s WHERE %%s IN (SELECT %%s FROM cte1)) SELECT * FROM cte1", quoteIdent(pk), quoteIdent(tbl), cond, batchLimit, quoteIdent(tbl), quoteIdent(pk), quoteIdent(pk))
	var total int64
	for {
		tx, err := db.Begin()
		if err != nil {
			return total, err
		}
		rows, err := tx.Query(q)
		if err != nil {
			_ = tx.Rollback()
			return total, err
		}
		var n int64
		cols, _ := rows.Columns()
		for rows.Next() {
			holders := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range holders {
				ptrs[i] = &holders[i]
			}
			_ = rows.Scan(ptrs...)
			n++
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			_ = tx.Rollback()
			return total, err
		}
		if err := tx.Commit(); err != nil {
			return total, err
		}
		total += n
		fmt.Printf("table=%%s affected_row=%%d\n", tbl, total)
		if n == 0 {
			return total, nil
		}
		time.Sleep(600 * time.Millisecond)
	}
}

func quoteIdent(name string) string {
	return "\"" + strings.ReplaceAll(name, "\"", "\"\"") + "\""
}
`, goString(p.Host), p.Port, goString(p.User), goString(p.Password), goString(p.DBName), tableLiterals, p.RetainDays, p.BatchLimit)
}

func normalizeScriptParams(p ScriptParams) ScriptParams {
	p.Host = strings.TrimSpace(p.Host)
	p.User = strings.TrimSpace(p.User)
	p.Password = strings.TrimSpace(p.Password)
	p.DBName = strings.TrimSpace(p.DBName)
	if p.Port <= 0 {
		p.Port = 5432
	}
	if p.RetainDays <= 0 {
		p.RetainDays = 180
	}
	if p.BatchLimit <= 0 {
		p.BatchLimit = pgDeleteBatch
	}
	tables := make([]string, 0, len(p.Tables))
	for _, t := range p.Tables {
		t = strings.TrimSpace(t)
		if t != "" {
			tables = append(tables, t)
		}
	}
	p.Tables = tables
	return p
}

func validateScriptParams(p ScriptParams) error {
	var missing []string
	if p.Host == "" {
		missing = append(missing, "host")
	}
	if p.User == "" {
		missing = append(missing, "user")
	}
	if p.Password == "" {
		missing = append(missing, "password")
	}
	if p.DBName == "" {
		missing = append(missing, "dbname")
	}
	if len(p.Tables) == 0 {
		missing = append(missing, "tables")
	}
	if len(missing) > 0 {
		return fmt.Errorf("请填写: %s", strings.Join(missing, ", "))
	}
	if err := validateIdent(p.DBName, "dbname"); err != nil {
		return err
	}
	for _, t := range p.Tables {
		if err := validateIdent(t, "tables"); err != nil {
			return err
		}
	}
	return nil
}

func goString(s string) string {
	return strconv.Quote(s)
}

func ScriptParamsFromMap(m map[string]any) ScriptParams {
	var p ScriptParams
	p.Host = strOf(m, "host")
	p.User = strOf(m, "user")
	p.Password = strOf(m, "password")
	p.DBName = strOf(m, "dbname")
	p.Port = intOf(m, "port", 5432)
	p.RetainDays = intOf(m, "retain_days", 180)
	p.BatchLimit = intOf(m, "batch_limit", pgDeleteBatch)
	switch v := m["tables"].(type) {
	case string:
		p.Tables = splitParamList(v)
	case []any:
		for _, it := range v {
			if s, ok := it.(string); ok {
				p.Tables = append(p.Tables, strings.TrimSpace(s))
			}
		}
	}
	return p
}

func splitParamList(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n' || r == ' '
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), `"'`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
