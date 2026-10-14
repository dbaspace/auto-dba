package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbprobe"
	kquery "auto-dba/internal/kafka"
	"auto-dba/internal/middleware"
	"auto-dba/internal/redisquery"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type InstanceHandler struct {
	Store  *store.DB
	Secret string
}

func normalizeInstanceCloud(it *store.Instance) error {
	if it == nil {
		return nil
	}
	vendor := strings.ToLower(strings.TrimSpace(it.CloudVendor))
	switch vendor {
	case "", "self", "selfhosted":
		it.CloudVendor = ""
	case "tencent", "aliyun", "huawei", "aws":
		it.CloudVendor = vendor
	default:
		return fmt.Errorf("云厂商仅支持 tencent / aliyun / huawei / aws")
	}
	it.CloudInstanceID = strings.TrimSpace(it.CloudInstanceID)
	it.CloudRegion = strings.TrimSpace(it.CloudRegion)
	dbType := strings.ToLower(strings.TrimSpace(it.DBType))
	if it.CloudVendor != "" && (dbType == "postgres" || dbType == "postgresql") {
		if it.CloudInstanceID == "" {
			return fmt.Errorf("云实例须填写云实例 ID，用于从云平台下载 WAL")
		}
		if it.CloudRegion == "" {
			return fmt.Errorf("云实例须填写云 Region，如 ap-guangzhou")
		}
	}
	return nil
}

func instanceNeedsPassword(dbType string) bool {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "mysql", "postgres", "postgresql", "tidb", "starrocks", "clickhouse":
		return true
	default:
		return false
	}
}

func redactInstance(it *store.Instance) {
	if it == nil {
		return
	}
	it.PasswordSet = strings.TrimSpace(it.Password) != ""
	it.Password = ""
}

func redactInstanceCopy(it store.Instance) store.Instance {
	redactInstance(&it)
	return it
}

func (h *InstanceHandler) tenantNameMap() map[string]string {
	out := map[string]string{}
	list, err := h.Store.ListTenants()
	if err != nil {
		return out
	}
	for _, t := range list {
		out[t.ID] = t.Name
		if t.Name == "" {
			out[t.ID] = t.Code
		}
	}
	return out
}

func (h *InstanceHandler) enrichInstanceTenants(list []store.Instance) {
	names := h.tenantNameMap()
	for i := range list {
		if n := names[list[i].TenantID]; n != "" {
			list[i].TenantName = n
		}
	}
}

func (h *InstanceHandler) loadInstance(c *gin.Context, id string) (*store.Instance, error) {
	return resolveInstanceInTenant(h.Store, c, id)
}

func (h *InstanceHandler) mergeHostedInstances(list []store.Instance, tid, dbType, q string) ([]store.Instance, error) {
	ids, err := h.Store.ListInstanceIDsForTenantDatabases(tid)
	if err != nil {
		return list, err
	}
	seen := map[string]struct{}{}
	for i := range list {
		seen[list[i].ID] = struct{}{}
	}
	dbType = strings.TrimSpace(dbType)
	q = strings.ToLower(strings.TrimSpace(q))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		it, err := h.Store.GetInstance(id)
		if err != nil || it == nil {
			continue
		}
		if dbType != "" && !strings.EqualFold(it.DBType, dbType) {
			continue
		}
		if q != "" {
			blob := strings.ToLower(it.Name + " " + it.DomainName + " " + it.Host)
			if !strings.Contains(blob, q) {
				continue
			}
		}
		seen[id] = struct{}{}
		list = append(list, *it)
	}
	return list, nil
}

func (h *InstanceHandler) List(c *gin.Context) {
	tid := middleware.TenantID(c)
	var list []store.Instance
	var err error
	if platformAdminAuditing(h.Store, c) {
		list, err = h.Store.ListInstances(c.Query("db_type"), c.Query("q"))
	} else {
		list, err = h.Store.ListInstancesInTenant(tid, c.Query("db_type"), c.Query("q"))
		if err == nil {
			list, err = h.mergeHostedInstances(list, tid, c.Query("db_type"), c.Query("q"))
		}
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.Instance{}
	}
	list = store.FilterVisibleInstancesByTenant(list, viewerForRequest(h.Store, c))
	h.enrichInstanceTenants(list)
	for i := range list {
		redactInstance(&list[i])
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "audit_scope": platformAdminAuditing(h.Store, c)})
}

func (h *InstanceHandler) Get(c *gin.Context) {
	it, err := resolveInstanceInTenant(h.Store, c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	viewer := h.Store.ViewerOfInTenant(middleware.Username(c), middleware.Role(c), it.TenantID)
	if !it.VisibleTo(viewer) {
		c.JSON(http.StatusForbidden, gin.H{"message": "无权访问该实例"})
		return
	}
	redactInstance(it)
	c.JSON(http.StatusOK, gin.H{"data": it})
}

type testConnectionReq struct {
	ID       string `json:"id"`
	DBType   string `json:"db_type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *InstanceHandler) TestConnection(c *gin.Context) {
	var req testConnectionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "参数无效"})
		return
	}
	host := strings.TrimSpace(req.Host)
	dbType := strings.TrimSpace(req.DBType)
	user := strings.TrimSpace(req.Username)
	password := req.Password
	if host == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "请填写连接主机"})
		return
	}
	if req.Port <= 0 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "请填写端口"})
		return
	}
	if dbType == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "请选择实例类型"})
		return
	}
	if instanceNeedsPassword(dbType) && user == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "请填写账号"})
		return
	}
	if strings.TrimSpace(password) == "" && strings.TrimSpace(req.ID) != "" {
		stored, err := h.loadInstance(c, strings.TrimSpace(req.ID))
		if err != nil || stored == nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "message": "实例不存在，无法使用已存密码"})
			return
		}
		plain, err := secret.Decrypt(h.Secret, stored.Password)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "message": "解密已存密码失败：密钥可能已变更，请重新填写密码"})
			return
		}
		password = plain
	}
	if instanceNeedsPassword(dbType) && strings.TrimSpace(password) == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "请填写密码"})
		return
	}

	inst := &store.Instance{
		DBType:   dbType,
		Host:     host,
		Port:     req.Port,
		Username: user,
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()

	var err error
	switch strings.ToLower(dbType) {
	case "host", "os", "主机":
		addr := net.JoinHostPort(host, strconv.Itoa(req.Port))
		var d net.Dialer
		conn, dialErr := d.DialContext(ctx, "tcp", addr)
		if dialErr != nil {
			err = dialErr
		} else {
			_ = conn.Close()
		}
	case "redis":
		err = redisquery.Ping(ctx, inst, redisquery.Options{Username: user, Password: password})
	case "kafka":
		err = kquery.Ping(ctx, inst)
	case "mongodb", "mongo", "elasticsearch", "es", "elastic", "cassandra":
		// 账号密码可选：未填则无认证探测，填了则带认证
		_, err = dbprobe.List(ctx, inst, password)
	case "mysql", "postgres", "postgresql", "tidb", "starrocks", "clickhouse",
		"oceanbase", "cockroachdb", "cockroach", "sqlserver", "mssql", "oracle":
		if user == "" {
			c.JSON(http.StatusOK, gin.H{"ok": false, "message": "请填写账号"})
			return
		}
		_, err = dbprobe.List(ctx, inst, password)
	default:
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "暂不支持该类型连接测试: " + dbType})
		return
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *InstanceHandler) Create(c *gin.Context) {
	var it store.Instance
	if err := c.ShouldBindJSON(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it.Name = strings.TrimSpace(it.Name)
	it.Host = strings.TrimSpace(it.Host)
	if it.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "连接主机必填"})
		return
	}
	if store.IsHostInstanceType(it.DBType) {
		if len(it.Domains) == 0 && strings.TrimSpace(it.DomainName) == "" {
			it.Domains = []string{it.Host}
		}
		if strings.TrimSpace(it.Visibility) == "" {
			it.Visibility = "private"
		}
		if strings.TrimSpace(it.Username) == "" {
			it.Username = "sysadmin"
		}
	}
	if len(it.Domains) == 0 && strings.TrimSpace(it.DomainName) != "" {
		it.Domains = []string{strings.TrimSpace(it.DomainName)}
	}
	if it.Name == "" || len(it.Domains) == 0 || it.Port <= 0 || it.DBType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "名称、域名、端口、类型必填"})
		return
	}
	if err := normalizeInstanceCloud(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if msg := validateHostACL(&it); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": msg})
		return
	}
	if instanceNeedsPassword(it.DBType) && strings.TrimSpace(it.Password) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "该实例类型必须填写密码"})
		return
	}
	enc, err := secret.Encrypt(h.Secret, it.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "密码加密失败"})
		return
	}
	it.Password = enc
	if err := applyInstanceLocation(h.Store, &it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it.TenantID = middleware.TenantID(c)
	if err := validateResourceACL(h.Store, it.TenantID, it.Visibility, it.AccessUsers, it.RoleIDs, it.Approvers); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.CreateInstance(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "创建失败（可能是 host+port 已存在）: " + err.Error()})
		return
	}
	_, _ = h.Store.SyncAccessDomainsFromInstance(&it)
	_ = h.Store.AppendCmdbAudit(c.GetString(middleware.CtxUsername), "create", "instance", it.ID, "api", nil, redactInstanceCopy(it))
	redactInstance(&it)
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *InstanceHandler) Update(c *gin.Context) {
	exist, err := h.loadInstance(c, c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	var it store.Instance
	if err := c.ShouldBindJSON(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it.ID = exist.ID
	it.TenantID = exist.TenantID
	it.Host = strings.TrimSpace(it.Host)
	if it.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "连接主机必填"})
		return
	}
	if strings.TrimSpace(it.Password) == "" {
		it.Password = exist.Password
	} else {
		enc, err := secret.Encrypt(h.Secret, it.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "密码加密失败"})
			return
		}
		it.Password = enc
	}
	if instanceNeedsPassword(it.DBType) && strings.TrimSpace(it.Password) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "该实例类型必须填写密码"})
		return
	}
	if store.IsHostInstanceType(it.DBType) {
		if len(it.Domains) == 0 && strings.TrimSpace(it.DomainName) == "" {
			it.Domains = []string{it.Host}
		}
		if strings.TrimSpace(it.Visibility) == "" {
			it.Visibility = "private"
		}
	}
	if msg := validateHostACL(&it); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": msg})
		return
	}
	if err := validateResourceACL(h.Store, it.TenantID, it.Visibility, it.AccessUsers, it.RoleIDs, it.Approvers); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := normalizeInstanceCloud(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := applyInstanceLocation(h.Store, &it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpdateInstance(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_, _ = h.Store.SyncAccessDomainsFromInstance(&it)
	_ = h.Store.AppendCmdbAudit(c.GetString(middleware.CtxUsername), "update", "instance", it.ID, "api", redactInstanceCopy(*exist), redactInstanceCopy(it))
	redactInstance(&it)
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func validateHostACL(it *store.Instance) string {
	if it == nil || !store.IsHostInstanceType(it.DBType) {
		return ""
	}
	vis := strings.ToLower(strings.TrimSpace(it.Visibility))
	if vis == "" {
		vis = "private"
		it.Visibility = "private"
	}
	if vis != "private" {
		return ""
	}
	if len(it.AccessUsers) == 0 && len(it.RoleIDs) == 0 {
		return "主机私有可见时须指定可访问用户或绑定角色"
	}
	return ""
}

func (h *InstanceHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	exist, _ := h.loadInstance(c, id)
	if exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	if strings.EqualFold(strings.TrimSpace(exist.DBType), "host") {
		refs, err := collectHostRefs(h.Store, exist)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "检查主机引用失败: " + err.Error()})
			return
		}
		if len(refs) > 0 {
			c.JSON(http.StatusConflict, gin.H{
				"message": "主机「" + exist.Name + "」仍被使用，请先下线节点并删除 Agent：" + formatHostRefs(refs),
				"data":    refs,
			})
			return
		}
	}
	var delErr error
	if platformAdminAuditing(h.Store, c) {
		delErr = h.Store.DeleteInstance(id)
	} else {
		delErr = h.Store.DeleteInstanceInTenant(id, middleware.TenantID(c))
	}
	if delErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": delErr.Error()})
		return
	}
	var before any
	if exist != nil {
		before = redactInstanceCopy(*exist)
	}
	_ = h.Store.AppendCmdbAudit(c.GetString(middleware.CtxUsername), "delete", "instance", id, "api", before, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AssignTenant POST /instances/:id/assign-tenant — 平台审计会话下将实例划拨到目标租户。
func (h *InstanceHandler) AssignTenant(c *gin.Context) {
	if !platformAdminAuditing(h.Store, c) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅平台管理员在租户审计会话中可划拨资源"})
		return
	}
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	_ = c.ShouldBindJSON(&req)
	to := strings.TrimSpace(req.TenantID)
	if to == "" {
		to = middleware.TenantID(c)
	}
	if to == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "目标租户必填"})
		return
	}
	id := c.Param("id")
	if err := h.Store.AssignInstanceTenant(id, to); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(to, middleware.UserID(c), "resource.assign", "instance:"+id)
	it, _ := h.Store.GetInstance(id)
	if it != nil {
		h.enrichInstanceTenants([]store.Instance{*it})
		redactInstance(it)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": it})
}

func (h *InstanceHandler) kafkaInstance(c *gin.Context) (*store.Instance, bool) {
	inst, err := h.loadInstance(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return nil, false
	}
	if inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return nil, false
	}
	if strings.ToLower(strings.TrimSpace(inst.DBType)) != "kafka" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例不是 Kafka"})
		return nil, false
	}
	return inst, true
}

func (h *InstanceHandler) ListKafkaTopics(c *gin.Context) {
	inst, ok := h.kafkaInstance(c)
	if !ok {
		return
	}
	lite := c.Query("lite") == "1" || strings.EqualFold(c.Query("lite"), "true")
	refresh := c.Query("refresh") == "1" || strings.EqualFold(c.Query("refresh"), "true")
	timeout := 60 * time.Second
	if lite {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()
	list, err := kquery.ListTopicDetails(ctx, inst, lite, refresh)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []kquery.TopicDetail{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "lite": lite})
}

func (h *InstanceHandler) KafkaTopicStats(c *gin.Context) {
	inst, ok := h.kafkaInstance(c)
	if !ok {
		return
	}
	var body struct {
		Names []string `json:"names"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "names 必填"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	list, err := kquery.TopicPageStats(ctx, inst, body.Names)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []kquery.TopicDetail{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *InstanceHandler) DeleteKafkaTopic(c *gin.Context) {
	inst, ok := h.kafkaInstance(c)
	if !ok {
		return
	}
	topic := strings.TrimSpace(c.Param("topic"))
	if topic == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "topic 必填"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if err := kquery.DeleteTopic(ctx, inst, topic); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *InstanceHandler) GetKafkaTopicPartitions(c *gin.Context) {
	inst, ok := h.kafkaInstance(c)
	if !ok {
		return
	}
	topic := strings.TrimSpace(c.Param("topic"))
	if topic == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "topic 必填"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	list, err := kquery.DescribePartitions(ctx, inst, topic)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []kquery.PartitionDetail{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *InstanceHandler) GetKafkaTopicConfigs(c *gin.Context) {
	inst, ok := h.kafkaInstance(c)
	if !ok {
		return
	}
	topic := strings.TrimSpace(c.Param("topic"))
	if topic == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "topic 必填"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	list, err := kquery.DescribeTopicConfigs(ctx, inst, topic)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []kquery.TopicConfigEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *InstanceHandler) GetKafkaTopicLogs(c *gin.Context) {
	inst, ok := h.kafkaInstance(c)
	if !ok {
		return
	}
	topic := strings.TrimSpace(c.Param("topic"))
	if topic == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "topic 必填"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	list, err := kquery.DescribeTopicLogs(ctx, inst, topic)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []kquery.TopicLogEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *InstanceHandler) QueryKafkaTopicMessages(c *gin.Context) {
	inst, ok := h.kafkaInstance(c)
	if !ok {
		return
	}
	topic := strings.TrimSpace(c.Param("topic"))
	if topic == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "topic 必填"})
		return
	}
	partition := -1
	if p := c.Query("partition"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n >= 0 {
			partition = n
		}
	}
	limit := 50
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	offset := int64(-1)
	if o := c.Query("offset"); o != "" {
		if n, err := strconv.ParseInt(o, 10, 64); err == nil && n >= 0 {
			offset = n
		}
	}
	sortOrder := "oldest"
	if c.Query("sort") == "newest" {
		sortOrder = "newest"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	list, err := kquery.ReadMessages(ctx, inst, topic, partition, limit, offset, sortOrder, time.Time{})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []kquery.KafkaMessage{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *InstanceHandler) redisInstance(c *gin.Context) (*store.Instance, string, bool) {
	inst, err := h.loadInstance(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return nil, "", false
	}
	if inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return nil, "", false
	}
	if strings.ToLower(strings.TrimSpace(inst.DBType)) != "redis" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例不是 Redis"})
		return nil, "", false
	}
	password := ""
	if strings.TrimSpace(inst.Password) != "" {
		plain, decErr := secret.Decrypt(h.Secret, inst.Password)
		if decErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "解密实例密码失败: " + decErr.Error()})
			return nil, "", false
		}
		password = plain
	}
	return inst, password, true
}

func (h *InstanceHandler) ListRedisDatabases(c *gin.Context) {
	inst, password, ok := h.redisInstance(c)
	if !ok {
		return
	}
	domain := strings.TrimSpace(c.Query("domain"))
	if domain != "" {
		if err := ensureTicketDomainVisible(h.Store, c, inst, domain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
	}
	host, err := redisquery.ResolveHost(inst, domain)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	list, err := redisquery.ListDatabasesWithKeys(ctx, inst, redisquery.Options{Username: inst.Username, Password: password, Host: host})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *InstanceHandler) ExecuteRedis(c *gin.Context) {
	inst, password, ok := h.redisInstance(c)
	if !ok {
		return
	}
	var req struct {
		Command    string `json:"command"`
		SQLContent string `json:"sql_content"`
		DBName     string `json:"db_name"`
		Domain     string `json:"domain"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	domain := strings.TrimSpace(req.Domain)
	if domain != "" {
		if err := ensureTicketDomainVisible(h.Store, c, inst, domain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
	}
	host, err := redisquery.ResolveHost(inst, domain)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cmd := strings.TrimSpace(req.Command)
	if cmd == "" {
		cmd = strings.TrimSpace(req.SQLContent)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := redisquery.Execute(ctx, inst, redisquery.Options{Username: inst.Username, Password: password, Host: host}, req.DBName, cmd)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

type DatabaseHandler struct {
	Store  *store.DB
	Secret string
}

type liveDBItem struct {
	Name       string `json:"name"`
	System     bool   `json:"system"`
	Registered bool   `json:"registered"`
}

func (h *DatabaseHandler) ListLive(c *gin.Context) {
	inst, err := resolveInstanceInTenant(h.Store, c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	var registered []store.Database
	if platformAdminAuditing(h.Store, c) {
		registered, err = h.Store.ListDatabases(inst.ID, "")
	} else {
		registered, err = h.Store.ListDatabasesInTenant(effectiveListTenantID(h.Store, c), inst.ID, "")
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	password, err := secret.Decrypt(h.Secret, inst.Password)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"data": mergeLive(nil, registered), "live": false, "message": "实例密码解密失败"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	live, err := dbprobe.List(ctx, inst, password)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"data": mergeLive(nil, registered), "live": false, "message": "无法连接实例拉取库列表，可手动输入库名: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": mergeLive(live, registered), "live": true})
}

func mergeLive(live []dbprobe.Item, registered []store.Database) []liveDBItem {
	regSet := map[string]struct{}{}
	for _, r := range registered {
		regSet[r.DBName] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := make([]liveDBItem, 0, len(live)+len(registered))
	for _, it := range live {
		seen[it.Name] = struct{}{}
		_, registered := regSet[it.Name]
		out = append(out, liveDBItem{Name: it.Name, System: it.System, Registered: registered})
	}
	for _, r := range registered {
		if _, ok := seen[r.DBName]; ok {
			continue
		}
		out = append(out, liveDBItem{Name: r.DBName, System: dbprobe.IsSystem(r.DBName), Registered: true})
	}
	return out
}

func (h *DatabaseHandler) enrichDatabaseTenants(list []store.Database) {
	names := map[string]string{}
	if ts, err := h.Store.ListTenants(); err == nil {
		for _, t := range ts {
			names[t.ID] = t.Name
			if t.Name == "" {
				names[t.ID] = t.Code
			}
		}
	}
	instCache := map[string]*store.Instance{}
	for i := range list {
		iid := list[i].InstanceID
		inst := instCache[iid]
		if inst == nil && iid != "" {
			inst, _ = h.Store.GetInstance(iid)
			if inst != nil {
				instCache[iid] = inst
			}
		}
		if list[i].TenantID == "" && inst != nil {
			list[i].TenantID = inst.TenantID
		}
		if inst != nil {
			list[i].InstanceName = inst.Name
		}
		if n := names[list[i].TenantID]; n != "" {
			list[i].TenantName = n
		}
	}
}

func (h *DatabaseHandler) List(c *gin.Context) {
	tid := middleware.TenantID(c)
	var list []store.Database
	var err error
	if platformAdminAuditing(h.Store, c) {
		list, err = h.Store.ListDatabases(c.Query("instance_id"), c.Query("q"))
	} else {
		list, err = h.Store.ListDatabasesInTenant(tid, c.Query("instance_id"), c.Query("q"))
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	list = store.FilterVisibleDatabasesByTenant(list, viewerForRequest(h.Store, c))
	if !platformAdminAuditing(h.Store, c) {
		extra, gerr := h.Store.ListGrantedDatabases(tid, middleware.UserID(c), c.Query("instance_id"), c.Query("q"))
		if gerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": gerr.Error()})
			return
		}
		seen := map[string]struct{}{}
		for i := range list {
			seen[list[i].ID] = struct{}{}
		}
		for _, it := range extra {
			if _, ok := seen[it.ID]; ok {
				continue
			}
			list = append(list, it)
		}
	}
	h.enrichDatabaseTenants(list)
	c.JSON(http.StatusOK, gin.H{"data": list, "audit_scope": platformAdminAuditing(h.Store, c)})
}

// AssignTenant POST /databases/:id/assign-tenant — 只改库归属，实例地址可留在原租户。
func (h *DatabaseHandler) AssignTenant(c *gin.Context) {
	to, ok := parseAssignTenantID(h.Store, c)
	if !ok {
		return
	}
	id := c.Param("id")
	if err := h.Store.AssignDatabaseTenant(id, to); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(to, middleware.UserID(c), "resource.assign", "database:"+id)
	it, _ := h.Store.GetDatabase(id)
	if it != nil {
		list := []store.Database{*it}
		h.enrichDatabaseTenants(list)
		it = &list[0]
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": it})
}

// AssignTenantBatch POST /databases/assign-tenant — 批量指定库归属租户。
func (h *DatabaseHandler) AssignTenantBatch(c *gin.Context) {
	if !platformAdminAuditing(h.Store, c) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅平台管理员在租户审计会话中可划拨资源"})
		return
	}
	var req struct {
		TenantID string   `json:"tenant_id"`
		IDs      []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	to := strings.TrimSpace(req.TenantID)
	if to == "" {
		to = middleware.TenantID(c)
	}
	if to == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "目标租户必填"})
		return
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "ids 必填"})
		return
	}
	type failItem struct {
		ID      string `json:"id"`
		DBName  string `json:"db_name,omitempty"`
		Message string `json:"message"`
	}
	okCount := 0
	failed := make([]failItem, 0)
	actor := middleware.UserID(c)
	for _, id := range ids {
		dbName := ""
		if it, _ := h.Store.GetDatabase(id); it != nil {
			dbName = it.DBName
		}
		if err := h.Store.AssignDatabaseTenant(id, to); err != nil {
			failed = append(failed, failItem{ID: id, DBName: dbName, Message: err.Error()})
			continue
		}
		okCount++
		_ = h.Store.AppendTenantAudit(to, actor, "resource.assign", "database:"+id)
	}
	c.JSON(http.StatusOK, gin.H{
		"ok_count":   okCount,
		"fail_count": len(failed),
		"failed":     failed,
	})
}

func (h *DatabaseHandler) Create(c *gin.Context) {
	var it store.Database
	if err := c.ShouldBindJSON(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if it.InstanceID == "" || it.DBName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "instance_id 与 db_name 必填"})
		return
	}
	inst, err := resolveInstanceInTenant(h.Store, c, it.InstanceID)
	if err != nil || inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例未登记，请先在配置中创建实例"})
		return
	}
	bound, err := store.ValidateDatabaseDomains(inst, it.Domains)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it.Domains = bound
	it.TenantID = middleware.TenantID(c)
	if it.TenantID == "" {
		it.TenantID = inst.TenantID
	}
	if err := validateResourceACL(h.Store, it.TenantID, it.Visibility, it.AccessUsers, it.RoleIDs, it.Approvers); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.CreateDatabase(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "创建失败（库名可能已存在）: " + err.Error()})
		return
	}
	_ = h.Store.AppendCmdbAudit(c.GetString(middleware.CtxUsername), "create", "database", it.ID, "api", nil, it)
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *DatabaseHandler) CreateBatch(c *gin.Context) {
	var req struct {
		InstanceID  string   `json:"instance_id"`
		DBNames     []string `json:"db_names"`
		Approvers   []string `json:"approvers"`
		Visibility  string   `json:"visibility"`
		AccessUsers []string `json:"access_users"`
		RoleIDs     []string `json:"role_ids"`
		Tags        []string `json:"tags"`
		Domains     []string `json:"domains"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.InstanceID) == "" || len(req.DBNames) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "instance_id 与 db_names 必填"})
		return
	}
	inst, err := resolveInstanceInTenant(h.Store, c, req.InstanceID)
	if err != nil || inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例未登记，请先在配置中创建实例"})
		return
	}
	bound, err := store.ValidateDatabaseDomains(inst, req.Domains)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	tid := middleware.TenantID(c)
	if tid == "" {
		tid = inst.TenantID
	}
	if err := validateResourceACL(h.Store, tid, req.Visibility, req.AccessUsers, req.RoleIDs, req.Approvers); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	created, skipped, err := h.Store.CreateDatabases(&store.Database{
		TenantID:    tid,
		InstanceID:  req.InstanceID,
		Approvers:   req.Approvers,
		Visibility:  req.Visibility,
		AccessUsers: req.AccessUsers,
		RoleIDs:     req.RoleIDs,
		Tags:        req.Tags,
		Domains:     bound,
	}, req.DBNames)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "创建失败: " + err.Error()})
		return
	}
	if len(created) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "所选库均已登记"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"created": created, "skipped": skipped}})
}

func (h *DatabaseHandler) Update(c *gin.Context) {
	var exist *store.Database
	var err error
	if platformAdminAuditing(h.Store, c) {
		exist, err = h.Store.GetDatabase(c.Param("id"))
	} else {
		exist, err = h.Store.GetDatabaseInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	}
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "库未登记"})
		return
	}
	var it store.Database
	if err := c.ShouldBindJSON(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it.ID = exist.ID
	it.InstanceID = exist.InstanceID
	if it.DBName == "" {
		it.DBName = exist.DBName
	}
	inst, err := resolveParentInstance(h.Store, c, exist)
	if err != nil || inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例未登记，请先在配置中创建实例"})
		return
	}
	bound, err := store.ValidateDatabaseDomains(inst, it.Domains)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it.Domains = bound
	aclTid := exist.TenantID
	if aclTid == "" {
		aclTid = inst.TenantID
	}
	if aclTid == "" {
		aclTid = middleware.TenantID(c)
	}
	if err := validateResourceACL(h.Store, aclTid, it.Visibility, it.AccessUsers, it.RoleIDs, it.Approvers); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpdateDatabase(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendCmdbAudit(c.GetString(middleware.CtxUsername), "update", "database", it.ID, "api", exist, it)
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *DatabaseHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	exist, err := h.Store.GetDatabaseInTenant(id, effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "数据库不存在"})
		return
	}
	if err := h.Store.DeleteDatabase(exist.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendCmdbAudit(c.GetString(middleware.CtxUsername), "delete", "database", exist.ID, "api", exist, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type GlobalArgHandler struct {
	Store *store.DB
}

func (h *GlobalArgHandler) List(c *gin.Context) {
	list, err := h.Store.ListGlobalArgs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.GlobalArg{}
	}
	for i := range list {
		if store.IsFlashbackSecretArg(list[i].Key) && strings.TrimSpace(list[i].Value) != "" {
			list[i].Value = "********"
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func bindGlobalArg(c *gin.Context) (key, value, desc string, ok bool) {
	var req struct {
		Key         string `json:"key"`
		Value       string `json:"value"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Key) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "key 必填"})
		return "", "", "", false
	}
	return strings.TrimSpace(req.Key), req.Value, req.Description, true
}

func keepFlashbackSecret(db *store.DB, key, value string) string {
	if db == nil || !store.IsFlashbackSecretArg(key) {
		return value
	}
	if strings.TrimSpace(value) == "" || store.IsMaskedSecret(value) {
		if cur, err := db.GetGlobalArg(key); err == nil {
			return cur
		}
	}
	return value
}

func (h *GlobalArgHandler) Create(c *gin.Context) {
	key, value, desc, ok := bindGlobalArg(c)
	if !ok {
		return
	}
	value = keepFlashbackSecret(h.Store, key, value)
	if err := h.Store.CreateGlobalArg(key, value, desc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *GlobalArgHandler) Update(c *gin.Context) {
	key, value, desc, ok := bindGlobalArg(c)
	if !ok {
		return
	}
	value = keepFlashbackSecret(h.Store, key, value)
	if err := h.Store.SetGlobalArg(key, value, desc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *GlobalArgHandler) Delete(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "key 必填"})
		return
	}
	if err := h.Store.DeleteGlobalArg(key); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
