package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// QueryCatalog GET /query/catalog
// 查询页尚未执行 SQL 时，只返回当前用户有权看到的已登记库。
func (h *DatabaseHandler) QueryCatalog(c *gin.Context) {
	if h == nil || h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "查询目录不可用"})
		return
	}
	dbs, insts, err := h.Store.ListQueryCatalog(
		middleware.TenantID(c),
		middleware.UserID(c),
		middleware.Username(c),
		middleware.Role(c),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if dbs == nil {
		dbs = []store.QueryCatalogDB{}
	}
	if insts == nil {
		insts = []store.QueryCatalogInstance{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"databases": dbs, "instances": insts}})
}
