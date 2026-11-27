package middleware

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/apierr"
)

func RejectInvalidParams() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, p := range c.Params {
			if !validParam(p.Value) {
				apierr.Abort(c, http.StatusBadRequest, apierr.CodeInvalidParameter, "invalid "+p.Key, apierr.FieldErr(p.Key, "invalid_characters"))
				return
			}
		}
		c.Next()
	}
}

func validParam(v string) bool {
	return utf8.ValidString(v) && !strings.ContainsRune(v, 0)
}
