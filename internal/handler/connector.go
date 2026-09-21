package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/store"
)

type ConnectorHandler struct {
	Store *store.DB
}

func (h *ConnectorHandler) List(c *gin.Context) {
	list, err := h.Store.ListConnectors(c.Query("env"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.Connector{}
	}
	for i := range list {
		list[i].KafkaHost = cdc.NormalizeKafkaBootstrap(list[i].KafkaHost)
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *ConnectorHandler) Get(c *gin.Context) {
	it, err := h.Store.GetConnector(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "连接器不存在"})
		return
	}
	it.KafkaHost = cdc.NormalizeKafkaBootstrap(it.KafkaHost)
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func normalizeConnector(it *store.Connector) error {
	it.DBZHost = strings.TrimSpace(it.DBZHost)
	it.Env = strings.TrimSpace(it.Env)
	it.KafkaHost = cdc.NormalizeKafkaBootstrap(strings.TrimSpace(it.KafkaHost))
	it.Remark = strings.TrimSpace(it.Remark)
	it.MessageSchemaMode = store.NormalizeMessageSchemaMode(it.MessageSchemaMode)
	if it.DBZHost == "" {
		return errBad("请输入 dbz_host")
	}
	if it.Env == "" {
		return errBad("请选择环境")
	}
	if it.KafkaHost == "" {
		return errBad("请选择 Kafka 资源地址")
	}
	return nil
}

type badReq struct{ msg string }

func (e badReq) Error() string { return e.msg }

func errBad(msg string) error { return badReq{msg: msg} }

func (h *ConnectorHandler) Create(c *gin.Context) {
	var it store.Connector
	if err := c.ShouldBindJSON(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := normalizeConnector(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.CreateConnector(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *ConnectorHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetConnector(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "连接器不存在"})
		return
	}
	var it store.Connector
	if err := c.ShouldBindJSON(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it.ID = exist.ID
	if err := normalizeConnector(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpdateConnector(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *ConnectorHandler) Delete(c *gin.Context) {
	if err := h.Store.DeleteConnector(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
