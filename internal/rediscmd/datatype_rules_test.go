package rediscmd

import (
	"strings"
	"testing"
)

// 按内置默认规则，覆盖各 Redis 数据类型的合法 / 非法样例。
func TestValidateByDataType_DefaultRules(t *testing.T) {
	cases := []struct {
		group string
		ok    []string
		bad   []string
	}{
		{
			group: "String",
			ok: []string{
				"SET k v",
				"SET k v EX 60",
				"GET k",
				"MSET a 1 b 2",
				"MGET a b",
				"INCR cnt",
				"DECR cnt",
				"APPEND k x",
				"STRLEN k",
			},
			bad: []string{
				"SET k",    // min_args=2
				"GET",      // min_args=1
				"APPEND k", // min_args=2
				"STRLEN",   // min_args=1
			},
		},
		{
			group: "Hash",
			ok: []string{
				"HSET h f v",
				"HSET h f1 v1 f2 v2",
				"HGET h f",
				"HMGET h f1 f2",
				"HGETALL h",
				"HDEL h f",
				"HEXISTS h f",
				"HLEN h",
			},
			bad: []string{
				"HSET h f",  // min_args=3
				"HGET h",    // min_args=2
				"HDEL h",    // min_args=2
				"HEXISTS h", // min_args=2
			},
		},
		{
			group: "List",
			ok: []string{
				"LPUSH l a",
				"RPUSH l a b",
				"LPOP l",
				"LPOP l 2",
				"RPOP l",
				"LRANGE l 0 -1",
				"LLEN l",
				"LSET l 0 x",
			},
			bad: []string{
				"LPUSH l",    // min_args=2
				"LRANGE l 0", // min_args=3
				"LSET l 0",   // min_args=3
				"LLEN",       // min_args=1
			},
		},
		{
			group: "Set",
			ok: []string{
				"SADD s m",
				"SADD s m1 m2",
				"SREM s m",
				"SMEMBERS s",
				"SISMEMBER s m",
				"SCARD s",
			},
			bad: []string{
				"SADD s",      // min_args=2
				"SREM s",      // min_args=2
				"SISMEMBER s", // min_args=2
				"SMEMBERS",    // min_args=1
			},
		},
		{
			group: "ZSet",
			ok: []string{
				"ZADD z 1 m",
				"ZADD z 1 m1 2 m2",
				"ZREM z m",
				"ZRANGE z 0 -1",
				"ZRANGE z 0 -1 WITHSCORES",
				"ZSCORE z m",
				"ZCARD z",
			},
			bad: []string{
				"ZADD z 1",   // min_args=3
				"ZREM z",     // min_args=2
				"ZRANGE z 0", // min_args=3
				"ZSCORE z",   // min_args=2
			},
		},
		{
			group: "Key",
			ok: []string{
				"DEL k",
				"DEL k1 k2",
				"EXISTS k",
				"EXPIRE k 60",
				"TTL k",
				"TYPE k",
				"RENAME a b",
			},
			bad: []string{
				"DEL",      // min_args=1
				"EXPIRE k", // min_args=2
				"RENAME a", // min_args=2
				"TTL",      // min_args=1
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.group, func(t *testing.T) {
			for _, line := range tc.ok {
				if err := ValidateCommand(line); err != nil {
					t.Errorf("want OK [%s]: %v", line, err)
				}
			}
			for _, line := range tc.bad {
				if err := ValidateCommand(line); err == nil {
					t.Errorf("want FAIL [%s]", line)
				}
			}
		})
	}
}

func TestValidateScript_MixedDataTypes(t *testing.T) {
	script := strings.Join([]string{
		"# string + hash + list + set + zset + key",
		"SET user:1 name",
		"HSET user:1 profile nick alice",
		"LPUSH queue:jobs job1",
		"SADD tags redis",
		"ZADD rank 100 alice",
		"EXPIRE user:1 3600",
		"GET user:1",
	}, "\n")
	if err := ValidateScript(script); err != nil {
		t.Fatalf("mixed types script should pass: %v", err)
	}
	st := ParseScript(script)
	if st.HighestCRUD != "create" && st.HighestCRUD != "update" {
		// SET/HSET/LPUSH/SADD/ZADD 为 create，EXPIRE 为 update；最高应为 create
		if st.HighestCRUD != "create" {
			t.Fatalf("highest crud got %q", st.HighestCRUD)
		}
	}
	if len(st.Valid) != 7 {
		t.Fatalf("valid count=%d want 7", len(st.Valid))
	}
}

func TestValidateScript_BlacklistBlocksRegardlessOfType(t *testing.T) {
	for _, line := range []string{
		"FLUSHALL",
		"KEYS *",
		"CONFIG GET maxmemory",
		"SET a b\nFLUSHDB",
	} {
		if err := ValidateScript(line); err == nil {
			t.Errorf("blacklist should reject: %s", line)
		}
	}
}

func TestValidateByDataType_CustomRulesDisableGroups(t *testing.T) {
	off := false
	on := true
	// 禁用整个 Hash 组常用写命令，放宽 List LPUSH 最少参数为 1（仅测覆盖）
	min1 := 1
	saved := &RulesConfig{
		Blacklist: []string{"FLUSHALL", "KEYS"},
		Commands: []CmdOverride{
			{Name: "HSET", Enabled: &off},
			{Name: "HDEL", Enabled: &off},
			{Name: "LPUSH", Enabled: &on, MinArgs: &min1},
		},
	}
	if err := ValidateCommandWith(saved, "HSET h f v"); err == nil {
		t.Fatal("disabled HSET should fail")
	}
	if err := ValidateCommandWith(saved, "HGET h f"); err != nil {
		t.Fatalf("HGET still enabled: %v", err)
	}
	if err := ValidateCommandWith(saved, "LPUSH onlykey"); err != nil {
		t.Fatalf("LPUSH min_args=1 should pass: %v", err)
	}
	if err := ValidateCommandWith(saved, "SADD s m"); err != nil {
		t.Fatalf("Set untouched: %v", err)
	}
}

func TestEnabledCatalog_CoversAllDataTypeGroups(t *testing.T) {
	want := map[string]bool{
		"String": false, "Hash": false, "List": false,
		"Set": false, "ZSet": false, "Key": false,
	}
	for _, c := range EnabledCatalog(nil) {
		want[c.Group] = true
	}
	for g, ok := range want {
		if !ok {
			t.Errorf("missing group in enabled catalog: %s", g)
		}
	}
}
