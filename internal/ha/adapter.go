package ha

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"auto-dba/internal/store"

	"github.com/redis/go-redis/v9"
)

type ControllerAuth struct {
	Kind     string
	BaseURL  string
	Username string
	Password string
	AppConf  string // MHA app.cnf 路径（来自控制器 remark）
}

// PlannedSwitchover 仅在已登记且与 mode 兼容的控制器上执行计划切换；失败返回 error，由调用方回退清单。
func PlannedSwitchover(ctx context.Context, cluster *store.HACluster, ctrl ControllerAuth, candidate *store.Instance) error {
	if cluster == nil || candidate == nil {
		return fmt.Errorf("集群或目标实例为空")
	}
	kind := strings.ToLower(strings.TrimSpace(ctrl.Kind))
	switch kind {
	case "orchestrator":
		return orchestratorTakeover(ctx, ctrl, candidate)
	case "mha":
		return mhaAgentPlannedSwitch(ctx, ctrl, cluster, candidate)
	case "proxysql":
		oldPrimary := findRegisteredPrimary(cluster)
		return ProxySQLPromoteWriter(ctx, ctrl, candidate, oldPrimary)
	case "pg_autofailover":
		return pgafAgentPlannedSwitch(ctx, ctrl, candidate)
	case "patroni":
		name := ""
		if candidate != nil {
			name = strings.TrimSpace(candidate.Name)
			if name == "" {
				name = strings.TrimSpace(candidate.Host)
			}
		}
		return patroniSwitchover(ctx, ctrl, name)
	case "repmgr":
		return repmgrAgentSwitchover(ctx, ctrl, candidate)
	case "sentinel":
		return sentinelFailover(ctx, ctrl, cluster.Name, candidate)
	default:
		return fmt.Errorf("控制器类型 %s 不支持自动切换", kind)
	}
}

// ForcedFailover 主宕机/强制切换（审批后）。
func ForcedFailover(ctx context.Context, cluster *store.HACluster, ctrl ControllerAuth, candidate *store.Instance) error {
	if cluster == nil {
		return fmt.Errorf("集群为空")
	}
	kind := strings.ToLower(strings.TrimSpace(ctrl.Kind))
	switch kind {
	case "orchestrator":
		alias := ParseOrchClusterAlias(ctrl.AppConf)
		var primary *store.Instance
		if cluster != nil {
			primary = findRegisteredPrimary(cluster)
		}
		return orchestratorForceFailover(ctx, ctrl, primary, alias)
	case "mha":
		return MHAAgentDeadSwitch(ctx, ctrl, cluster, candidate)
	case "pg_autofailover":
		return pgafAgentFailover(ctx, ctrl, candidate)
	case "patroni":
		name := ""
		if candidate != nil {
			name = strings.TrimSpace(candidate.Name)
			if name == "" {
				name = strings.TrimSpace(candidate.Host)
			}
		}
		return patroniFailoverAPI(ctx, ctrl, name)
	default:
		return fmt.Errorf("控制器类型 %s 不支持强制切换", kind)
	}
}

func findRegisteredPrimary(cluster *store.HACluster) *store.Instance {
	if cluster == nil {
		return nil
	}
	for _, m := range cluster.Members {
		if strings.EqualFold(m.Role, "primary") && m.Enabled {
			return &store.Instance{
				ID: m.InstanceID, Name: m.InstanceName, Host: m.Host, Port: m.Port,
			}
		}
	}
	return nil
}

func sentinelFailover(ctx context.Context, ctrl ControllerAuth, masterName string, candidate *store.Instance) error {
	name := strings.TrimSpace(masterName)
	if name == "" {
		name = strings.TrimSpace(candidate.Name)
	}
	if name == "" {
		return fmt.Errorf("Sentinel failover 需要集群名作为 master-name")
	}
	host := strings.TrimSpace(ctrl.BaseURL)
	port := 26379
	if u, err := url.Parse(ctrl.BaseURL); err == nil && u.Host != "" {
		host = u.Hostname()
		if p := u.Port(); p != "" {
			fmt.Sscanf(p, "%d", &port)
		}
	} else if h, p, ok := strings.Cut(strings.TrimPrefix(ctrl.BaseURL, "redis://"), ":"); ok {
		host = h
		fmt.Sscanf(p, "%d", &port)
	} else {
		host = strings.TrimPrefix(ctrl.BaseURL, "redis://")
	}
	fake := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", host, port),
		Username:     strings.TrimSpace(ctrl.Username),
		Password:     ctrl.Password,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		PoolSize:     1,
	})
	defer fake.Close()
	if err := fake.Do(ctx, "SENTINEL", "FAILOVER", name).Err(); err != nil {
		return fmt.Errorf("Sentinel FAILOVER 失败: %w", err)
	}
	return nil
}
