package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// LocalOnlyHeaders 添加一些基础安全响应头。
// 这里先保持很小：教学项目重点是 adapter 机制，不是完整 Web 安全框架。
func LocalOnlyHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	}
}

// BearerAuth 实现本地 OpenAI-compatible 网关的最小鉴权。
//
// OpenAI SDK 会把 apiKey 放进：
//
//	Authorization: Bearer <apiKey>
//
// 因此这里校验 Bearer token，就能让这个 adapter 被 OpenAI SDK 当作 baseURL 使用。
// 默认 token 是 local-api-token，可在 config.yaml 或环境变量中覆盖。
func BearerAuth(apiToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		const prefix = "Bearer "
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, prefix) || strings.TrimSpace(strings.TrimPrefix(header, prefix)) != apiToken {
			// 错误响应也保持 OpenAI 风格，方便客户端统一处理。
			// 注意不要把期望 token 或用户传来的 token 打到日志/响应里。
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
