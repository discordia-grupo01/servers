package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/middleware"
)

func paramsRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RejectInvalidParams())
	r.GET("/items/:id", func(c *gin.Context) { c.String(http.StatusOK, c.Param("id")) })
	return r
}

func getParam(r http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestRejectInvalidParams_AcceptsOrdinaryValues(t *testing.T) {
	r := paramsRouter()
	for _, path := range []string{"/items/abc", "/items/user-1_2", "/items/%C3%B1and%C3%BA", "/items/%20"} {
		if w := getParam(r, path); w.Code != http.StatusOK {
			t.Errorf("%s answered %d, want 200", path, w.Code)
		}
	}
}

func TestRejectInvalidParams_RejectsNulAndInvalidUTF8(t *testing.T) {
	r := paramsRouter()
	for _, path := range []string{"/items/%00", "/items/a%00b", "/items/%FF", "/items/%C3"} {
		w := getParam(r, path)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", path, w.Code)
		}
	}
}
