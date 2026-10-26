package redisquery

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"auto-dba/internal/rediscmd"
	"auto-dba/internal/store"
)

type Result struct {
	Columns  []string `json:"columns"`
	Rows     [][]any  `json:"rows"`
	RowCount int      `json:"row_count"`
	TimeMs   int64    `json:"execution_time_ms"`
	Raw      string   `json:"raw,omitempty"`
}

func Execute(ctx context.Context, inst *store.Instance, opt Options, dbName, command string) (*Result, error) {
	db, err := ParseDBIndex(dbName)
	if err != nil {
		return nil, err
	}
	parts := strings.Fields(strings.TrimSpace(command))
	if len(parts) == 0 {
		return nil, fmt.Errorf("命令不能为空")
	}
	cmd := strings.ToUpper(parts[0])
	args := parts[1:]
	client, err := openReadyClient(ctx, inst, opt, db)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	switch cmd {
	case "GET":
		return execGET(ctx, client, args, start)
	case "INFO":
		return execINFO(ctx, client, args, start)
	case "SCAN":
		return execSCAN(ctx, client, args, start)
	default:
		return nil, fmt.Errorf("仅允许只读命令 GET / INFO / SCAN（收到 %s）", cmd)
	}
}

// ExecuteAllowed 执行工单白名单内的 CRUD 命令（含写操作）。查询页请继续用 Execute。
// rules 为 nil 时使用内置默认规则。
func ExecuteAllowed(ctx context.Context, inst *store.Instance, opt Options, dbName, command string, rules *rediscmd.RulesConfig) (*Result, error) {
	if err := rediscmd.ValidateCommandWith(rules, command); err != nil {
		return nil, err
	}
	db, err := ParseDBIndex(dbName)
	if err != nil {
		return nil, err
	}
	parts, err := rediscmd.SplitArgs(command)
	if err != nil {
		return nil, fmt.Errorf("命令解析失败: %w", err)
	}
	client, err := openReadyClient(ctx, inst, opt, db)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	noteRedisSession(ctx, client)
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	args := make([]any, len(parts))
	for i, p := range parts {
		args[i] = p
	}
	val, err := client.Do(ctx, args...).Result()
	if err == redis.Nil {
		return resultRaw([]string{"result"}, [][]any{{nil}}, start, "nil"), nil
	}
	if err != nil {
		return nil, err
	}
	return formatDoResult(val, start), nil
}

func formatDoResult(val any, start time.Time) *Result {
	switch v := val.(type) {
	case string:
		return resultRaw([]string{"result"}, [][]any{{v}}, start, v)
	case []byte:
		s := string(v)
		return resultRaw([]string{"result"}, [][]any{{s}}, start, s)
	case int64:
		s := strconv.FormatInt(v, 10)
		return resultRaw([]string{"result"}, [][]any{{v}}, start, s)
	case bool:
		s := strconv.FormatBool(v)
		return resultRaw([]string{"result"}, [][]any{{v}}, start, s)
	case []any:
		rows := make([][]any, 0, len(v))
		for i, item := range v {
			rows = append(rows, []any{i, fmt.Sprint(item)})
		}
		if len(rows) == 0 {
			return resultRaw([]string{"index", "value"}, [][]any{}, start, "[]")
		}
		return resultRaw([]string{"index", "value"}, rows, start, fmt.Sprint(v))
	case map[string]string:
		rows := make([][]any, 0, len(v))
		for k, vv := range v {
			rows = append(rows, []any{k, vv})
		}
		return resultRaw([]string{"field", "value"}, rows, start, fmt.Sprint(v))
	default:
		s := fmt.Sprint(v)
		return resultRaw([]string{"result"}, [][]any{{s}}, start, s)
	}
}

func execGET(ctx context.Context, client *redis.Client, args []string, start time.Time) (*Result, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("GET 需要 key")
	}
	val, err := client.Get(ctx, args[0]).Result()
	if err == redis.Nil {
		return result([]string{"key", "value"}, [][]any{{args[0], nil}}, start), nil
	}
	if err != nil {
		return nil, err
	}
	return result([]string{"key", "value"}, [][]any{{args[0], val}}, start), nil
}

func execINFO(ctx context.Context, client *redis.Client, args []string, start time.Time) (*Result, error) {
	section := "server"
	if len(args) >= 1 {
		section = args[0]
	}
	info, err := client.Info(ctx, section).Result()
	if err != nil {
		return nil, err
	}
	rows := parseINFO(info)
	if len(rows) == 0 {
		return result([]string{"info"}, [][]any{{info}}, start), nil
	}
	return result([]string{"section", "key", "value"}, rows, start), nil
}

func parseINFO(raw string) [][]any {
	section := ""
	var rows [][]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			rows = append(rows, []any{section, line, ""})
			continue
		}
		rows = append(rows, []any{section, key, val})
	}
	return rows
}

func execSCAN(ctx context.Context, client *redis.Client, args []string, start time.Time) (*Result, error) {
	cursor := uint64(0)
	if len(args) >= 1 {
		if n, err := strconv.ParseUint(args[0], 10, 64); err == nil {
			cursor = n
		}
	}
	match := "*"
	count := int64(100)
	for i := 1; i < len(args); i += 2 {
		if i+1 >= len(args) {
			break
		}
		switch strings.ToUpper(args[i]) {
		case "MATCH":
			match = args[i+1]
		case "COUNT":
			if n, err := strconv.ParseInt(args[i+1], 10, 64); err == nil && n > 0 {
				count = n
			}
		}
	}
	if count > 1000 {
		count = 1000
	}
	keys, next, err := client.Scan(ctx, cursor, match, count).Result()
	if err != nil {
		return nil, err
	}
	rows := [][]any{{"cursor", next}}
	for _, k := range keys {
		rows = append(rows, []any{k, nil})
	}
	return result([]string{"cursor_or_key", "next_or_empty"}, rows, start), nil
}

func result(cols []string, rows [][]any, start time.Time) *Result {
	return resultRaw(cols, rows, start, "")
}

func resultRaw(cols []string, rows [][]any, start time.Time, raw string) *Result {
	return &Result{
		Columns:  cols,
		Rows:     rows,
		RowCount: len(rows),
		TimeMs:   time.Since(start).Milliseconds(),
		Raw:      raw,
	}
}
