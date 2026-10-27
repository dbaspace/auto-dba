package cloudsvc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/alert"
	"auto-dba/internal/cloud"
	"auto-dba/internal/flashback"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"

	_ "auto-dba/internal/cloud/tencentcvm"
	_ "auto-dba/internal/cloud/tencentmysql"
	_ "auto-dba/internal/cloud/tencentpg"
	_ "auto-dba/internal/cloud/tencentredis"
)

type Service struct {
	DB     *store.DB
	Secret string
	mu     sync.Mutex
	busy   map[string]bool
}

func New(db *store.DB, secretKey string) *Service {
	return &Service{DB: db, Secret: secretKey, busy: map[string]bool{}}
}

func (s *Service) Install() {
	if s == nil || s.DB == nil {
		return
	}
	flashback.ResolveInstanceCred = s.ResolveCred
	cloud.DefaultLimiter().SetQPS(func() int {
		v, _ := s.DB.GetGlobalArg(store.ArgCloudAPIQPS)
		n := atoi(v)
		if n <= 0 {
			return 5
		}
		return n
	})
	if err := s.migrateLegacy(); err != nil {
		log.Printf("cloud: migrate legacy ak/sk: %v", err)
	}
}

func (s *Service) ResolveCred(ctx context.Context, instanceID, username string) (string, string, string, error) {
	it, err := s.DB.GetInstance(strings.TrimSpace(instanceID))
	if err != nil {
		return "", "", "", err
	}
	if it == nil || strings.TrimSpace(it.CloudAccountID) == "" {
		return "", "", "", cloud.ErrNoAccount
	}
	user, err := s.DB.GetUserByUsername(strings.TrimSpace(username))
	if err != nil {
		return "", "", "", err
	}
	if user == nil {
		return "", "", "", cloud.ErrNotBound
	}
	ok, err := s.DB.UserBoundToCloudAccount(it.CloudAccountID, user.ID)
	if err != nil {
		return "", "", "", err
	}
	if !ok {
		return "", "", "", cloud.ErrNotBound
	}
	id, key, err := s.key(it.CloudAccountID, cloud.RoleReadonly)
	if err != nil {
		return "", "", "", err
	}
	return id, key, it.CloudAccountID, nil
}

func (s *Service) key(accountID, role string) (string, string, error) {
	k, err := s.DB.CloudAccountKey(accountID, role)
	if err != nil {
		return "", "", err
	}
	if k == nil || k.SecretID == "" || k.SecretKeyEnc == "" {
		return "", "", cloud.ErrNoKey
	}
	plain, err := secret.Decrypt(s.Secret, k.SecretKeyEnc)
	if err != nil {
		return "", "", err
	}
	return k.SecretID, plain, nil
}

func (s *Service) Cred(account *store.CloudAccount, role, region string) (cloud.Cred, error) {
	id, key, err := s.key(account.ID, role)
	if err != nil {
		return cloud.Cred{}, err
	}
	if region == "" && len(account.Regions) > 0 {
		region = account.Regions[0]
	}
	return cloud.Cred{AccountID: account.ID, Vendor: account.Vendor, Region: region, SecretID: id, SecretKey: key, Role: role}, nil
}

func (s *Service) tryAccount(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[id] {
		return false
	}
	s.busy[id] = true
	return true
}

func (s *Service) doneAccount(id string) {
	s.mu.Lock()
	delete(s.busy, id)
	s.mu.Unlock()
}

// SyncResult 汇总同步结果：无只读密钥的账号会被跳过，不阻断其它账号与定时任务。
type SyncResult struct {
	Synced  int      `json:"synced"`
	Skipped []string `json:"skipped,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

func (s *Service) SyncAll(ctx context.Context) error {
	_, err := s.SyncAllEngines(ctx, "")
	return err
}

func (s *Service) SyncAllEngines(ctx context.Context, onlyEngine string) (*SyncResult, error) {
	accounts, err := s.DB.ListCloudAccounts("", true)
	if err != nil {
		return nil, err
	}
	engines := cloud.Engines()
	if onlyEngine != "" {
		engines = []string{onlyEngine}
	}
	res := &SyncResult{}
	for i := range accounts {
		acc := &accounts[i]
		supported := false
		for _, eng := range engines {
			if cloud.Supported(acc.Vendor, eng) {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}
		// 无只读密钥：整账号跳过（不按引擎重复报错），健康态仍按引擎记 no_key
		if _, _, kerr := s.key(acc.ID, cloud.RoleReadonly); errors.Is(kerr, cloud.ErrNoKey) {
			res.Skipped = append(res.Skipped, acc.Name+" 缺少只读密钥")
			for _, eng := range engines {
				if cloud.Supported(acc.Vendor, eng) {
					s.noteSync(acc, eng, "inventory", "", cloud.ErrNoKey)
				}
			}
			continue
		}
		for _, eng := range engines {
			if !cloud.Supported(acc.Vendor, eng) {
				continue
			}
			err := s.SyncAccountEngine(ctx, acc, eng)
			if err == nil {
				res.Synced++
				continue
			}
			if errors.Is(err, cloud.ErrNoKey) {
				// Cred 竞态缺失时仍跳过，不阻断其它账号
				res.Skipped = append(res.Skipped, acc.Name+" 缺少只读密钥")
				continue
			}
			res.Errors = append(res.Errors, acc.Name+"/"+eng+": "+err.Error())
		}
	}
	return res, finalizeSyncResult(res)
}

func (s *Service) SyncAccount(ctx context.Context, account *store.CloudAccount) error {
	return s.SyncAccountEngine(ctx, account, cloud.EnginePostgres)
}

func (s *Service) SyncAccountEngine(ctx context.Context, account *store.CloudAccount, engine string) error {
	if account == nil {
		return cloud.ErrNoAccount
	}
	if engine == "" {
		engine = cloud.EnginePostgres
	}
	lockKey := account.ID + ":" + engine
	if !s.tryAccount(lockKey) {
		return fmt.Errorf("排队中")
	}
	defer s.doneAccount(lockKey)
	reader, err := cloud.GetReader(account.Vendor, engine)
	if err != nil {
		return err
	}
	cred, err := s.Cred(account, cloud.RoleReadonly, "")
	if err != nil {
		s.noteSync(account, engine, "inventory", "", err)
		return err
	}
	var all []cloud.Instance
	regions := account.Regions
	if len(regions) == 0 {
		regions = []string{cred.Region}
	}
	for _, region := range regions {
		cred.Region = region
		var page []cloud.Instance
		err = cloud.RetryRead(ctx, nil, func() error {
			var e error
			page, e = reader.ListInstances(ctx, cred)
			return e
		})
		if err != nil {
			s.noteSync(account, engine, "inventory", "", err)
			return err
		}
		all = append(all, page...)
	}
	seen := make([]string, 0, len(all))
	for _, inst := range all {
		seen = append(seen, inst.CloudID)
		prev, _ := s.DB.GetCloudResourceByCloudID(account.Vendor, engine, inst.CloudID)
		row := &store.CloudInstance{
			TenantID: account.TenantID, Vendor: account.Vendor, Engine: engine, AccountID: account.ID, CloudID: inst.CloudID,
			Name: inst.Name, Region: inst.Region, Status: inst.Status, CPU: inst.CPU, MemoryMB: inst.MemoryMB,
			DiskGB: inst.DiskGB, DiskUsedGB: inst.DiskUsed, Version: inst.Version, Zone: inst.Zone,
			PayType: inst.PayType, ExpireAt: inst.ExpireAt, Extras: inst.Extras,
		}
		s.bindPlatform(row, prev)
		if err := s.DB.UpsertCloudInstance(row); err != nil {
			return err
		}
		if prev != nil && prev.Zone != "" && inst.Zone != "" && prev.Zone != inst.Zone {
			_ = s.DB.AddCloudEvent(account.TenantID, account.Vendor, account.ID, inst.CloudID, "switchover", "主可用区从 "+prev.Zone+" 变为 "+inst.Zone, time.Now().UTC().Format(time.RFC3339))
		}
		s.pullEvents(ctx, reader, cred, account, inst.CloudID)
	}
	if err := s.DB.MarkMissingCloudResources(account.ID, engine, seen); err != nil {
		return err
	}
	s.noteSync(account, engine, "inventory", "", nil)
	return nil
}

// AssignAccount 人工指定云资源归属账号：合并同云实例在多账号下的重复行，并回写平台实例绑定。
func (s *Service) AssignAccount(resourceID, accountID, username string) (*store.CloudInstance, error) {
	row, err := s.DB.GetCloudResource(resourceID)
	if err != nil || row == nil {
		return nil, fmt.Errorf("云资源不存在")
	}
	acc, err := s.DB.GetCloudAccount(accountID)
	if err != nil || acc == nil {
		return nil, cloud.ErrNoAccount
	}
	if acc.Vendor != row.Vendor {
		return nil, fmt.Errorf("云账号厂商与资源不一致")
	}
	if row.TenantID != "" && acc.TenantID != "" && row.TenantID != acc.TenantID {
		return nil, fmt.Errorf("云账号与资源租户不一致")
	}
	user, _ := s.DB.GetUserByUsername(username)
	if user != nil {
		ok, _ := s.DB.UserBoundToCloudAccount(acc.ID, user.ID)
		admin, _ := s.DB.UserIsAdmin(user.ID)
		if !ok && !admin {
			keysOK, _ := s.DB.UserHasURN(user.ID, store.URNCloudKeys)
			if !keysOK {
				return nil, cloud.ErrNotBound
			}
		}
	}
	siblings, err := s.DB.ListCloudResourcesByCloudID(row.Vendor, row.Engine, row.CloudID)
	if err != nil {
		return nil, err
	}
	keepID := row.ID
	instanceID := row.InstanceID
	for i := range siblings {
		if siblings[i].AccountID == acc.ID {
			keepID = siblings[i].ID
			if instanceID == "" {
				instanceID = siblings[i].InstanceID
			}
			break
		}
	}
	for _, sib := range siblings {
		if sib.ID == keepID {
			continue
		}
		if sib.InstanceID != "" && instanceID == "" {
			instanceID = sib.InstanceID
		}
		_ = s.DB.DeleteCloudResource(sib.ID)
	}
	keep, err := s.DB.GetCloudResource(keepID)
	if err != nil || keep == nil {
		return nil, fmt.Errorf("云资源不存在")
	}
	keep.AccountID = acc.ID
	if keep.TenantID == "" {
		keep.TenantID = acc.TenantID
	}
	if instanceID != "" {
		keep.InstanceID = instanceID
	}
	s.bindPlatform(keep, keep)
	matches, _ := s.DB.InstancesByCloudID(keep.TenantID, keep.Vendor, keep.CloudID)
	if len(matches) == 1 {
		_ = s.DB.SetInstanceCloudAccount(matches[0].ID, keep.AccountID)
		keep.InstanceID = matches[0].ID
		keep.Reconcile = cloud.ReconcileLinked
	} else if keep.Reconcile == cloud.ReconcileConflict || keep.Reconcile == "" {
		if keep.InstanceID != "" {
			_ = s.DB.SetInstanceCloudAccount(keep.InstanceID, keep.AccountID)
			keep.Reconcile = cloud.ReconcileLinked
		} else {
			keep.Reconcile = cloud.ReconcileCloudOnly
		}
	}
	if err := s.DB.UpdateCloudResourceOwnership(keep.ID, keep.AccountID, keep.InstanceID, keep.Reconcile); err != nil {
		return nil, err
	}
	out, err := s.DB.GetCloudResource(keep.ID)
	if err != nil {
		return nil, err
	}
	if out != nil {
		if a, _ := s.DB.GetCloudAccount(out.AccountID); a != nil {
			out.AccountName = a.Name
		}
		if out.InstanceID != "" {
			if it, _ := s.DB.GetInstance(out.InstanceID); it != nil {
				out.InstanceName = it.Name
			}
		}
	}
	return out, nil
}

func (s *Service) bindPlatform(row *store.CloudInstance, prev *store.CloudInstance) {
	matches, err := s.DB.InstancesByCloudID(row.TenantID, row.Vendor, row.CloudID)
	if err != nil {
		row.Reconcile = cloud.ReconcileCloudOnly
		return
	}
	n, _ := s.DB.CountCloudResourcesByCloudID(row.Vendor, row.Engine, row.CloudID)
	if prev == nil {
		n++
	}
	if n > 1 {
		row.Reconcile = cloud.ReconcileConflict
		return
	}
	switch cloud.DecideBind(accountOf(matches), row.AccountID, len(matches)) {
	case cloud.BindFill:
		_ = s.DB.SetInstanceCloudAccount(matches[0].ID, row.AccountID)
		row.InstanceID = matches[0].ID
		row.Reconcile = cloud.ReconcileLinked
	case cloud.BindKeep:
		row.InstanceID = matches[0].ID
		row.Reconcile = cloud.ReconcileLinked
	case cloud.BindConflict:
		row.Reconcile = cloud.ReconcileConflict
		if len(matches) == 1 {
			row.InstanceID = matches[0].ID
		}
	default:
		if len(matches) == 0 {
			row.Reconcile = cloud.ReconcileCloudOnly
		} else {
			row.Reconcile = cloud.ReconcileCloudOnly
		}
	}
	if prev != nil && prev.Status != "" && prev.Status != row.Status && row.Reconcile == cloud.ReconcileLinked {
		row.Reconcile = cloud.ReconcileChanged
	}
}

func accountOf(list []store.Instance) string {
	if len(list) != 1 {
		return ""
	}
	return list[0].CloudAccountID
}

func (s *Service) pullEvents(ctx context.Context, reader cloud.Reader, cred cloud.Cred, account *store.CloudAccount, cloudID string) {
	var ev []cloud.Event
	err := cloud.RetryRead(ctx, nil, func() error {
		var e error
		ev, e = reader.ListEvents(ctx, cred, cloudID, time.Now().Add(-30*time.Minute), time.Now())
		return e
	})
	if err != nil {
		return
	}
	for _, e := range ev {
		_ = s.DB.AddCloudEvent(account.TenantID, account.Vendor, account.ID, cloudID, e.Type, e.Message, e.At.UTC().Format(time.RFC3339))
	}
}

func (s *Service) noteSync(account *store.CloudAccount, engine, dataType, cloudID string, err error) {
	if engine == "" {
		engine = cloud.EnginePostgres
	}
	if dataType == "" {
		dataType = "inventory"
	}
	cur := &store.CloudCursor{
		TenantID: account.TenantID, Vendor: account.Vendor, Engine: engine, AccountID: account.ID,
		CloudID: cloudID, DataType: dataType, Status: "ok",
	}
	if err != nil {
		if errors.Is(err, cloud.ErrNoKey) {
			cur.Status = "no_key"
			cur.Error = "缺少只读密钥，已跳过"
			cur.FailCount = 0
		} else {
			cur.Status = "error"
			cur.Error = err.Error()
			prev, _ := s.DB.GetCloudCursor(account.Vendor, engine, account.ID, cloudID, dataType)
			if prev != nil {
				cur.FailCount = prev.FailCount + 1
				cur.CursorAt = prev.CursorAt
			} else {
				cur.FailCount = 1
			}
			if cur.FailCount >= 3 {
				_ = s.DB.UpsertAlert(&store.Alert{
					ID: "cloud-sync-" + account.ID + "-" + engine, Fingerprint: "cloud-sync-" + account.ID + "-" + engine,
					Severity: store.AlertSeverityP2, Title: "云资源同步连续失败", Service: "cloud",
					Status: store.AlertStatusFiring, Handle: store.AlertHandleUnclaimed,
					Labels:      map[string]string{"account_id": account.ID, "engine": engine},
					Annotations: map[string]string{"error": err.Error()},
					StartedAt:   time.Now().UTC().Format(time.RFC3339),
				})
			}
		}
	} else {
		cur.CursorAt = time.Now().UTC().Format(time.RFC3339)
		_ = s.DB.UpsertAlert(&store.Alert{
			ID: "cloud-sync-" + account.ID + "-" + engine, Fingerprint: "cloud-sync-" + account.ID + "-" + engine,
			Severity: store.AlertSeverityP2, Title: "云资源同步连续失败", Service: "cloud",
			Status: store.AlertStatusResolved, Handle: store.AlertHandleResolved,
			Labels:     map[string]string{"account_id": account.ID, "engine": engine},
			ResolvedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	_ = s.DB.SaveCloudCursor(cur)
}

func (s *Service) SlowlogAll(ctx context.Context, onlyCloudID string) error {
	accounts, err := s.DB.ListCloudAccounts("", true)
	if err != nil {
		return err
	}
	var first error
	for i := range accounts {
		if !cloud.Supported(accounts[i].Vendor, cloud.EnginePostgres) {
			continue
		}
		if !s.tryAccount(accounts[i].ID + ":slow") {
			if first == nil {
				first = fmt.Errorf("排队中")
			}
			continue
		}
		err := s.slowAccount(ctx, &accounts[i], onlyCloudID)
		s.doneAccount(accounts[i].ID + ":slow")
		if err != nil && !errors.Is(err, cloud.ErrNoKey) && first == nil {
			first = err
		}
	}
	s.purgeSlow()
	return first
}

func (s *Service) slowAccount(ctx context.Context, account *store.CloudAccount, only string) error {
	reader, err := cloud.GetReader(account.Vendor, cloud.EnginePostgres)
	if err != nil {
		return err
	}
	cred, err := s.Cred(account, cloud.RoleReadonly, "")
	if err != nil {
		return err
	}
	list, err := s.DB.ListCloudResources(account.TenantID, cloud.EnginePostgres, "")
	if err != nil {
		return err
	}
	now := time.Now()
	for _, inst := range list {
		if inst.AccountID != account.ID {
			continue
		}
		if only != "" && inst.CloudID != only && inst.InstanceID != only {
			continue
		}
		if strings.TrimSpace(inst.InstanceID) != "" {
			pi, _ := s.DB.GetInstance(inst.InstanceID)
			if pi != nil && strings.TrimSpace(pi.CloudAccountID) == "" {
				continue
			}
		}
		if inst.Region != "" {
			cred.Region = inst.Region
		}
		if err := s.slowOne(ctx, reader, cred, account, inst, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) slowOne(ctx context.Context, reader cloud.Reader, cred cloud.Cred, account *store.CloudAccount, inst store.CloudInstance, now time.Time) error {
	cur, err := s.DB.GetCloudCursor(account.Vendor, "postgres", account.ID, inst.CloudID, "slow_log")
	if err != nil {
		return err
	}
	var last time.Time
	if cur != nil && cur.CursorAt != "" {
		last, _ = time.Parse(time.RFC3339, cur.CursorAt)
	}
	win := cloud.NextWindow(last, now, 5*time.Minute, time.Minute)
	parts := cloud.SplitWindow(win, 24*time.Hour)
	cursor := last
	total := 0
	for _, seg := range parts {
		n, err := s.pullSlow(ctx, reader, cred, account, inst, seg)
		row := &store.CloudCursor{
			TenantID: account.TenantID, Vendor: account.Vendor, Engine: "postgres", AccountID: account.ID,
			CloudID: inst.CloudID, DataType: "slow_log", LastCount: n,
		}
		if err != nil {
			row.CursorAt = cursor.UTC().Format(time.RFC3339)
			if cursor.IsZero() {
				row.CursorAt = ""
			}
			row.Status = "error"
			row.Error = err.Error()
			if cur != nil {
				row.FailCount = cur.FailCount + 1
			}
			_ = s.DB.SaveCloudCursor(row)
			return err
		}
		total += n
		cursor = cloud.AdvanceCursor(cursor, seg, true)
		row.CursorAt = cursor.UTC().Format(time.RFC3339)
		row.Status = "ok"
		row.LastCount = total
		if err := s.DB.SaveCloudCursor(row); err != nil {
			return err
		}
		cur = row
	}
	return nil
}

func (s *Service) pullSlow(ctx context.Context, reader cloud.Reader, cred cloud.Cred, account *store.CloudAccount, inst store.CloudInstance, seg cloud.Window) (int, error) {
	offset := 0
	n := 0
	for {
		var page []cloud.SlowLog
		var total int
		err := cloud.RetryRead(ctx, nil, func() error {
			var e error
			page, total, e = reader.ListSlowLogs(ctx, cred, inst.CloudID, seg.From, seg.To, offset, 100)
			return e
		})
		if err != nil {
			return n, err
		}
		for _, row := range page {
			if row.Time.Before(seg.From) || row.Time.After(seg.To) {
				continue
			}
			fp := cloud.Fingerprint(row.SQL)
			if err := s.DB.InsertCloudSlowLog(&store.CloudSlowLog{
				TenantID: account.TenantID, Vendor: account.Vendor, Engine: "postgres", AccountID: account.ID,
				CloudID: inst.CloudID, LogTime: row.Time.UTC().Format(time.RFC3339Nano), DBName: row.DBName,
				DBUser: row.User, Client: row.Client, Duration: row.DurationMS, SQL: row.SQL, Fingerprint: fp,
			}); err != nil {
				return n, err
			}
			n++
		}
		offset += len(page)
		if len(page) == 0 || offset >= total {
			break
		}
	}
	return n, nil
}

func (s *Service) purgeSlow() {
	raw, _ := s.DB.GetGlobalArg(store.ArgCloudSlowlogRetention)
	days := atoi(raw)
	if days <= 0 {
		days = 15
	}
	before := time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
	_ = s.DB.PurgeCloudSlowLogs(before)
}

func (s *Service) QueueSlow(cloudID string) error {
	list, err := s.DB.ListCloudResources("", cloud.EnginePostgres, "")
	if err != nil {
		return err
	}
	for _, inst := range list {
		if cloudID != "" && inst.CloudID != cloudID && inst.InstanceID != cloudID {
			continue
		}
		cur, _ := s.DB.GetCloudCursor(inst.Vendor, "postgres", inst.AccountID, inst.CloudID, "slow_log")
		row := &store.CloudCursor{TenantID: inst.TenantID, Vendor: inst.Vendor, Engine: "postgres", AccountID: inst.AccountID, CloudID: inst.CloudID, DataType: "slow_log", Status: "queued"}
		if cur != nil {
			row.CursorAt = cur.CursorAt
			row.FailCount = cur.FailCount
		}
		if err := s.DB.SaveCloudCursor(row); err != nil {
			return err
		}
	}
	return nil
}

func atoi(s string) int {
	n := 0
	for _, c := range strings.TrimSpace(s) {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func (s *Service) migrateLegacy() error {
	type pair struct{ vendor, idEnv, keyEnv, idArg, keyArg, region string }
	pairs := []pair{
		{"tencent", "FLASHBACK_TENCENT_SECRET_ID", "FLASHBACK_TENCENT_SECRET_KEY", store.ArgFlashbackTencentSecretID, store.ArgFlashbackTencentSecretKey, store.ArgFlashbackTencentRegion},
		{"aliyun", "FLASHBACK_ALIYUN_ACCESS_KEY_ID", "FLASHBACK_ALIYUN_ACCESS_KEY_SECRET", store.ArgFlashbackAliyunAccessKeyID, store.ArgFlashbackAliyunAccessKeySecret, ""},
		{"huawei", "FLASHBACK_HUAWEI_ACCESS_KEY_ID", "FLASHBACK_HUAWEI_SECRET_ACCESS_KEY", store.ArgFlashbackHuaweiAccessKeyID, store.ArgFlashbackHuaweiSecretAccessKey, ""},
		{"aws", "FLASHBACK_AWS_ACCESS_KEY_ID", "FLASHBACK_AWS_SECRET_ACCESS_KEY", store.ArgFlashbackAWSAccessKeyID, store.ArgFlashbackAWSSecretAccessKey, ""},
	}
	admin, _ := s.DB.GetUserByUsername("admin")
	for _, p := range pairs {
		id := strings.TrimSpace(os.Getenv(p.idEnv))
		key := strings.TrimSpace(os.Getenv(p.keyEnv))
		if id == "" {
			id, _ = s.DB.GetGlobalArg(p.idArg)
			id = strings.TrimSpace(id)
		}
		if key == "" {
			key, _ = s.DB.GetGlobalArg(p.keyArg)
			key = strings.TrimSpace(key)
		}
		if id == "" || key == "" {
			continue
		}
		name := "迁移-" + p.vendor
		acc, err := s.DB.FindCloudAccountByName(store.DefaultTenantID, p.vendor, name)
		if err != nil {
			return err
		}
		if acc == nil {
			region, _ := s.DB.GetGlobalArg(p.region)
			var regions []string
			if strings.TrimSpace(region) != "" {
				regions = []string{strings.TrimSpace(region)}
			}
			acc = &store.CloudAccount{TenantID: store.DefaultTenantID, Vendor: p.vendor, Name: name, Regions: regions, Enabled: true}
			if err := s.DB.CreateCloudAccount(acc); err != nil {
				return err
			}
		}
		enc, err := secret.Encrypt(s.Secret, key)
		if err != nil {
			return err
		}
		if err := s.DB.SaveCloudAccountKey(acc.ID, cloud.RoleReadonly, id, enc); err != nil {
			return err
		}
		if admin != nil {
			_ = s.DB.SetCloudAccountUsers(acc.ID, []string{admin.ID})
		}
		missing, err := s.DB.ListInstancesMissingCloudAccount(p.vendor)
		if err != nil {
			return err
		}
		for _, it := range missing {
			if it.TenantID != "" && it.TenantID != acc.TenantID {
				continue
			}
			_ = s.DB.SetInstanceCloudAccount(it.ID, acc.ID)
		}
	}
	return nil
}

// 让 alert 包的实例标签解析留在调用方，避免 cloudsvc 复制规则。
var labelInstance = alert.LabelInstanceID
