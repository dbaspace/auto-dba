package kafka

import (
	"context"
	"fmt"
	"sort"

	"auto-dba/internal/store"
)

func ListPartitions(ctx context.Context, inst *store.Instance, topic string) ([]int, error) {
	addr := bootstrapAddr(inst)
	conn, err := dialBroker(ctx, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	partitions, err := conn.ReadPartitions()
	if err != nil {
		return nil, fmt.Errorf("read partitions: %w", err)
	}
	var ids []int
	seen := make(map[int]struct{})
	for _, p := range partitions {
		if p.Topic != topic {
			continue
		}
		if _, ok := seen[p.ID]; !ok {
			seen[p.ID] = struct{}{}
			ids = append(ids, p.ID)
		}
	}
	sort.Ints(ids)
	return ids, nil
}
