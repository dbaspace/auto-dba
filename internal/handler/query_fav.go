package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func (h *DatabaseHandler) ListQueryFavorites(c *gin.Context) {
	list, err := h.Store.ListQueryFavorites(middleware.Username(c), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *DatabaseHandler) CreateQueryFavorite(c *gin.Context) {
	var req struct {
		Name       string `json:"name"`
		DatabaseID string `json:"database_id"`
		DBName     string `json:"db_name"`
		SQL        string `json:"sql"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	sqlText := strings.TrimSpace(req.SQL)
	if sqlText == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "SQL 不能为空"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(req.DBName)
	}
	if name == "" {
		name = "未命名"
	}
	it := &store.QueryFavorite{
		UserID:     middleware.UserID(c),
		Username:   middleware.Username(c),
		Name:       name,
		DatabaseID: strings.TrimSpace(req.DatabaseID),
		DBName:     strings.TrimSpace(req.DBName),
		SQL:        sqlText,
	}
	if err := h.Store.CreateQueryFavorite(it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *DatabaseHandler) DeleteQueryFavorite(c *gin.Context) {
	err := h.Store.DeleteQueryFavorite(c.Param("id"), middleware.Username(c))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"message": "收藏不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *DatabaseHandler) ListMyQueryHistory(c *gin.Context) {
	list, err := h.Store.ListMyQueryAudits(middleware.Username(c), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}
