package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const ctxUserID = "auth.user_id"

type AuthConfig struct {
	UserIDHeader string
}

func Auth(cfg AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := resolveUserID(c, cfg)
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"code":    http.StatusUnauthorized,
					"message": "missing authentication credential",
					"details": gin.H{},
				},
			})
			return
		}
		c.Set(ctxUserID, uid)
		c.Next()
	}
}

func UserID(c *gin.Context) string {
	v, ok := c.Get(ctxUserID)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func resolveUserID(c *gin.Context, cfg AuthConfig) string {
	if cfg.UserIDHeader != "" {
		if v := strings.TrimSpace(c.GetHeader(cfg.UserIDHeader)); v != "" {
			return v
		}
	}
	return subFromBearer(c.GetHeader("Authorization"))
}

func subFromBearer(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	segments := strings.Split(strings.TrimSpace(parts[1]), ".")
	if len(segments) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return strings.TrimSpace(claims.Sub)
}
