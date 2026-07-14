package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithCORSAllowsOnlyConfiguredPreviewOrigin(t *testing.T) {
	const allowedOrigin = "https://pr-42.preview.seebyte.xyz"
	handler := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), []string{allowedOrigin})

	request := httptest.NewRequest(http.MethodOptions, "/api/profiles", nil)
	request.Header.Set("Origin", allowedOrigin)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, allowedOrigin)
	}
}

func TestWithCORSDoesNotAuthorizeOtherOrigins(t *testing.T) {
	handler := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), []string{"https://pr-42.preview.seebyte.xyz"})

	request := httptest.NewRequest(http.MethodGet, "/api/profiles", nil)
	request.Header.Set("Origin", "https://pr-43.preview.seebyte.xyz")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected Access-Control-Allow-Origin header: %q", got)
	}
}
