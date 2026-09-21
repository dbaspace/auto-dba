package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// respondInteractiveQueryErr maps client-aborted queries to a clear message.
// Returns true if the response was already written.
func respondInteractiveQueryErr(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	reqCanceled := errors.Is(c.Request.Context().Err(), context.Canceled)
	if errors.Is(err, context.Canceled) || reqCanceled {
		c.JSON(http.StatusOK, gin.H{"canceled": true, "message": "查询已终止"})
		return true
	}
	return false
}
