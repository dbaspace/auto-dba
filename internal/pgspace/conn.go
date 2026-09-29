package pgspace

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/pgdiag"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

const (
	pgSpaceConnMaxLifetime = 30 * time.Second
	pgSpacePingTimeout     = 15 * time.Second
)

type connTarget struct {
	inst *store.Instance
	host string
	port int64
}

type physicalInstance struct {
	target connTarget
}

var pgSpaceRealtimeLocks sync.Map

func connectionKey(host string, port int64) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(host)), port)
}

func pgSpaceLockFor(key string) *sync.Mutex {
	m, _ := pgSpaceRealtimeLocks.LoadOrStore(key, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (s *Service) decryptPassword(inst *store.Instance) (string, error) {
	if inst == nil {
		return "", fmt.Errorf("实例不存在")
	}
	if strings.TrimSpace(inst.Password) == "" {
		return "", fmt.Errorf("credential empty")
	}
	return secret.Decrypt(s.Secret, inst.Password)
}

func buildPGDSN(host string, port int64, user, pass, dbname string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, pass),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + dbname,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("connect_timeout", "8")
	u.RawQuery = q.Encode()
	return u.String()
}

func openPgSpaceConn(ctx context.Context, host string, port int64, user, pass, dbname string, stmtMs int) (*sql.DB, error) {
	conn, err := sql.Open("pgx", buildPGDSN(host, port, user, pass, dbname))
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(0)
	conn.SetConnMaxLifetime(pgSpaceConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, pgSpacePingTimeout)
	defer cancel()
	if err := conn.PingContext(pingCtx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	applyPgSpaceGuards(ctx, conn, stmtMs)
	return conn, nil
}

func applyPgSpaceGuards(ctx context.Context, conn *sql.DB, stmtMs int) {
	if stmtMs <= 0 {
		stmtMs = pgSpaceDefaultStatementMs
	}
	stmts := []string{
		fmt.Sprintf("SET application_name = '%s'", pgSpaceAppName),
		"SET default_transaction_read_only = on",
		fmt.Sprintf("SET statement_timeout = %d", stmtMs),
		"SET lock_timeout = 2000",
		"SET idle_in_transaction_session_timeout = 5000",
	}
	for _, s := range stmts {
		_, _ = conn.ExecContext(ctx, s)
	}
}

// enumeratePhysicalPostgresInstances 列出本地 PostgreSQL 实例，按 host:port 去重。
func (s *Service) enumeratePhysicalPostgresInstances() ([]physicalInstance, error) {
	list, err := pgdiag.ListPostgresInstances(s.Store)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]physicalInstance, 0, len(list))
	for i := range list {
		inst := &list[i]
		host := pgdiag.InstanceHost(inst)
		if host == "" || inst.Port <= 0 {
			continue
		}
		key := connectionKey(host, int64(inst.Port))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, physicalInstance{
			target: connTarget{inst: inst, host: host, port: int64(inst.Port)},
		})
	}
	return out, nil
}

func (s *Service) resolveTarget(instanceID, host string, port int64) (connTarget, error) {
	instanceID = strings.TrimSpace(instanceID)
	host = strings.TrimSpace(host)

	if instanceID != "" {
		inst, err := s.Store.GetInstance(instanceID)
		if err != nil {
			return connTarget{}, err
		}
		if inst == nil {
			return connTarget{}, fmt.Errorf("instance not found: %s", instanceID)
		}
		if !pgdiag.IsPostgres(inst.DBType) {
			return connTarget{}, fmt.Errorf("仅支持 PostgreSQL 实例")
		}
		h := pgdiag.InstanceHost(inst)
		return connTarget{inst: inst, host: h, port: int64(inst.Port)}, nil
	}

	physicals, err := s.enumeratePhysicalPostgresInstances()
	if err != nil {
		return connTarget{}, err
	}
	if host != "" {
		for _, p := range physicals {
			if strings.EqualFold(p.target.host, host) && (port == 0 || p.target.port == port) {
				return p.target, nil
			}
		}
	}
	return connTarget{}, fmt.Errorf("instance not found: instance_id=%s host=%s port=%d", instanceID, host, port)
}
