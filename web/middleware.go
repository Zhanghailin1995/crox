package web

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"time"
)

func JWT() gin.HandlerFunc {
	return func(c *gin.Context) {
		var code int
		var data interface{}

		code = SUCCESS
		token := c.Query("access_token")
		if token == "" {
			token = c.GetHeader("Authorization")
			if token == "" {
				code = INVALID_PARAMS
			} else if strings.HasPrefix(token, "Bearer ") {
				token = token[7:]
			} else {
				code = INVALID_PARAMS
			}
		}
		// 401 和 403的区别，401表示未通过认证，403表示通过了认证，但是该认证并没有权限访问该资源
		if code != SUCCESS {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": code,
				"data": data,
			})
			c.Abort()
			return
		}
		claims, err := ParseToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": code,
				"data": data,
			})
			c.Abort()
			return
		} else if timeLeft := claims.ExpiresAt - time.Now().Unix(); timeLeft < 0 {
			code = ERROR_AUTH_CHECK_TOKEN_TIMEOUT
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": code,
				"data": data,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
