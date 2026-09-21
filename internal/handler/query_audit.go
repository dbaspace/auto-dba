package handler

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func recordQueryAudit(st *store.DB, c *gin.Context, it *store.QueryAudit) {
	if st == nil || it == nil {
		return
	}
	it.Username = middleware.Username(c)
	it.UserID = middleware.UserID(c)
	if u, err := st.GetUserByUsername(it.Username); err == nil && u != nil {
		if it.UserID == "" {
			it.UserID = u.ID
		}
		it.DisplayName = u.DisplayName
	}
	it.IP = clientLoginIP(c)
	it.UserAgent = c.Request.UserAgent()
	if err := st.CreateQueryAudit(it); err != nil {
		log.Printf("query audit: %v", err)
	}
}

func fillInstanceAudit(it *store.QueryAudit, inst *store.Instance) {
	if it == nil || inst == nil {
		return
	}
	it.InstanceID = inst.ID
	it.InstanceName = strings.TrimSpace(inst.Name)
	if it.InstanceName == "" {
		it.InstanceName = strings.TrimSpace(inst.DomainName)
	}
	if it.InstanceName == "" {
		it.InstanceName = strings.TrimSpace(inst.Host)
	}
	it.DBType = inst.DBType
}

func requireAdminViewer(st *store.DB, c *gin.Context, denyMsg string) bool {
	if st == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return false
	}
	u, err := st.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return false
	}
	ok, err := st.UserIsAdmin(u.ID)
	if err != nil || !ok {
		c.JSON(http.StatusForbidden, gin.H{"message": denyMsg})
		return false
	}
	return true
}
