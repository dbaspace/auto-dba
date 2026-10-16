package sessionstore

import (
	"context"
	"log"
	"time"

	"auto-dba/internal/store"
)

// StartReconcile 定时对账：元库 online ↔ Redis ↔ ZSET。ctx 取消即停。
func StartReconcile(ctx context.Context, s *Store, db *store.DB) {
	if s == nil || db == nil {
		return
	}
	go func() {
		// 启动后稍晚跑一次
		t := time.NewTimer(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if s.Enabled() {
					if n, err := ReconcileOnce(ctx, s, db); err != nil {
						log.Printf("sessionstore reconcile: %v", err)
					} else if n > 0 {
						log.Printf("sessionstore reconcile: fixed %d inconsistencies", n)
					}
				}
				t.Reset(s.ReconcileInterval())
			}
		}
	}()
}

// ReconcileOnce 执行一轮对账，返回修复条数。
func ReconcileOnce(ctx context.Context, s *Store, db *store.DB) (int, error) {
	if s == nil || !s.Enabled() || db == nil {
		return 0, nil
	}
	fixed := 0
	// ① 元库 online 但 Redis 无用户 key → expired
	rows, err := db.ListOnlineUserSessionRows(5000)
	if err != nil {
		return 0, err
	}
	seenUser := map[string]bool{}
	for _, row := range rows {
		exists, e := s.UserKeyExists(ctx, row.UserID)
		if e != nil {
			return fixed, e
		}
		if !exists {
			if e := db.MarkUserSessionExpired(row.JTI); e == nil {
				fixed++
			}
			continue
		}
		// 用户 key 在，但该 jti 可能已被踢：再查会话列表
		sessList, e := s.SessionsOfUser(ctx, row.UserID)
		if e != nil {
			return fixed, e
		}
		found := false
		for _, ss := range sessList {
			if ss.JTI == row.JTI {
				found = true
				break
			}
		}
		if !found {
			if e := db.MarkUserSessionExpired(row.JTI); e == nil {
				fixed++
			}
		}
		seenUser[row.UserID] = true
	}
	// ② ZSET 孤儿
	ids, err := s.OnlineUserIDsAll(ctx)
	if err != nil {
		return fixed, err
	}
	for _, uid := range ids {
		exists, e := s.UserKeyExists(ctx, uid)
		if e != nil {
			return fixed, e
		}
		if !exists {
			_ = s.RemoveOnlineMember(ctx, uid)
			fixed++
		}
	}
	_ = seenUser
	return fixed, nil
}
