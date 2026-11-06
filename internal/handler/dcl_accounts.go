package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/ticketexec"
)

// Accounts GET /dcl-grants/accounts?instance_id=&domain=
// 用实例管理账号连到所选域名，列出库上已有账号。
func (h *DCLHandler) Accounts(c *gin.Context) {
	instanceID := strings.TrimSpace(c.Query("instance_id"))
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择数据源实例"})
		return
	}
	inst, err := resolveInstanceInTenant(h.Store, c, instanceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	domain := strings.TrimSpace(c.Query("domain"))
	if domain != "" {
		if err := ensureTicketDomainVisible(h.Store, c, inst, domain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
	}
	if !ticketexec.Executable(inst.DBType) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "不支持的数据库类型"})
		return
	}
	th := &TicketHandler{Store: h.Store, Secret: h.Secret}
	admin, err := th.execTarget(inst, "", domain)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	names, err := ticketexec.ListInstanceAccounts(ctx, admin)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "读取实例账号失败: " + err.Error()})
		return
	}
	if names == nil {
		names = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"data": names})
}
