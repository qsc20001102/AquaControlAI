package response

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func OK(c *gin.Context, data any)      { c.JSON(http.StatusOK, Envelope{0, "success", data}) }
func Created(c *gin.Context, data any) { c.JSON(http.StatusCreated, Envelope{0, "success", data}) }
func Error(c *gin.Context, status, code int, message string, data any) {
	c.JSON(status, Envelope{code, message, data})
}
