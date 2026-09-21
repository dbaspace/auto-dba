package flashback

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

type Runtime struct {
	Store  *store.DB
	Secret string
	Cfg    config.FlashbackConfig
}

var runtime *Runtime

func SetRuntime(r *Runtime) { runtime = r }

func fbLog(msg string, args ...any) {
	if len(args) == 0 {
		log.Printf("flashback %s", msg)
		return
	}
	log.Printf("flashback %s %v", msg, args)
}

func getGlobalArgStrDefault(_ context.Context, key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(strings.ToUpper(key))); v != "" {
		return v
	}
	if runtime != nil {
		switch key {
		case gaFlashbackWorkDir:
			if runtime.Cfg.WorkDir != "" {
				return runtime.Cfg.WorkDir
			}
		case gaFlashbackArchiveDir:
			if runtime.Cfg.ArchiveDir != "" {
				return runtime.Cfg.ArchiveDir
			}
		case gaFlashbackTencentSecretID:
			if runtime.Cfg.TencentSecretID != "" {
				return runtime.Cfg.TencentSecretID
			}
		case gaFlashbackTencentSecretKey:
			if runtime.Cfg.TencentSecretKey != "" {
				return runtime.Cfg.TencentSecretKey
			}
		case gaFlashbackTencentRegion:
			if runtime.Cfg.TencentRegion != "" {
				return runtime.Cfg.TencentRegion
			}
		case gaFlashbackMaxWALBytes:
			if runtime.Cfg.MaxWALBytes > 0 {
				return strconv.FormatInt(runtime.Cfg.MaxWALBytes, 10)
			}
		}
		if runtime.Store != nil {
			if v, err := runtime.Store.GetGlobalArg(key); err == nil && strings.TrimSpace(v) != "" {
				return v
			}
		}
	}
	return fallback
}

func getGlobalArgIntDefault(ctx context.Context, key string, fallback int) int {
	v := getGlobalArgStrDefault(ctx, key, "")
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

var runMu sync.Mutex

func flashbackAcquireRunLock(ctx context.Context, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	for {
		if runMu.TryLock() {
			return runMu.Unlock, nil
		}
		if time.Now().After(deadline) {
			return nil, errWaitLock
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

var errWaitLock = errString("等待闪回执行锁超时")

type errString string

func (e errString) Error() string { return string(e) }

func flashbackRunLockWait(ctx context.Context) time.Duration {
	sec := getGlobalArgIntDefault(ctx, "flashback_run_lock_wait_sec", 120)
	return time.Duration(sec) * time.Second
}

func flashbackLockWaitBounded(_ context.Context, d time.Duration) time.Duration {
	if d <= 0 {
		return 2 * time.Minute
	}
	return d
}
