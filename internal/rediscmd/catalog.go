package rediscmd

import (
	"fmt"
	"strings"
)

// Arg 描述命令参数（对齐官方 Commands 页常见字段）。
type Arg struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Hint     string `json:"hint,omitempty"`
}

// Command 内置 CRUD 命令目录项。
type Command struct {
	Group   string `json:"group"`
	Name    string `json:"name"`
	Syntax  string `json:"syntax"`
	Args    []Arg  `json:"args"`
	DocsURL string `json:"docs_url"`
	CRUD    string `json:"crud"`
	MinArgs int    `json:"min_args"`
}

var blacklist = map[string]struct{}{
	"FLUSHALL": {}, "FLUSHDB": {}, "CONFIG": {}, "SHUTDOWN": {}, "DEBUG": {},
	"SAVE": {}, "BGREWRITEAOF": {}, "BGSAVE": {}, "SLAVEOF": {}, "REPLICAOF": {},
	"KEYS": {}, "MONITOR": {}, "SYNC": {}, "PSYNC": {}, "MODULE": {}, "ACL": {},
	"CLUSTER": {}, "MIGRATE": {}, "RESTORE": {}, "SWAPDB": {},
}

var catalog = []Command{
	// String
	{Group: "String", Name: "SET", Syntax: "SET key value [EX seconds|PX milliseconds] [NX|XX]", Args: []Arg{{"key", true, ""}, {"value", true, ""}, {"options", false, "可选 EX/PX/NX/XX"}}, DocsURL: "https://redis.io/docs/latest/commands/set/", CRUD: "create", MinArgs: 2},
	{Group: "String", Name: "GET", Syntax: "GET key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/get/", CRUD: "read", MinArgs: 1},
	{Group: "String", Name: "MSET", Syntax: "MSET key value [key value ...]", Args: []Arg{{"key", true, ""}, {"value", true, ""}, {"pairs", false, "继续 key value"}}, DocsURL: "https://redis.io/docs/latest/commands/mset/", CRUD: "create", MinArgs: 2},
	{Group: "String", Name: "MGET", Syntax: "MGET key [key ...]", Args: []Arg{{"key", true, ""}, {"keys", false, "更多 key"}}, DocsURL: "https://redis.io/docs/latest/commands/mget/", CRUD: "read", MinArgs: 1},
	{Group: "String", Name: "INCR", Syntax: "INCR key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/incr/", CRUD: "update", MinArgs: 1},
	{Group: "String", Name: "DECR", Syntax: "DECR key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/decr/", CRUD: "update", MinArgs: 1},
	{Group: "String", Name: "APPEND", Syntax: "APPEND key value", Args: []Arg{{"key", true, ""}, {"value", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/append/", CRUD: "update", MinArgs: 2},
	{Group: "String", Name: "STRLEN", Syntax: "STRLEN key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/strlen/", CRUD: "read", MinArgs: 1},
	{Group: "String", Name: "GETRANGE", Syntax: "GETRANGE key start end", Args: []Arg{{"key", true, ""}, {"start", true, ""}, {"end", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/getrange/", CRUD: "read", MinArgs: 3},
	// Hash
	{Group: "Hash", Name: "HSET", Syntax: "HSET key field value [field value ...]", Args: []Arg{{"key", true, ""}, {"field", true, ""}, {"value", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/hset/", CRUD: "create", MinArgs: 3},
	{Group: "Hash", Name: "HGET", Syntax: "HGET key field", Args: []Arg{{"key", true, ""}, {"field", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/hget/", CRUD: "read", MinArgs: 2},
	{Group: "Hash", Name: "HMGET", Syntax: "HMGET key field [field ...]", Args: []Arg{{"key", true, ""}, {"field", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/hmget/", CRUD: "read", MinArgs: 2},
	{Group: "Hash", Name: "HGETALL", Syntax: "HGETALL key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/hgetall/", CRUD: "read", MinArgs: 1},
	{Group: "Hash", Name: "HDEL", Syntax: "HDEL key field [field ...]", Args: []Arg{{"key", true, ""}, {"field", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/hdel/", CRUD: "delete", MinArgs: 2},
	{Group: "Hash", Name: "HEXISTS", Syntax: "HEXISTS key field", Args: []Arg{{"key", true, ""}, {"field", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/hexists/", CRUD: "read", MinArgs: 2},
	{Group: "Hash", Name: "HLEN", Syntax: "HLEN key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/hlen/", CRUD: "read", MinArgs: 1},
	{Group: "Hash", Name: "HSTRLEN", Syntax: "HSTRLEN key field", Args: []Arg{{"key", true, ""}, {"field", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/hstrlen/", CRUD: "read", MinArgs: 2},
	{Group: "Hash", Name: "HKEYS", Syntax: "HKEYS key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/hkeys/", CRUD: "read", MinArgs: 1},
	{Group: "Hash", Name: "HVALS", Syntax: "HVALS key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/hvals/", CRUD: "read", MinArgs: 1},
	{Group: "Hash", Name: "HSCAN", Syntax: "HSCAN key cursor [MATCH pattern] [COUNT count]", Args: []Arg{{"key", true, ""}, {"cursor", true, "如 0"}}, DocsURL: "https://redis.io/docs/latest/commands/hscan/", CRUD: "read", MinArgs: 2},
	// List
	{Group: "List", Name: "LPUSH", Syntax: "LPUSH key element [element ...]", Args: []Arg{{"key", true, ""}, {"element", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/lpush/", CRUD: "create", MinArgs: 2},
	{Group: "List", Name: "RPUSH", Syntax: "RPUSH key element [element ...]", Args: []Arg{{"key", true, ""}, {"element", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/rpush/", CRUD: "create", MinArgs: 2},
	{Group: "List", Name: "LPOP", Syntax: "LPOP key [count]", Args: []Arg{{"key", true, ""}, {"count", false, ""}}, DocsURL: "https://redis.io/docs/latest/commands/lpop/", CRUD: "delete", MinArgs: 1},
	{Group: "List", Name: "RPOP", Syntax: "RPOP key [count]", Args: []Arg{{"key", true, ""}, {"count", false, ""}}, DocsURL: "https://redis.io/docs/latest/commands/rpop/", CRUD: "delete", MinArgs: 1},
	{Group: "List", Name: "LRANGE", Syntax: "LRANGE key start stop", Args: []Arg{{"key", true, ""}, {"start", true, "如 0"}, {"stop", true, "如 -1"}}, DocsURL: "https://redis.io/docs/latest/commands/lrange/", CRUD: "read", MinArgs: 3},
	{Group: "List", Name: "LLEN", Syntax: "LLEN key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/llen/", CRUD: "read", MinArgs: 1},
	{Group: "List", Name: "LINDEX", Syntax: "LINDEX key index", Args: []Arg{{"key", true, ""}, {"index", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/lindex/", CRUD: "read", MinArgs: 2},
	{Group: "List", Name: "LSET", Syntax: "LSET key index element", Args: []Arg{{"key", true, ""}, {"index", true, ""}, {"element", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/lset/", CRUD: "update", MinArgs: 3},
	// Set
	{Group: "Set", Name: "SADD", Syntax: "SADD key member [member ...]", Args: []Arg{{"key", true, ""}, {"member", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/sadd/", CRUD: "create", MinArgs: 2},
	{Group: "Set", Name: "SREM", Syntax: "SREM key member [member ...]", Args: []Arg{{"key", true, ""}, {"member", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/srem/", CRUD: "delete", MinArgs: 2},
	{Group: "Set", Name: "SMEMBERS", Syntax: "SMEMBERS key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/smembers/", CRUD: "read", MinArgs: 1},
	{Group: "Set", Name: "SISMEMBER", Syntax: "SISMEMBER key member", Args: []Arg{{"key", true, ""}, {"member", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/sismember/", CRUD: "read", MinArgs: 2},
	{Group: "Set", Name: "SCARD", Syntax: "SCARD key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/scard/", CRUD: "read", MinArgs: 1},
	{Group: "Set", Name: "SRANDMEMBER", Syntax: "SRANDMEMBER key [count]", Args: []Arg{{"key", true, ""}, {"count", false, ""}}, DocsURL: "https://redis.io/docs/latest/commands/srandmember/", CRUD: "read", MinArgs: 1},
	{Group: "Set", Name: "SSCAN", Syntax: "SSCAN key cursor [MATCH pattern] [COUNT count]", Args: []Arg{{"key", true, ""}, {"cursor", true, "如 0"}}, DocsURL: "https://redis.io/docs/latest/commands/sscan/", CRUD: "read", MinArgs: 2},
	// ZSet
	{Group: "ZSet", Name: "ZADD", Syntax: "ZADD key score member [score member ...]", Args: []Arg{{"key", true, ""}, {"score", true, ""}, {"member", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/zadd/", CRUD: "create", MinArgs: 3},
	{Group: "ZSet", Name: "ZREM", Syntax: "ZREM key member [member ...]", Args: []Arg{{"key", true, ""}, {"member", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/zrem/", CRUD: "delete", MinArgs: 2},
	{Group: "ZSet", Name: "ZRANGE", Syntax: "ZRANGE key start stop [WITHSCORES]", Args: []Arg{{"key", true, ""}, {"start", true, ""}, {"stop", true, ""}, {"WITHSCORES", false, "可选"}}, DocsURL: "https://redis.io/docs/latest/commands/zrange/", CRUD: "read", MinArgs: 3},
	{Group: "ZSet", Name: "ZSCORE", Syntax: "ZSCORE key member", Args: []Arg{{"key", true, ""}, {"member", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/zscore/", CRUD: "read", MinArgs: 2},
	{Group: "ZSet", Name: "ZCARD", Syntax: "ZCARD key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/zcard/", CRUD: "read", MinArgs: 1},
	{Group: "ZSet", Name: "ZRANK", Syntax: "ZRANK key member", Args: []Arg{{"key", true, ""}, {"member", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/zrank/", CRUD: "read", MinArgs: 2},
	{Group: "ZSet", Name: "ZREVRANK", Syntax: "ZREVRANK key member", Args: []Arg{{"key", true, ""}, {"member", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/zrevrank/", CRUD: "read", MinArgs: 2},
	{Group: "ZSet", Name: "ZCOUNT", Syntax: "ZCOUNT key min max", Args: []Arg{{"key", true, ""}, {"min", true, ""}, {"max", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/zcount/", CRUD: "read", MinArgs: 3},
	{Group: "ZSet", Name: "ZRANGEBYSCORE", Syntax: "ZRANGEBYSCORE key min max", Args: []Arg{{"key", true, ""}, {"min", true, ""}, {"max", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/zrangebyscore/", CRUD: "read", MinArgs: 3},
	{Group: "ZSet", Name: "ZREVRANGE", Syntax: "ZREVRANGE key start stop", Args: []Arg{{"key", true, ""}, {"start", true, ""}, {"stop", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/zrevrange/", CRUD: "read", MinArgs: 3},
	{Group: "ZSet", Name: "ZSCAN", Syntax: "ZSCAN key cursor [MATCH pattern] [COUNT count]", Args: []Arg{{"key", true, ""}, {"cursor", true, "如 0"}}, DocsURL: "https://redis.io/docs/latest/commands/zscan/", CRUD: "read", MinArgs: 2},
	// Key
	{Group: "Key", Name: "DEL", Syntax: "DEL key [key ...]", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/del/", CRUD: "delete", MinArgs: 1},
	{Group: "Key", Name: "EXISTS", Syntax: "EXISTS key [key ...]", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/exists/", CRUD: "read", MinArgs: 1},
	{Group: "Key", Name: "EXPIRE", Syntax: "EXPIRE key seconds", Args: []Arg{{"key", true, ""}, {"seconds", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/expire/", CRUD: "update", MinArgs: 2},
	{Group: "Key", Name: "TTL", Syntax: "TTL key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/ttl/", CRUD: "read", MinArgs: 1},
	{Group: "Key", Name: "PTTL", Syntax: "PTTL key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/pttl/", CRUD: "read", MinArgs: 1},
	{Group: "Key", Name: "TYPE", Syntax: "TYPE key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/type/", CRUD: "read", MinArgs: 1},
	{Group: "Key", Name: "DBSIZE", Syntax: "DBSIZE", Args: []Arg{}, DocsURL: "https://redis.io/docs/latest/commands/dbsize/", CRUD: "read", MinArgs: 0},
	{Group: "Key", Name: "OBJECT", Syntax: "OBJECT ENCODING|IDLETIME|REFCOUNT|FREQ key", Args: []Arg{{"subcommand", true, "ENCODING、IDLETIME、REFCOUNT、FREQ"}, {"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/object/", CRUD: "read", MinArgs: 2},
	{Group: "Key", Name: "MEMORY", Syntax: "MEMORY USAGE key", Args: []Arg{{"USAGE", true, ""}, {"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/memory-usage/", CRUD: "read", MinArgs: 2},
	{Group: "Key", Name: "SLOWLOG", Syntax: "SLOWLOG GET [count]", Args: []Arg{{"GET", true, ""}, {"count", false, ""}}, DocsURL: "https://redis.io/docs/latest/commands/slowlog-get/", CRUD: "read", MinArgs: 1},
	{Group: "Key", Name: "RENAME", Syntax: "RENAME key newkey", Args: []Arg{{"key", true, ""}, {"newkey", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/rename/", CRUD: "update", MinArgs: 2},
	// Stream
	{Group: "Stream", Name: "XLEN", Syntax: "XLEN key", Args: []Arg{{"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/xlen/", CRUD: "read", MinArgs: 1},
	{Group: "Stream", Name: "XRANGE", Syntax: "XRANGE key start end", Args: []Arg{{"key", true, ""}, {"start", true, ""}, {"end", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/xrange/", CRUD: "read", MinArgs: 3},
	{Group: "Stream", Name: "XREVRANGE", Syntax: "XREVRANGE key end start", Args: []Arg{{"key", true, ""}, {"end", true, ""}, {"start", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/xrevrange/", CRUD: "read", MinArgs: 3},
	{Group: "Stream", Name: "XINFO", Syntax: "XINFO STREAM|GROUPS|CONSUMERS key", Args: []Arg{{"subcommand", true, ""}, {"key", true, ""}}, DocsURL: "https://redis.io/docs/latest/commands/xinfo/", CRUD: "read", MinArgs: 1},
}

var allowedByName map[string]*Command

func init() {
	allowedByName = make(map[string]*Command, len(catalog))
	for i := range catalog {
		c := &catalog[i]
		allowedByName[strings.ToUpper(c.Name)] = c
	}
}

// Catalog 返回内置命令目录副本。
func Catalog() []Command {
	out := make([]Command, len(catalog))
	copy(out, catalog)
	return out
}

// Lookup 按命令名查找目录项。
func Lookup(name string) *Command {
	if c, ok := allowedByName[strings.ToUpper(strings.TrimSpace(name))]; ok {
		return c
	}
	return nil
}

// IsDangerousCommand 是否为内置危险命令。KEYS、CONFIG、FLUSHALL 等都在此列。
func IsDangerousCommand(name string) bool {
	_, ok := blacklist[strings.ToUpper(strings.TrimSpace(name))]
	return ok
}

var objectReadSub = map[string]struct{}{
	"ENCODING": {}, "IDLETIME": {}, "REFCOUNT": {}, "FREQ": {},
}

// CheckReadSubcommand 限制带写子命令的只读入口。其它命令返回 nil。
func CheckReadSubcommand(name string, args []string) error {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "OBJECT":
		if len(args) == 0 {
			return fmt.Errorf("OBJECT 查询只允许 ENCODING、IDLETIME、REFCOUNT、FREQ")
		}
		if _, ok := objectReadSub[strings.ToUpper(args[0])]; !ok {
			return fmt.Errorf("OBJECT 查询只允许 ENCODING、IDLETIME、REFCOUNT、FREQ")
		}
	case "MEMORY":
		if len(args) == 0 || !strings.EqualFold(args[0], "USAGE") {
			return fmt.Errorf("MEMORY 查询只允许 USAGE")
		}
	case "SLOWLOG":
		if len(args) == 0 || !strings.EqualFold(args[0], "GET") {
			return fmt.Errorf("SLOWLOG 查询只允许 GET")
		}
	}
	return nil
}

// ValidateCommand 校验首词在白名单、不在黑名单，且参数个数大致满足（使用内置默认规则）。
func ValidateCommand(line string) error {
	return ValidateCommandWith(nil, line)
}

// IsAllowedVerb 仅检查动词是否白名单（默认规则且已启用）。
func IsAllowedVerb(name string) bool {
	for _, c := range EnabledCatalog(nil) {
		if strings.EqualFold(c.Name, name) {
			return true
		}
	}
	return false
}

// ScriptStats 多行脚本解析结果。
type ScriptStats struct {
	Valid         []string `json:"valid"`
	Invalid       []string `json:"invalid"`
	CommentCount  int      `json:"comment_count"`
	HighestCRUD   string   `json:"highest_crud"`
	HighestRiskCN string   `json:"highest_risk_cn"`
}

var crudRank = map[string]int{
	"read": 1, "create": 2, "update": 3, "delete": 4,
}

func crudCN(crud string) string {
	switch strings.ToLower(crud) {
	case "delete":
		return "删除"
	case "update":
		return "更新"
	case "create":
		return "写入"
	case "read":
		return "读取"
	default:
		return "—"
	}
}

// ParseScript 解析多行脚本（默认规则）。
func ParseScript(text string) ScriptStats {
	return ParseScriptWith(nil, text)
}

// ValidateScript 要求至少一条有效命令且无非法行（默认规则）。
func ValidateScript(text string) error {
	return ValidateScriptWith(nil, text)
}

// ValidCommands 返回可执行命令行列表（默认规则）。
func ValidCommands(text string) ([]string, error) {
	return ValidCommandsWith(nil, text)
}
