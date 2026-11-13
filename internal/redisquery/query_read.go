package redisquery

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"auto-dba/internal/rediscmd"
)

// CheckQueryCommand 在连接 Redis 之前判断查询页能否执行该命令。
// 危险命令、写命令和目录外命令在这里拒绝。
func CheckQueryCommand(command string) error {
	parts, err := rediscmd.SplitArgs(command)
	if err != nil {
		return fmt.Errorf("命令解析失败: %v", err)
	}
	if len(parts) == 0 {
		return fmt.Errorf("命令不能为空")
	}
	cmd := strings.ToUpper(parts[0])
	if rediscmd.IsDangerousCommand(cmd) {
		return fmt.Errorf("禁止执行危险命令: %s", cmd)
	}
	switch cmd {
	case "GET", "INFO", "SCAN":
		return nil
	}
	meta := rediscmd.Lookup(cmd)
	if meta == nil || !strings.EqualFold(meta.CRUD, "read") {
		return fmt.Errorf("查询仅允许只读命令")
	}
	if err := rediscmd.CheckReadSubcommand(cmd, parts[1:]); err != nil {
		return err
	}
	argc := len(parts) - 1
	if argc < meta.MinArgs {
		return fmt.Errorf("%s 至少需要 %d 个参数（当前 %d）", meta.Name, meta.MinArgs, argc)
	}
	return nil
}

func capScanCount(parts []string) []string {
	out := append([]string(nil), parts...)
	for i := 0; i+1 < len(out); i++ {
		if !strings.EqualFold(out[i], "COUNT") {
			continue
		}
		n, err := strconv.ParseInt(out[i+1], 10, 64)
		if err == nil && n > 1000 {
			out[i+1] = "1000"
		}
	}
	return out
}

func execRead(ctx context.Context, client *redis.Client, parts []string, start time.Time) (*Result, error) {
	cmd := strings.ToUpper(parts[0])
	switch cmd {
	case "HSCAN", "SSCAN", "ZSCAN":
		parts = capScanCount(parts)
	}
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
