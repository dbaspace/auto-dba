package sessionstore

import (
	"context"
	"log"

	"auto-dba/internal/store"
)

// StartReconcile 已废弃：平台调度由 internal/jobreg 接管（session.reconcile）。
//
// Deprecated: 使用 jobboot.RegisterPlatformJobs + jobreg.Registry.Start。
func StartReconcile(ctx context.Context, s *Store, db *store.DB) {
	_ = ctx
	_ = s
	_ = db
	log.Printf("DEPRECATED: sessionstore.StartReconcile ignored; scheduling is owned by jobreg (session.reconcile)")
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
