// Package response provides unified API response helpers.
// All handlers use business codes (code field) instead of HTTP status codes
// to distinguish between different error/success scenarios.
// HTTP level always returns 200 to let the client parse the business code.
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response is the unified API response envelope.
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Business codes — separate business semantics from HTTP transport.
const (
	CodeSuccess       = 0
	CodeBadRequest    = 1
	CodeNotFound      = 2
	CodeInternalError = 3
)

// Success returns a successful response with data.
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    CodeSuccess,
		Message: "success",
		Data:    data,
	})
}

// SuccessMessage returns a successful response with a custom message and data.
func SuccessMessage(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    CodeSuccess,
		Message: message,
		Data:    data,
	})
}

// BadRequest returns a client error response.
func BadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusOK, Response{
		Code:    CodeBadRequest,
		Message: message,
	})
}

// NotFound returns a not-found response.
func NotFound(c *gin.Context, message string) {
	c.JSON(http.StatusOK, Response{
		Code:    CodeNotFound,
		Message: message,
	})
}

// InternalError returns an internal server error response.
func InternalError(c *gin.Context, message string) {
	c.JSON(http.StatusOK, Response{
		Code:    CodeInternalError,
		Message: message,
	})
}
