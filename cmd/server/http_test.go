package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectCorsAllowsTrustedApplicationOrigins(t *testing.T) {
	application := &api{webPublicURL: "https://panel.example.com"}
	handler := application.cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, origin := range []string{"https://panel.example.com", "chrome-extension://nkkilejiaedcndfekaibdagbnodglnoe"} {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/projects/project-id/origins", nil)
			req.Header.Set("Origin", origin)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, req)

			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
			}
			if got := response.Header().Get("Access-Control-Allow-Origin"); got != origin {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, origin)
			}
		})
	}
}
