package awr

import (
	"fmt"
	"strings"

	"auto-dba/internal/pgdiag"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type connTarget struct {
	inst *store.Instance
	host string
	port int64
}

func connectionKey(host string, port int64) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(host)), port)
}

func (s *Service) enumeratePostgresConnections() ([]connTarget, error) {
	list, err := pgdiag.ListPostgresInstances(s.Store)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]connTarget, 0, len(list))
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
		out = append(out, connTarget{inst: inst, host: host, port: int64(inst.Port)})
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

	targets, err := s.enumeratePostgresConnections()
	if err != nil {
		return connTarget{}, err
	}
	if host != "" {
		for _, t := range targets {
			if strings.EqualFold(t.host, host) && (port == 0 || t.port == port) {
				return t, nil
			}
		}
	}
	return connTarget{}, fmt.Errorf("instance not found: instance_id=%s host=%s port=%d", instanceID, host, port)
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
