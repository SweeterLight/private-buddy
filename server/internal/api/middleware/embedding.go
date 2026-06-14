package middleware

import (
	"private-buddy-server/internal/api/response"
	"private-buddy-server/internal/service"

	"github.com/gin-gonic/gin"
)

// RequireEmbedding blocks requests when the embedding config is not set up.
func RequireEmbedding(c *gin.Context) {
	if !service.IsEmbeddingConfigured() {
		response.BadRequest(c, "Embedding config is required but not configured")
		c.Abort()
		return
	}
	c.Next()
}
