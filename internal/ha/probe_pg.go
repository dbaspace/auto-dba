package ha

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"auto-dba/internal/store"
)

func probePostgres(ctx context.Context, cluster *store.HACluster, passwordFn PasswordFn, out *Topology) (*Topology, error) {
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
		host := strings.TrimSpace(inst.Host)
		if host == "" {
			host = strings.TrimSpace(inst.DomainName)
		}
		db, err := openPostgres(host, inst.Port, inst.Username, pass)
		if err != nil {
			live.Error = err.Error()
			out.Members = append(out.Members, live)
			continue
		}
		func() {
			defer db.Close()
			if err := db.PingContext(ctx); err != nil {
				live.Error = err.Error()
				return
			}
			live.Healthy = true
			live.Host = host
			live.Port = inst.Port

			var inRecovery bool
			if err := db.QueryRowContext(ctx, `SELECT pg_is_in_recovery()`).Scan(&inRecovery); err == nil {
				if inRecovery {
					live.Role = "replica"
					var lag sql.NullFloat64
					_ = db.QueryRowContext(ctx, `
						SELECT EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))
					`).Scan(&lag)
					if lag.Valid {
						v := lag.Float64
						live.LagSeconds = &v
						live.Detail = fmt.Sprintf("replay lag ≈ %.1fs", v)
					} else {
						live.Detail = "in recovery"
					}
					return
				}
			}
			live.Role = "primary"
			n := 0
			_ = db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_replication`).Scan(&n)
			live.Detail = fmt.Sprintf("pg_stat_replication 从库数=%d", n)
			rows, err := db.QueryContext(ctx, `
				SELECT application_name, client_addr::text,
					COALESCE(EXTRACT(EPOCH FROM (now() - COALESCE(reply_time, backend_start))), 0)
				FROM pg_stat_replication`)
			if err == nil {
				defer rows.Close()
				var parts []string
				for rows.Next() {
					var app, addr string
					var lag float64
					if rows.Scan(&app, &addr, &lag) == nil {
						parts = append(parts, fmt.Sprintf("%s@%s lag=%.1fs", app, addr, lag))
					}
				}
				if len(parts) > 0 {
					live.Detail = strings.Join(parts, "; ")
				}
			}
		}()
		out.Members = append(out.Members, live)
	}
	return out, nil
}
