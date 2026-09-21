package redisquery

import (
	"context"
	"fmt"

	"auto-dba/internal/store"
)

type Database struct {
	Name string `json:"name"`
	Keys int64  `json:"keys"`
}

func ListDatabasesWithKeys(ctx context.Context, inst *store.Instance, opt Options) ([]Database, error) {
	if inst == nil {
		return nil, fmt.Errorf("实例不存在")
	}
	client, err := openReadyClient(ctx, inst, opt, 0)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	conn := client.Conn()
	defer conn.Close()
	out := make([]Database, 0, 16)
	for i := 0; i < 16; i++ {
		if err := conn.Select(ctx, i).Err(); err != nil {
			out = append(out, Database{Name: fmt.Sprintf("db%d", i), Keys: -1})
			continue
		}
		n, err := conn.DBSize(ctx).Result()
		if err != nil {
			out = append(out, Database{Name: fmt.Sprintf("db%d", i), Keys: -1})
			continue
		}
		out = append(out, Database{Name: fmt.Sprintf("db%d", i), Keys: n})
	}
	return out, nil
}
