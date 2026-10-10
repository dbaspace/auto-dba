package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbdeploy"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// ProbeInstance POST /db-deploy/instance-probe {instance_id}：用资源配置实例账号只读探测，供登记已有集群自动填写。
func (h *DbDeployHandler) ProbeInstance(c *gin.Context) {
	var req struct {
		InstanceID string `json:"instance_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.InstanceID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "instance_id 必填"})
		return
	}
	inst, err := resolveInstanceInTenant(h.Store, c, strings.TrimSpace(req.InstanceID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	viewer := h.Store.ViewerOfInTenant(middleware.Username(c), middleware.Role(c), inst.TenantID)
	if !inst.VisibleTo(viewer) {
		c.JSON(http.StatusForbidden, gin.H{"message": "无权访问该实例"})
		return
	}
	eng := store.NormalizeDBType(inst.DBType)
	if eng != store.DbDeployEngineMySQL && eng != store.DbDeployEnginePostgres {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅支持探测 MySQL / PostgreSQL 实例"})
		return
	}
	p, err := dbdeploy.ProbeInstance(c.Request.Context(), inst, h.Secret)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": p})
}

// validateRegisterNodes 登记已有自建集群时的节点校验。角色只有 primary / replica（旧值 single 视同 primary），
// 单机即 1 个 primary、无从库。
func validateRegisterNodes(topology string, nodes []store.DbDeployNode) error {
	seen := map[string]bool{}
	primaries, replicas := 0, 0
	for i, n := range nodes {
		host := strings.TrimSpace(n.Host)
		if host == "" {
			return fmt.Errorf("第 %d 个节点主机为空", i+1)
		}
		if n.Port <= 0 {
			return fmt.Errorf("节点 %s 端口未填写", host)
		}
		k := strings.ToLower(host) + fmt.Sprintf(":%d", n.Port)
		if seen[k] {
			return fmt.Errorf("节点 %s:%d 重复", host, n.Port)
		}
		seen[k] = true
		switch n.Role {
		case "primary", "single":
			primaries++
		case "replica":
			replicas++
		default:
			return fmt.Errorf("节点 %s 角色无效：%s", host, n.Role)
		}
	}
	switch topology {
	case store.DbDeployTopoStandalone:
		if primaries != 1 || replicas != 0 {
			return fmt.Errorf("单机拓扑只能登记 1 个主节点")
		}
	case store.DbDeployTopoMasterReplica:
		if primaries != 1 {
			return fmt.Errorf("主从拓扑须且仅能有 1 个主节点")
		}
		if replicas == 0 {
			return fmt.Errorf("主从拓扑至少需要 1 个从节点，没有从库请选择单机")
		}
	}
	return nil
}

// agentIDForHost base_url 主机与节点 host 相同的 DB Agent。
func agentIDForHost(agents []store.DbDeployAgent, host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	for _, a := range agents {
		u, err := url.Parse(strings.TrimSpace(a.BaseURL))
		if err == nil && strings.ToLower(u.Hostname()) == host {
			return a.ID
		}
	}
	return ""
}
