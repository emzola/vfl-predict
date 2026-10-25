package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOperationalAPIRequiresInternalTokenWhenConfigured(t *testing.T) {
	server := NewServer(nil, "local-test-token")
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/current", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d", response.Code, http.StatusUnauthorized)
	}
	request = httptest.NewRequest(http.MethodGet, "/internal/v1/current", nil)
	request.Header.Set("X-Internal-Token", "local-test-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("authorized nil-db status=%d want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestOperationalAPIRejectsNonLoopbackWithoutToken(t *testing.T) {
	server := NewServer(nil, "")
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/current", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d want %d", response.Code, http.StatusForbidden)
	}
}

func TestOperationalAPIRejectsWrites(t *testing.T) {
	server := NewServer(nil, "token")
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/current", nil)
	request.Header.Set("X-Internal-Token", "token")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
