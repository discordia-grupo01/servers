package middleware_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/discordia-grupo01/servers/internal/middleware"
)

func run(cfg middleware.AuthConfig, headers map[string]string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Auth(cfg))
	r.GET("/x", func(c *gin.Context) { c.String(http.StatusOK, middleware.UserID(c)) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func jwt(sub string) string {
	seg := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return seg(`{"alg":"HS256"}`) + "." + seg(`{"sub":"`+sub+`"}`) + "." + seg("sig")
}

func TestAuth_KongHeader(t *testing.T) {
	w := run(middleware.AuthConfig{UserIDHeader: "X-User-Id"}, map[string]string{"X-User-Id": "  user-9  "})
	if w.Code != http.StatusOK || w.Body.String() != "user-9" {
		t.Fatalf("code=%d body=%q", w.Code, w.Body.String())
	}
}

func TestAuth_SubFromBearer(t *testing.T) {
	w := run(middleware.AuthConfig{UserIDHeader: "X-User-Id"}, map[string]string{"Authorization": "Bearer " + jwt("user-7")})
	if w.Code != http.StatusOK || w.Body.String() != "user-7" {
		t.Fatalf("code=%d body=%q", w.Code, w.Body.String())
	}
}

func TestAuth_HeaderWins(t *testing.T) {
	w := run(middleware.AuthConfig{UserIDHeader: "X-User-Id"}, map[string]string{
		"X-User-Id":     "from-header",
		"Authorization": "Bearer " + jwt("from-jwt"),
	})
	if w.Body.String() != "from-header" {
		t.Fatalf("body=%q", w.Body.String())
	}
}

func TestAuth_NoCredential(t *testing.T) {
	w := run(middleware.AuthConfig{UserIDHeader: "X-User-Id"}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestAuth_MalformedBearer(t *testing.T) {
	for _, h := range []string{"Bearer not-a-jwt", "Basic abc", "Bearer a.b"} {
		w := run(middleware.AuthConfig{}, map[string]string{"Authorization": h})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%q => code=%d", h, w.Code)
		}
	}
}
