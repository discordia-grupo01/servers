package apierror

import "github.com/gin-gonic/gin"

type Response struct {
	Error Detail `json:"error"`
}

type Detail struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func Write(c *gin.Context, status int, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	c.JSON(status, Response{Error: Detail{Code: status, Message: message, Details: details}})
}

func Field(field, reason string) map[string]any {
	return map[string]any{"field": field, "reason": reason}
}
