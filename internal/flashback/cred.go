package flashback

import (
	"context"
	"strings"

	"auto-dba/internal/cloud"
)

type actorKey struct{}
type credKey struct{}

type credState struct {
	id, key, account, err string
}

// ResolveInstanceCred 由云账号模块注入。返回只读 SecretId、SecretKey 和账号 ID。
var ResolveInstanceCred func(ctx context.Context, instanceID, username string) (string, string, string, error)

func WithActor(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, actorKey{}, strings.TrimSpace(username))
}

func Actor(ctx context.Context) string {
	v, _ := ctx.Value(actorKey{}).(string)
	return v
}

func flashbackUseAccount(ctx context.Context, instanceID, username string) context.Context {
	if strings.TrimSpace(username) == "" {
		username = Actor(ctx)
	}
	if ResolveInstanceCred == nil {
		return context.WithValue(ctx, credKey{}, credState{err: "未绑定云账号"})
	}
	id, key, account, err := ResolveInstanceCred(ctx, instanceID, username)
	if err != nil {
		return context.WithValue(ctx, credKey{}, credState{err: err.Error()})
	}
	return context.WithValue(ctx, credKey{}, credState{id: id, key: key, account: account})
}

func cloudWait(ctx context.Context, account string) error {
	return cloud.DefaultLimiter().Wait(ctx, account)
}

func flashbackCred(ctx context.Context) (credState, bool) {
	v, ok := ctx.Value(credKey{}).(credState)
	return v, ok
}
