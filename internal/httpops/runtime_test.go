package httpops

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMutationRateLimit(t *testing.T) {
	limiter := NewLimiter()
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for i := 0; i < 60; i++ {
		request := httptest.NewRequest("POST", "http://api/v1/projects/p/tickets", nil)
		request.Header.Set("Authorization", "Bearer principal")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 204 {
			t.Fatalf("request %d=%d", i, response.Code)
		}
	}
	request := httptest.NewRequest("POST", "http://api/v1/projects/p/tickets", nil)
	request.Header.Set("Authorization", "Bearer principal")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 429 {
		t.Fatalf("overflow=%d", response.Code)
	}
}
