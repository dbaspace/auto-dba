package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cloud"
	"auto-dba/internal/store"
)

// SyncCloud POST /instances/:id/sync-cloud
// 用已同步的云清单补齐云厂商、云实例 ID 和云 Region。不请求公有云，不改连接主机和云账号。
func (h *InstanceHandler) SyncCloud(c *gin.Context) {
	exist, err := h.loadInstance(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	var req struct {
		Host            string `json:"host"`
		CloudVendor     string `json:"cloud_vendor"`
		CloudInstanceID string `json:"cloud_instance_id"`
		CloudRegion     string `json:"cloud_region"`
		DBType          string `json:"db_type"`
	}
	_ = c.ShouldBindJSON(&req)
	dbType := strings.TrimSpace(req.DBType)
	if dbType == "" {
		dbType = exist.DBType
	}
	engine := cloud.EngineOfDBType(dbType)
	if engine != cloud.EnginePostgres && engine != cloud.EngineMySQL {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 PostgreSQL 和 MySQL 实例可同步云资源信息"})
		return
	}
	host := strings.TrimSpace(req.Host)
	if host == "" {
		host = exist.Host
	}
	cloudIDIn := strings.TrimSpace(req.CloudInstanceID)
	refs, err := h.cloudFillRefs(exist.TenantID, engine, cloudIDIn)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	got := cloud.FillFromInventory(host, req.CloudVendor, cloudIDIn, req.CloudRegion, refs)
	if !got.OK {
		c.JSON(http.StatusBadRequest, gin.H{"message": got.Message})
		return
	}
	vendor, cloudID, region := got.Vendor, got.CloudID, got.Region
	if err := normalizeFilledCloud(&vendor, &cloudID, &region); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if vendor != strings.TrimSpace(exist.CloudVendor) || cloudID != strings.TrimSpace(exist.CloudInstanceID) || region != strings.TrimSpace(exist.CloudRegion) {
		if err := h.Store.PatchInstanceCloudIdentity(exist.ID, vendor, cloudID, region); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	for _, id := range got.ResourceIDs {
		if err := h.Store.LinkCloudResourceIfUnbound(id, exist.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"message": got.Message,
		"data": gin.H{
			"cloud_vendor":      vendor,
			"cloud_instance_id": cloudID,
			"cloud_region":      region,
		},
	})
}

// cloudFillRefs 已填云实例 ID 时只查这一条。对不上才读取全部原始返回，用内网地址再对。
func (h *InstanceHandler) cloudFillRefs(tenantID, engine, cloudID string) ([]cloud.InventoryRef, error) {
	if cloudID != "" {
		rows, err := h.Store.ListCloudResourcesByCloudIDLite(tenantID, engine, cloudID)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return cloudRefs(engine, rows, false), nil
		}
	}
	rows, err := h.Store.ListCloudResourcesForMatch(tenantID, engine)
	if err != nil {
		return nil, err
	}
	return cloudRefs(engine, rows, true), nil
}

func cloudRefs(engine string, rows []store.CloudInstance, withIntranet bool) []cloud.InventoryRef {
	refs := make([]cloud.InventoryRef, 0, len(rows))
	for _, row := range rows {
		ref := cloud.InventoryRef{
			ID:         row.ID,
			CloudID:    row.CloudID,
			Vendor:     row.Vendor,
			Region:     row.Region,
			InstanceID: row.InstanceID,
		}
		if withIntranet {
			ref.Intranet = cloud.IntranetFromExtras(engine, row.Extras)
		}
		refs = append(refs, ref)
	}
	return refs
}

func normalizeFilledCloud(vendor, cloudID, region *string) error {
	it := &store.Instance{CloudVendor: *vendor, CloudInstanceID: *cloudID, CloudRegion: *region}
	if err := normalizeInstanceCloud(it); err != nil {
		return err
	}
	*vendor = it.CloudVendor
	*cloudID = it.CloudInstanceID
	*region = it.CloudRegion
	return nil
}
