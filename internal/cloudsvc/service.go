package cloudsvc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"auto-dba/internal/alert"
	"auto-dba/internal/cloud"
	"auto-dba/internal/flashback"
	"auto-dba/internal/secret"
	"auto-dba/internal/slowbus"
	"auto-dba/internal/store"

	_ "auto-dba/internal/cloud/tencentckafka"
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

	slowMu     sync.Mutex
	stopping   bool
	pulling    int
	pullCancel context.CancelFunc
	busOpen    sync.Mutex
	bus        *slowbus.Bus
	busKey     string
	kafkaDown  time.Time
	drainSince time.Time
	lastPurge  time.Time
	rateHits   atomic.Int64

	// MergeDuplicateHosts 在云主机同步前合并连接地址相同的主机，由 server 注入。
	MergeDuplicateHosts func()
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
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range accounts {
		acc := accounts[i]
		wg.Add(1)
		go func(acc store.CloudAccount) {
			defer wg.Done()
			local := &SyncResult{}
			s.syncScheduled(ctx, &acc, engines, local)
			mu.Lock()
			res.Synced += local.Synced
			res.Skipped = append(res.Skipped, local.Skipped...)
			res.Errors = append(res.Errors, local.Errors...)
			mu.Unlock()
		}(acc)
	}
	wg.Wait()
	return res, finalizeSyncResult(res)
}

// syncScheduled 按勾选的引擎同步一个账号。未勾选的引擎写入 skipped，避免健康状态停在上一次成功。
func (s *Service) syncScheduled(ctx context.Context, acc *store.CloudAccount, want []string, res *SyncResult) {
	missed := unscheduledEngines(acc, want)
	for _, eng := range missed {
		s.noteSync(acc, eng, "inventory", "", errNotScheduled)
	}
	picked := enginesForAccount(acc, want, true)
	if len(picked) == 0 {
		if len(acc.SyncEngines) == 0 {
			res.Skipped = append(res.Skipped, acc.Name+" 未勾选定时同步资源")
		}
		return
	}
	s.syncPicked(ctx, acc, picked, res)
}

// SyncAccounts 手动同步指定账号。不受 sync_engines 限制，只跑传入的引擎。
func (s *Service) SyncAccounts(ctx context.Context, engine string, accountIDs []string) (*SyncResult, error) {
	if strings.TrimSpace(engine) == "" {
		engine = cloud.EnginePostgres
	}
	res := &SyncResult{}
	seen := map[string]bool{}
	for _, id := range accountIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		acc, err := s.DB.GetCloudAccount(id)
		if err != nil {
			return res, err
		}
		if acc == nil {
			res.Skipped = append(res.Skipped, id+" 不存在")
			continue
		}
		if !acc.Enabled {
			res.Skipped = append(res.Skipped, acc.Name+" 已停用")
			continue
		}
		if !cloud.Supported(acc.Vendor, engine) {
			res.Skipped = append(res.Skipped, acc.Name+" 不支持该资源")
			continue
		}
		s.syncPicked(ctx, acc, []string{engine}, res)
	}
	return res, finalizeSyncResult(res)
}

func enginesForAccount(acc *store.CloudAccount, want []string, honorSchedule bool) []string {
	allow := map[string]bool{}
	if honorSchedule {
		if len(acc.SyncEngines) == 0 {
			return nil
		}
		for _, e := range acc.SyncEngines {
			allow[e] = true
		}
	}
	var out []string
	for _, eng := range want {
		if honorSchedule && !allow[eng] {
			continue
		}
		if cloud.Supported(acc.Vendor, eng) {
			out = append(out, eng)
		}
	}
	return out
}

// unscheduledEngines 是厂商支持、但账号没有勾选定时同步的引擎。
func unscheduledEngines(acc *store.CloudAccount, want []string) []string {
	if acc == nil {
		return nil
	}
	allow := map[string]bool{}
	for _, e := range acc.SyncEngines {
		allow[strings.TrimSpace(e)] = true
	}
	var out []string
	for _, eng := range want {
		if !cloud.Supported(acc.Vendor, eng) || allow[eng] {
			continue
		}
		out = append(out, eng)
	}
	return out
}

func (s *Service) syncPicked(ctx context.Context, acc *store.CloudAccount, engines []string, res *SyncResult) {
	if _, _, kerr := s.key(acc.ID, cloud.RoleReadonly); errors.Is(kerr, cloud.ErrNoKey) {
		res.Skipped = append(res.Skipped, acc.Name+" 缺少只读密钥")
		for _, eng := range engines {
			s.noteSync(acc, eng, "inventory", "", cloud.ErrNoKey)
		}
		return
	}
	for _, eng := range engines {
		partial, err := s.SyncAccountEngine(ctx, acc, eng)
		if err == nil {
			res.Synced++
			res.Errors = append(res.Errors, partial...)
			continue
		}
		if errors.Is(err, cloud.ErrNoKey) {
			res.Skipped = append(res.Skipped, acc.Name+" 缺少只读密钥")
			continue
		}
		if errors.Is(err, errSyncBusy) {
			res.Skipped = append(res.Skipped, acc.Name+"/"+eng+" 上一轮仍在执行")
			continue
		}
		if len(partial) > 0 {
			res.Errors = append(res.Errors, partial...)
			continue
		}
		res.Errors = append(res.Errors, acc.Name+"/"+eng+": "+err.Error())
	}
}

func (s *Service) SyncAccount(ctx context.Context, account *store.CloudAccount) error {
	_, err := s.SyncAccountEngine(ctx, account, cloud.EnginePostgres)
	return err
}

func (s *Service) SyncAccountEngine(ctx context.Context, account *store.CloudAccount, engine string) ([]string, error) {
	if account == nil {
		return nil, cloud.ErrNoAccount
	}
	if engine == "" {
		engine = cloud.EnginePostgres
	}
	lockKey := account.ID + ":" + engine
	if !s.tryAccount(lockKey) {
		s.noteSync(account, engine, "inventory", "", errSyncBusy)
		return nil, errSyncBusy
	}
	defer s.doneAccount(lockKey)
	ctx, cancel := context.WithTimeout(ctx, engineSyncTimeout)
	defer cancel()
	reader, err := cloud.GetReader(account.Vendor, engine)
	if err != nil {
		return nil, err
	}
	base, err := s.Cred(account, cloud.RoleReadonly, "")
	if err != nil {
		s.noteSync(account, engine, "inventory", "", err)
		return nil, err
	}
	regions := uniqueRegions(account.Regions)
	if len(regions) == 0 {
		err = fmt.Errorf("未配置云地域")
		s.noteSync(account, engine, "inventory", "", err)
		return nil, err
	}
	pages := listRegionsParallel(ctx, regions, func(ctx context.Context, region string) ([]cloud.Instance, error) {
		cred := base
		cred.Region = region
		var page []cloud.Instance
		err := cloud.RetryRead(ctx, nil, func() error {
			var e error
			page, e = reader.ListInstances(ctx, cred)
			return e
		})
		if err != nil {
			return nil, err
		}
		for i := range page {
			page[i].Region = region
		}
		return page, nil
	})
	var all []cloud.Instance
	var okRegions []string
	var regionErrs []string
	for _, page := range pages {
		if page.Err != nil {
			regionErrs = append(regionErrs, account.Name+"/"+engine+"/"+page.Region+": "+page.Err.Error())
			continue
		}
		okRegions = append(okRegions, page.Region)
		all = append(all, page.Items...)
	}
	if len(okRegions) == 0 {
		err = fmt.Errorf("%s", strings.Join(regionErrs, "; "))
		s.noteSync(account, engine, "inventory", "", err)
		return regionErrs, err
	}
	onlyHosts := engine == cloud.EngineHost
	if onlyHosts && s.MergeDuplicateHosts != nil {
		s.MergeDuplicateHosts()
	}
	plats, _ := s.instancesForInventoryMatch(account.TenantID)
	hostKeys := registeredHostKeys(plats)
	seen := make([]string, 0, len(all))
	for _, inst := range all {
		if onlyHosts {
			ip := matchRegisteredHostIP(inst, hostKeys)
			if ip == "" {
				continue
			}
			inst.Host = ip
		}
		if enr, ok := reader.(cloud.InstanceEnricher); ok {
			evCred := base
			evCred.Region = inst.Region
			if err := enr.Enrich(ctx, evCred, &inst); err != nil {
				regionErrs = append(regionErrs, account.Name+"/"+engine+"/"+inst.Region+"/"+inst.CloudID+": "+err.Error())
			}
		}
		seen = append(seen, inst.CloudID)
		prev, _ := s.DB.GetCloudResourceByCloudID(account.Vendor, engine, inst.CloudID)
		row := &store.CloudInstance{
			TenantID: account.TenantID, Vendor: account.Vendor, Engine: engine, AccountID: account.ID, CloudID: inst.CloudID,
			Name: inst.Name, Region: inst.Region, Status: inst.Status, CPU: inst.CPU, MemoryMB: inst.MemoryMB,
			DiskGB: inst.DiskGB, DiskUsedGB: inst.DiskUsed, Version: inst.Version, Zone: inst.Zone,
			PayType: inst.PayType, ExpireAt: inst.ExpireAt, Extras: inst.Extras,
		}
		s.bindPlatform(row, prev, inst.Host, plats)
		if row.Reconcile == cloud.ReconcileConflict && strings.TrimSpace(row.InstanceID) != "" {
			if err := s.DB.SetInstanceCloudAccount(row.InstanceID, row.AccountID); err != nil {
				return regionErrs, err
			}
			row.Reconcile = cloud.ReconcileLinked
		}
		if row.InstanceID != "" && (row.Reconcile == cloud.ReconcileLinked || row.Reconcile == cloud.ReconcileChanged) {
			s.fillInstanceCloudBlanks(row.InstanceID, row.Vendor, row.CloudID, row.Region)
		}
		if err := s.DB.UpsertCloudInstance(row); err != nil {
			return regionErrs, err
		}
		if prev != nil && prev.Zone != "" && inst.Zone != "" && prev.Zone != inst.Zone {
			_ = s.DB.AddCloudEvent(account.TenantID, account.Vendor, account.ID, inst.CloudID, "switchover", "主可用区从 "+prev.Zone+" 变为 "+inst.Zone, time.Now().UTC().Format(time.RFC3339))
		}
		evCred := base
		evCred.Region = inst.Region
		s.pullEvents(ctx, reader, evCred, account, inst.CloudID)
	}
	if onlyHosts {
		if _, err := s.DB.DeleteCloudResourcesNotIn(account.ID, engine, okRegions, seen); err != nil {
			return regionErrs, err
		}
	}
	if err := s.DB.MarkMissingCloudResourcesInRegions(account.ID, engine, okRegions, seen); err != nil {
		return regionErrs, err
	}
	var note error
	if len(regionErrs) > 0 {
		note = fmt.Errorf("%s", strings.Join(regionErrs, "; "))
	}
	s.noteSync(account, engine, "inventory", "", note)
	return regionErrs, nil
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
		if sib.InstanceID != "" {
			instanceID = sib.InstanceID
		}
	}
	if instanceID != "" {
		return nil, fmt.Errorf("已关联平台实例，请提交改绑工单")
	}
	for _, sib := range siblings {
		if sib.ID == keepID {
			continue
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
	keep.InstanceID = ""
	keep.Reconcile = cloud.ReconcileCloudOnly
	s.bindPlatform(keep, keep, "", nil)
	if keep.InstanceID != "" {
		// 同步对账可能匹配到已有平台实例；改清单账号不得回写平台绑定
		keep.InstanceID = ""
		keep.Reconcile = cloud.ReconcileCloudOnly
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

func matchAllTenants(accountTenant string) bool {
	return strings.TrimSpace(accountTenant) == store.DefaultTenantID
}

// instancesForInventoryMatch 默认租户的云账号按连接主机对照全部租户的实例。其他租户只对照自己的实例。
func (s *Service) instancesForInventoryMatch(accountTenant string) ([]store.Instance, error) {
	accountTenant = strings.TrimSpace(accountTenant)
	if accountTenant == "" {
		return nil, nil
	}
	if matchAllTenants(accountTenant) {
		return s.DB.ListInstances("", "")
	}
	return s.DB.ListInstancesInTenant(accountTenant, "", "")
}

func (s *Service) bindPlatform(row *store.CloudInstance, prev *store.CloudInstance, intranet string, plats []store.Instance) {
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
	if len(matches) == 0 {
		s.bindByIntranet(row, intranet, plats)
	} else {
		switch cloud.DecideBind(accountOf(matches), row.AccountID, len(matches)) {
		case cloud.BindFill:
			// 只标记对账，不把云账号写进平台实例；登记/改绑须工单。
			row.InstanceID = matches[0].ID
			row.Reconcile = cloud.ReconcileLinked
		case cloud.BindKeep:
			row.InstanceID = matches[0].ID
			row.Reconcile = cloud.ReconcileLinked
		case cloud.BindConflict:
			// 唯一云实例且账号不同。同步循环会把实例云账号改成这条清单的账号，再标成已关联。
			row.Reconcile = cloud.ReconcileConflict
			if len(matches) == 1 {
				row.InstanceID = matches[0].ID
			}
		default:
			row.Reconcile = cloud.ReconcileCloudOnly
		}
	}
	if prev != nil && prev.Status != "" && prev.Status != row.Status && row.Reconcile == cloud.ReconcileLinked {
		row.Reconcile = cloud.ReconcileChanged
	}
}

func (s *Service) bindByIntranet(row *store.CloudInstance, intranet string, plats []store.Instance) {
	row.Reconcile = cloud.ReconcileCloudOnly
	refs := make([]cloud.PlatformRef, 0, len(plats))
	byID := make(map[string]store.Instance, len(plats))
	for _, it := range plats {
		refs = append(refs, cloud.PlatformRef{ID: it.ID, Host: it.Host, DBType: it.DBType})
		byID[it.ID] = it
	}
	ids := cloud.MatchPlatformIDs(intranet, row.Engine, refs)
	if len(ids) != 1 {
		return
	}
	hit, ok := byID[ids[0]]
	if !ok {
		return
	}
	if cur := strings.TrimSpace(hit.CloudInstanceID); cur != "" && !strings.EqualFold(cur, row.CloudID) {
		return
	}
	row.InstanceID = hit.ID
	row.Reconcile = cloud.ReconcileLinked
}

// registeredHostKeys 是已登记主机的连接地址集合。
func registeredHostKeys(plats []store.Instance) map[string]bool {
	keys := map[string]bool{}
	for _, it := range plats {
		if !store.IsHostInstanceType(it.DBType) {
			continue
		}
		if k := store.HostAddrKey(it.Host); k != "" {
			keys[k] = true
		}
	}
	return keys
}

// matchRegisteredHostIP 返回云主机内网地址里第一个已登记为主机的地址，没有时为空。
func matchRegisteredHostIP(inst cloud.Instance, keys map[string]bool) string {
	cands := []string{inst.Host}
	if ips, ok := inst.Extras["PrivateIpAddresses"].([]any); ok {
		for _, ip := range ips {
			if s, ok := ip.(string); ok {
				cands = append(cands, s)
			}
		}
	}
	for _, c := range cands {
		if k := store.HostAddrKey(c); k != "" && keys[k] {
			return k
		}
	}
	return ""
}

func (s *Service) fillInstanceCloudBlanks(id, vendor, cloudID, region string) {
	it, err := s.DB.GetInstance(id)
	if err != nil || it == nil {
		return
	}
	if cur := strings.TrimSpace(it.CloudInstanceID); cur != "" && !strings.EqualFold(cur, strings.TrimSpace(cloudID)) {
		return
	}
	nv := cloud.BlankFill(it.CloudVendor, vendor)
	nid := cloud.BlankFill(it.CloudInstanceID, cloudID)
	nr := cloud.BlankFill(it.CloudRegion, region)
	if nv == strings.TrimSpace(it.CloudVendor) && nid == strings.TrimSpace(it.CloudInstanceID) && nr == strings.TrimSpace(it.CloudRegion) {
		return
	}
	_ = s.DB.PatchInstanceCloudIdentity(it.ID, nv, nid, nr)
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

const engineSyncTimeout = 4 * time.Minute

var (
	errNotScheduled = errors.New("未勾选定时同步")
	errSyncBusy     = errors.New("上一轮仍在执行")
)

func classifySyncErr(err error) (status, message string, keepCursor, countFail bool) {
	switch {
	case err == nil:
		return "ok", "", false, false
	case errors.Is(err, errNotScheduled):
		return "skipped", "未勾选定时同步", true, false
	case errors.Is(err, errSyncBusy):
		return "running", "上一轮仍在执行", true, false
	case errors.Is(err, cloud.ErrNoKey):
		return "no_key", "缺少只读密钥，已跳过", false, false
	default:
		return "error", err.Error(), true, true
	}
}

func (s *Service) noteSync(account *store.CloudAccount, engine, dataType, cloudID string, err error) {
	if engine == "" {
		engine = cloud.EnginePostgres
	}
	if dataType == "" {
		dataType = "inventory"
	}
	status, message, keepCursor, countFail := classifySyncErr(err)
	cur := &store.CloudCursor{
		TenantID: account.TenantID, Vendor: account.Vendor, Engine: engine, AccountID: account.ID,
		CloudID: cloudID, DataType: dataType, Status: status, Error: message,
	}
	if err != nil {
		if keepCursor || countFail {
			prev, _ := s.DB.GetCloudCursor(account.Vendor, engine, account.ID, cloudID, dataType)
			if prev != nil {
				cur.CursorAt = prev.CursorAt
				if countFail {
					cur.FailCount = prev.FailCount + 1
				}
			} else if countFail {
				cur.FailCount = 1
			}
		}
		if countFail && cur.FailCount >= 3 {
			_ = s.DB.UpsertAlert(&store.Alert{
				ID: "cloud-sync-" + account.ID + "-" + engine, Fingerprint: "cloud-sync-" + account.ID + "-" + engine,
				Severity: store.AlertSeverityP2, Title: "云资源同步连续失败", Service: "cloud",
				Status: store.AlertStatusFiring, Handle: store.AlertHandleUnclaimed,
				Labels:      map[string]string{"account_id": account.ID, "engine": engine},
				Annotations: map[string]string{"error": message},
				StartedAt:   time.Now().UTC().Format(time.RFC3339),
			})
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
