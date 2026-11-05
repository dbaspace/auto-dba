package handler

import (
	"context"
	"fmt"
	"strings"

	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

// ensureSRDCLUser StarRocks 账号不存在时以随机密码新建，并立即登记账号管理（避免授权失败时密码丢失）。
func (h *TicketHandler) ensureSRDCLUser(ctx context.Context, admin ticketexec.Target, inst *store.Instance, it *store.Issue, grantee string, dbs, perms []string) (dclUserProvision, error) {
	var out dclUserProvision
	exists, err := ticketexec.StarRocksUserExists(ctx, admin, grantee)
	if err == nil && exists {
		return out, nil
	}
	plain := randomAccountPassword(16)
	res := ticketexec.ExecStatements(ctx, admin, []string{buildStarRocksCreateUserSQL(grantee, plain)})
	if res == nil || !res.Success {
		if execAlreadyExists(res) {
			return out, nil
		}
		if err := execOKOrExists(res); err != nil {
			return out, fmt.Errorf("CREATE USER: %w", err)
		}
	}
	out.Created = true
	out.Password = plain
	if err := materializeDCLAccounts(h.Store, h.Secret, inst, it, grantee, plain, dbs, perms); err != nil {
		return out, fmt.Errorf("写入账号管理失败: %w", err)
	}
	return out, nil
}

// recordSRDCLAccounts 授权成功后同步账号管理：已有记录合并权限；新库沿用平台托管的密码，
// 平台未托管密码时登记空密码并注明。
func recordSRDCLAccounts(st *store.DB, inst *store.Instance, it *store.Issue, grantee string, dbs, perms []string) error {
	if st == nil || inst == nil || it == nil {
		return nil
	}
	grantee = strings.TrimSpace(grantee)
	if grantee == "" {
		return nil
	}
	pwdEnc := ""
	if prev, _ := st.FindAccountByInstanceUser(inst.ID, grantee, ""); prev != nil {
		pwdEnc = strings.TrimSpace(prev.Password)
	}
	for _, dbName := range dbs {
		dbName = strings.TrimSpace(dbName)
		if dbName == "" {
			continue
		}
		if found, _ := st.FindAccount(inst.ID, grantee, dbName); found != nil {
			found.Permissions = mergePerms(found.Permissions, perms)
			found.UpdatedBy = it.Submitter
			if err := st.UpdateAccount(found); err != nil {
				return err
			}
			continue
		}
		var dbID string
		if rec, _ := st.FindDatabase(inst.ID, dbName); rec != nil {
			dbID = rec.ID
		}
		desc := strings.TrimSpace(it.Description)
		if pwdEnc == "" {
			desc = joinDesc(desc, "实例上已存在该账号，密码未托管")
		}
		acc := &store.DBAccount{
			InstanceID:  inst.ID,
			DatabaseID:  dbID,
			DBName:      dbName,
			DBType:      inst.DBType,
			IDC:         inst.IDC,
			Username:    grantee,
			Password:    pwdEnc,
			Permissions: append([]string{}, perms...),
			Description: desc,
			TicketID:    it.ID,
			CreatedBy:   it.Submitter,
			UpdatedBy:   it.Submitter,
			TenantID:    it.TenantID,
		}
		if err := st.CreateAccount(acc); err != nil {
			return err
		}
	}
	return nil
}

func mergePerms(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	seen := map[string]bool{}
	for _, p := range append(append([]string{}, a...), b...) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
