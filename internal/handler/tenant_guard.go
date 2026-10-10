package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// 资源归属守卫种类（路由参数 :id 指向的资源）。
const (
	GuardSrCluster       = "sr_cluster"
	GuardDbDeployCluster = "db_deploy_cluster"
	GuardHACluster       = "ha_cluster"
	GuardSrChange        = "sr_change"
	GuardDbDeployChange  = "db_deploy_change"
	GuardSrAgent         = "sr_agent"
	GuardSrAgentDeploy   = "sr_agent_deploy"
	GuardDbDeployAgent   = "db_deploy_agent"
	GuardHAController    = "ha_controller"
	GuardFlashback       = "flashback"
	GuardArchive         = "archive"
	GuardProxyService    = "proxy_service"
)

var guardTables = map[string]string{
	GuardSrAgent:       "tbl_sr_agents",
	GuardSrAgentDeploy: "tbl_sr_agent_deploys",
	GuardDbDeployAgent: "tbl_db_deploy_agents",
	GuardHAController:  "tbl_ha_controllers",
	GuardFlashback:     "tbl_flashback_tasks",
	GuardArchive:       "tbl_archive_tasks",
	GuardProxyService:  "tbl_proxy_services",
}

// TenantResourceGuard 校验 c.Param("id") 指向的资源属于当前租户，否则 404。
// 集群类 GET 请求额外放行资源授权（grant）；平台审计会话放行跨租读写。
func TenantResourceGuard(db *store.DB, kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.Param("id"))
		if id == "" || db == nil {
			c.Next()
			return
		}
		if platformAdminAuditing(db, c) {
			c.Next()
			return
		}
		tid := middleware.TenantID(c)
		readOnly := c.Request.Method == http.MethodGet
		ok, err := guardVisible(db, c, kind, id, tid, readOnly)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"message": "资源不存在"})
			return
		}
		c.Next()
	}
}

func guardVisible(db *store.DB, c *gin.Context, kind, id, tid string, readOnly bool) (bool, error) {
	switch kind {
	case GuardSrCluster:
		if readOnly {
			cl, err := resolveSrClusterInTenant(db, c, id)
			return cl != nil, err
		}
		cl, err := db.GetSrClusterInTenant(id, tid)
		return cl != nil, err
	case GuardDbDeployCluster:
		if readOnly {
			cl, err := resolveDbDeployClusterInTenant(db, c, id)
			return cl != nil, err
		}
		cl, err := db.GetDbDeployClusterInTenant(id, tid)
		return cl != nil, err
	case GuardHACluster:
		cl, err := db.GetHAClusterInTenant(id, tid)
		return cl != nil, err
	case GuardSrChange:
		ch, err := db.GetSrChangeInTenant(id, tid)
		return ch != nil, err
	case GuardDbDeployChange:
		ch, err := db.GetDbDeployChangeInTenant(id, tid)
		return ch != nil, err
	}
	if table, ok := guardTables[kind]; ok {
		return db.RowVisibleInTenant(table, id, tid)
	}
	return true, nil
}
