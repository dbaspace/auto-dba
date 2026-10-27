package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cloud"
	"auto-dba/internal/cloudsvc"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type CloudHandler struct {
	Store  *store.DB
	Secret string
	Svc    *cloudsvc.Service
}

func (h *CloudHandler) tenant(c *gin.Context) string {
	if middleware.IsAdminRole(middleware.Role(c)) && c.Query("all") == "1" {
		return ""
	}
	return middleware.TenantID(c)
}

func (h *CloudHandler) ListAccounts(c *gin.Context) {
	list, err := h.Store.ListCloudAccounts(h.tenant(c), false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if c.Query("mine") == "1" {
		user, _ := h.Store.GetUserByUsername(middleware.Username(c))
		if user != nil {
			var mine []store.CloudAccount
			for _, a := range list {
				ok, _ := h.Store.UserBoundToCloudAccount(a.ID, user.ID)
				if ok {
					mine = append(mine, a)
				}
			}
			list = mine
		}
	}
	if v := strings.TrimSpace(c.Query("vendor")); v != "" {
		var filtered []store.CloudAccount
		for _, a := range list {
			if a.Vendor == v {
				filtered = append(filtered, a)
			}
		}
		list = filtered
	}
	user, _ := h.Store.GetUserByUsername(middleware.Username(c))
	seeAll := middleware.IsAdminRole(middleware.Role(c))
	if user != nil && !seeAll {
		ok, _ := h.Store.UserHasURNInTenant(user.ID, middleware.TenantID(c), store.URNCloudKeys)
		seeAll = ok
	}
	if !seeAll {
		var mine []store.CloudAccount
		if user != nil {
			for _, a := range list {
				ok, _ := h.Store.UserBoundToCloudAccount(a.ID, user.ID)
				if ok {
					mine = append(mine, a)
				}
			}
		}
		list = mine
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CloudHandler) CreateAccount(c *gin.Context) {
	var req store.CloudAccount
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	switch req.Vendor {
	case "tencent", "aliyun", "huawei", "aws":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "厂商仅支持 tencent / aliyun / huawei / aws"})
		return
	}
	req.TenantID = middleware.TenantID(c)
	if err := h.Store.CreateCloudAccount(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": req})
}

func (h *CloudHandler) UpdateAccount(c *gin.Context) {
	acc, err := h.Store.GetCloudAccount(c.Param("id"))
	if err != nil || acc == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "云账号不存在"})
		return
	}
	tenant := middleware.TenantID(c)
	if !middleware.IsAdminRole(middleware.Role(c)) && tenant != "" && acc.TenantID != "" && acc.TenantID != tenant {
		c.JSON(http.StatusForbidden, gin.H{"message": "无权编辑该云账号"})
		return
	}
	var req struct {
		Name              string   `json:"name"`
		Regions           []string `json:"regions"`
		PlatformRegionIDs []string `json:"platform_region_ids"`
		EnvIDs            []string `json:"env_ids"`
		Enabled           *bool    `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	acc.Name = req.Name
	if req.Regions != nil {
		acc.Regions = req.Regions
	}
	if req.PlatformRegionIDs != nil {
		acc.PlatformRegionIDs = req.PlatformRegionIDs
	}
	if req.EnvIDs != nil {
		acc.EnvIDs = req.EnvIDs
	}
	if req.Enabled != nil {
		acc.Enabled = *req.Enabled
	}
	if err := h.Store.UpdateCloudAccount(acc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": acc})
}

func (h *CloudHandler) SaveKey(c *gin.Context) {
	var req struct {
		Role      string `json:"role"`
		SecretID  string `json:"secret_id"`
		SecretKey string `json:"secret_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.SecretKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请填写 SecretId 和 SecretKey"})
		return
	}
	acc, err := h.Store.GetCloudAccount(c.Param("id"))
	if err != nil || acc == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "云账号不存在"})
		return
	}
	enc, err := secret.Encrypt(h.Secret, req.SecretKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.SaveCloudAccountKey(acc.ID, req.Role, strings.TrimSpace(req.SecretID), enc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *CloudHandler) BindUsers(c *gin.Context) {
	var req struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	acc, err := h.Store.GetCloudAccount(c.Param("id"))
	if err != nil || acc == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "云账号不存在"})
		return
	}
	for _, id := range req.UserIDs {
		u, err := h.Store.GetUserByID(id)
		if err != nil || u == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "用户不存在"})
			return
		}
	}
	if err := h.Store.SetCloudAccountUsers(acc.ID, req.UserIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *CloudHandler) ListPostgres(c *gin.Context) {
	engine := c.Query("engine")
	if engine == "" {
		engine = cloud.EnginePostgres
	}
	list, err := h.Store.ListCloudResources(h.tenant(c), engine, c.Query("reconcile"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	for i := range list {
		if acc, _ := h.Store.GetCloudAccount(list[i].AccountID); acc != nil {
			list[i].AccountName = acc.Name
		}
		if list[i].InstanceID != "" {
			if it, _ := h.Store.GetInstance(list[i].InstanceID); it != nil {
				list[i].InstanceName = it.Name
			}
		}
	}
	caps := map[string]cloud.Caps{}
	if reader, err := cloud.GetReader(cloud.VendorTencent, engine); err == nil {
		caps[cloud.VendorTencent] = reader.Caps()
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "caps": caps, "engine": engine})
}

func (h *CloudHandler) AssignAccount(c *gin.Context) {
	var req struct {
		AccountID string `json:"account_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.AccountID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择云账号"})
		return
	}
	row, err := h.Svc.AssignAccount(c.Param("id"), strings.TrimSpace(req.AccountID), middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": row})
}

func (h *CloudHandler) SyncPostgres(c *gin.Context) {
	engine := c.Query("engine")
	if engine == "" {
		var body struct {
			Engine string `json:"engine"`
		}
		_ = c.ShouldBindJSON(&body)
		engine = body.Engine
	}
	res, err := h.Svc.SyncAllEngines(c.Request.Context(), engine)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "result": res})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "result": res})
}

func (h *CloudHandler) Health(c *gin.Context) {
	n, _ := h.Store.CountQueuedCursors()
	list, _ := h.Store.ListCloudAccounts(h.tenant(c), true)
	engine := c.Query("engine")
	if engine == "" {
		engine = "postgres"
	}
	type row struct {
		AccountID string `json:"account_id"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		Error     string `json:"error"`
		At        string `json:"cursor_at"`
		LagSec    int    `json:"lag_sec"`
		FailCount int    `json:"fail_count"`
	}
	var out []row
	for _, a := range list {
		item := row{AccountID: a.ID, Name: a.Name}
		k, _ := h.Store.CloudAccountKey(a.ID, cloud.RoleReadonly)
		if k == nil || strings.TrimSpace(k.SecretID) == "" || strings.TrimSpace(k.SecretKeyEnc) == "" {
			item.Status = "no_key"
			item.Error = "缺少只读密钥，已跳过同步"
			out = append(out, item)
			continue
		}
		cur, _ := h.Store.GetCloudCursor(a.Vendor, engine, a.ID, "", "inventory")
		if cur != nil {
			item.Status, item.Error, item.At, item.FailCount = cur.Status, cur.Error, cur.CursorAt, cur.FailCount
			if t, err := time.Parse(time.RFC3339, cur.CursorAt); err == nil {
				item.LagSec = int(time.Since(t).Seconds())
				if item.LagSec < 0 {
					item.LagSec = 0
				}
			}
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "queued": n})
}

func (h *CloudHandler) Quote(c *gin.Context) {
	var p cloud.Payload
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	q, err := h.Svc.Quote(c.Request.Context(), p)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	blip, impact := cloud.ImpactOf(p.Action, p.BeforeCPU, p.BeforeMem, p.CPU, p.MemoryMB)
	c.JSON(http.StatusOK, gin.H{"data": q, "blip": blip, "impact": impact})
}

func (h *CloudHandler) SubmitTicket(c *gin.Context) {
	var p cloud.Payload
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	var instID string
	if it, _ := h.Store.GetInstance(c.Query("instance_id")); it != nil {
		instID = it.ID
	}
	if p.Engine == "" {
		p.Engine = cloud.EnginePostgres
	}
	if instID == "" && p.CloudID != "" {
		if row, _ := h.Store.GetCloudResourceByCloudID(p.Vendor, p.Engine, p.CloudID); row != nil {
			instID = row.InstanceID
			if p.AccountID == "" {
				p.AccountID = row.AccountID
			}
			if p.BeforeCPU == 0 {
				p.BeforeCPU, p.BeforeMem, p.BeforeDisk = row.CPU, row.MemoryMB, row.DiskGB
			}
			if p.Region == "" {
				p.Region = row.Region
			}
			if p.Vendor == "" {
				p.Vendor = row.Vendor
			}
			if p.Engine == "" {
				p.Engine = row.Engine
			}
		}
	}
	issue, err := h.Svc.Submit(c.Request.Context(), p, middleware.Username(c), middleware.TenantID(c), instID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": issue})
}

func (h *CloudHandler) Parameters(c *gin.Context) {
	eng := c.Query("engine")
	if eng == "" {
		eng = cloud.EnginePostgres
	}
	row, _ := h.Store.GetCloudResourceByCloudID(c.Query("vendor"), eng, c.Param("cloudId"))
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "云实例不存在"})
		return
	}
	acc, _ := h.Store.GetCloudAccount(row.AccountID)
	if acc == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": cloud.ErrNoAccount.Error()})
		return
	}
	if row.Engine != "" {
		eng = row.Engine
	}
	reader, err := cloud.GetReader(row.Vendor, eng)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cred, err := h.SvcCred(acc, row.Region)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	list, err := reader.ListParameters(c.Request.Context(), cred, row.CloudID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CloudHandler) Specs(c *gin.Context) {
	acc, _ := h.Store.GetCloudAccount(c.Query("account_id"))
	if acc == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": cloud.ErrNoAccount.Error()})
		return
	}
	eng := c.Query("engine")
	if eng == "" {
		eng = cloud.EnginePostgres
	}
	reader, err := cloud.GetReader(acc.Vendor, eng)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cred, err := h.SvcCred(acc, c.Query("region"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	list, err := reader.ListSpecs(c.Request.Context(), cred, c.Query("zone"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cap := reader.Caps()
	c.JSON(http.StatusOK, gin.H{"data": list, "caps": cap, "scale_disk_down": cap.ScaleDiskDown, "switchover": cap.Switchover})
}

func (h *CloudHandler) SvcCred(acc *store.CloudAccount, region string) (cloud.Cred, error) {
	return h.Svc.Cred(acc, cloud.RoleReadonly, region)
}

func (h *CloudHandler) ListPolicies(c *gin.Context) {
	list, err := h.Store.ListCloudPolicies(h.tenant(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CloudHandler) SavePolicy(c *gin.Context) {
	var p store.CloudPolicy
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	if p.TenantID == "" {
		p.TenantID = middleware.TenantID(c)
	}
	if p.Mode != "auto" && p.Mode != "ticket" {
		p.Mode = "ticket"
	}
	if err := h.Store.SaveCloudPolicy(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": p})
}

func (h *CloudHandler) SlowLogs(c *gin.Context) {
	instID := c.Query("instance_id")
	it, _ := h.Store.GetInstance(instID)
	if it == nil || it.CloudInstanceID == "" {
		c.JSON(http.StatusOK, gin.H{"data": []any{}, "message": "暂未支持：实例未关联云实例"})
		return
	}
	if !cloud.Supported(it.CloudVendor, cloud.EnginePostgres) {
		c.JSON(http.StatusOK, gin.H{"data": []any{}, "message": "暂未支持"})
		return
	}
	minMS, _ := strconv.Atoi(c.Query("min_ms"))
	list, err := h.Store.ListCloudSlowLogs(it.CloudInstanceID, c.Query("from"), c.Query("to"), c.Query("db"), c.Query("q"), minMS, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	cur, _ := h.Store.GetCloudCursor(it.CloudVendor, "postgres", it.CloudAccountID, it.CloudInstanceID, "slow_log")
	c.JSON(http.StatusOK, gin.H{"data": list, "cursor": cur})
}

func (h *CloudHandler) SyncSlow(c *gin.Context) {
	var req struct {
		InstanceID string `json:"instance_id"`
	}
	_ = c.ShouldBindJSON(&req)
	user, _ := h.Store.GetUserByUsername(middleware.Username(c))
	if req.InstanceID != "" {
		it, _ := h.Store.GetInstance(req.InstanceID)
		if it == nil || it.CloudAccountID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": cloud.ErrNoAccount.Error()})
			return
		}
		if !cloud.Supported(it.CloudVendor, cloud.EnginePostgres) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "暂未支持"})
			return
		}
		if user != nil {
			ok, _ := h.Store.UserBoundToCloudAccount(it.CloudAccountID, user.ID)
			if !ok {
				c.JSON(http.StatusForbidden, gin.H{"message": cloud.ErrNotBound.Error()})
				return
			}
		}
		if err := h.Svc.QueueSlow(it.CloudInstanceID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		go h.Svc.SlowlogAll(context.Background(), it.CloudInstanceID)
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": "queued"})
		return
	}
	if err := h.Svc.QueueSlow(""); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	go h.Svc.SlowlogAll(context.Background(), "")
	c.JSON(http.StatusOK, gin.H{"ok": true, "status": "queued"})
}
