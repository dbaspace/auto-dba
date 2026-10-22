package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/ticketenv"
)

// EnvGroups GET /api/v1/tickets/env-groups
// 申请页环境档。不改实例、环境管理或域名上的具体编码。
func (h *TicketHandler) EnvGroups(c *gin.Context) {
	var configured []config.TicketEnvGroup
	if live := config.Live(); live != nil {
		configured = live.Ticket.EnvGroups
	}
	c.JSON(http.StatusOK, gin.H{"data": ticketenv.Resolve(configured, h.knownTicketEnvCodes(c))})
}

func (h *TicketHandler) knownTicketEnvCodes(c *gin.Context) []string {
	if h == nil || h.Store == nil {
		return nil
	}
	var codes []string
	if envs, err := h.Store.ListEnvironments(); err == nil {
		for _, e := range envs {
			codes = append(codes, e.Code)
		}
	}
	tid := effectiveListTenantID(h.Store, c)
	if rows, err := h.Store.ListAccessDomainsInTenant(tid, "", "", ""); err == nil {
		for _, r := range rows {
			codes = append(codes, r.EnvCode)
		}
	}
	return codes
}
