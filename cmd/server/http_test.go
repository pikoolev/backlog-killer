package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectCorsAllowsExactDashboardOrigin(t *testing.T) {
	application := &api{webPublicURL: "https://panel.example.com"}
	handler := application.cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/project-id/origins", nil)
	req.Header.Set("Origin", "https://panel.example.com")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, req)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://panel.example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}
