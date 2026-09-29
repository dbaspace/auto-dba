package ha

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"auto-dba/internal/redisquery"
	"auto-dba/internal/store"
)

func probeRedis(ctx context.Context, cluster *store.HACluster, passwordFn PasswordFn, out *Topology) (*Topology, error) {
	for i := range cluster.Members {
		m := cluster.Members[i]
		live := memberBase(m)
		inst, err := loadInstance(m.InstanceID)
		if err != nil || inst == nil {
			live.Error = "无法加载实例"
			out.Members = append(out.Members, live)
			continue
		}
		pass, err := passwordFn(inst)
		if err != nil {
			live.Error = err.Error()
			out.Members = append(out.Members, live)
			continue
		}
		info, err := redisquery.InfoSection(ctx, inst, redisquery.Options{Username: inst.Username, Password: pass}, "replication")
		if err != nil {
			live.Error = err.Error()
			out.Members = append(out.Members, live)
			continue
		}
		live.Healthy = true
		host := strings.TrimSpace(inst.Host)
		if host == "" {
			host = strings.TrimSpace(inst.DomainName)
		}
		live.Host = host
		live.Port = inst.Port
		role := strings.ToLower(info["role"])
		switch role {
		case "master":
			live.Role = "primary"
			live.Detail = fmt.Sprintf("connected_slaves=%s", info["connected_slaves"])
		case "slave":
			live.Role = "replica"
			live.Detail = fmt.Sprintf("master=%s:%s link=%s", info["master_host"], info["master_port"], info["master_link_status"])
			if o, ok := info["master_last_io_seconds_ago"]; ok {
				if f, err := strconv.ParseFloat(o, 64); err == nil {
					live.LagSeconds = &f
				}
			}
		default:
			live.Role = m.Role
			live.Detail = "role=" + role
		}
		out.Members = append(out.Members, live)
	}
	return out, nil
}
