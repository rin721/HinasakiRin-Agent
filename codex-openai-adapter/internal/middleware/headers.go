package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func LocalOnlyHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	}
}

func BearerAuth(apiToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		const prefix = "Bearer "
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, prefix) || strings.TrimSpace(strings.TrimPrefix(header, prefix)) != apiToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "missing or invalid Authorization bearer token",
					"type":    "invalid_request_error",
					"code":    "invalid_api_key",
				},
			})
			return
		}

		c.Next()
	}
}
