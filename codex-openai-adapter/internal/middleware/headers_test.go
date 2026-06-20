package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBearerAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized},
		{name: "wrong", header: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "valid", header: "Bearer local-api-token", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(BearerAuth("local-api-token"))
			engine.GET("/v1/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			recorder := httptest.NewRecorder()

			engine.ServeHTTP(recorder, req)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}
