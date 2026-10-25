package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmailAndRedirectValidation(t *testing.T) {
	value, err := normalizeEmail("Owner@Example.com")
	if err != nil || value != "owner@example.com" {
		t.Fatalf("normalized email=%q err=%v", value, err)
	}
	for _, value := range []string{"", "owner@example.com\r\nbcc@example.com", "Owner Example <owner@example.com>", "owner"} {
		if _, err := normalizeEmail(value); err == nil {
			t.Fatalf("normalizeEmail(%q) unexpectedly succeeded", value)
		}
	}
	if value, err := safeRedirectPath("/dashboard/predictions?round=4"); err != nil || value != "/dashboard/predictions?round=4" {
		t.Fatalf("safe dashboard redirect=%q err=%v", value, err)
	}
	for _, value := range []string{"https://evil.example", "//evil.example/dashboard", "/login", "/dashboard/../../admin"} {
		if _, err := safeRedirectPath(value); err == nil {
			t.Fatalf("safeRedirectPath(%q) unexpectedly succeeded", value)
		}
	}
}

func TestAuthCookiesAreOpaqueAndSecurelyScoped(t *testing.T) {
	recorder := httptest.NewRecorder()
	setAuthCookies(recorder, "opaque-session", "csrf-value", AuthConfig{SessionTTL: time.Hour, CookieSecure: true})
	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookie count=%d want 2", len(cookies))
	}
	if cookies[0].Name != sessionCookieName || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie flags=%+v", cookies[0])
	}
	if cookies[1].Name != csrfCookieName || cookies[1].HttpOnly || !cookies[1].Secure || cookies[1].SameSite != http.SameSiteLaxMode {
		t.Fatalf("csrf cookie flags=%+v", cookies[1])
	}
	if strings.Contains(recorder.Header().Get("Set-Cookie"), hashSecret("opaque-session")) {
		t.Fatal("cookie unexpectedly contains a session hash")
	}
}

func TestProtectedDashboardRequiresSession(t *testing.T) {
	server := NewServerWithAuthConfig(nil, "", AuthConfig{BaseURL: "http://127.0.0.1:3000"})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestLoginLinkFailsClosedWhenDatabaseUnavailable(t *testing.T) {
	server := NewServerWithAuthConfig(nil, "", AuthConfig{AuthorizedEmail: "owner@example.com", BaseURL: "https://private.example"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/request-link", strings.NewReader("{\"email\":\"other@example.com\"}"))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want %d when database is unavailable", response.Code, http.StatusServiceUnavailable)
	}
}

func TestProductionAuthConfigDoesNotUseMemoryMailer(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("MAILER_MODE", "memory")
	cfg := AuthConfigFromEnv()
	if _, ok := cfg.Mailer.(unavailableMailer); !ok {
		t.Fatalf("production mailer=%T, want unavailableMailer when memory mode is selected", cfg.Mailer)
	}
}

func TestSecurityHeadersAreSet(t *testing.T) {
	server := NewServerWithAuthConfig(nil, "", AuthConfig{CookieSecure: true})
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	for name, want := range map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
		"Permissions-Policy":        "camera=(), microphone=(), geolocation=()",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	} {
		if got := response.Header().Get(name); got != want {
			t.Fatalf("header %s=%q want %q", name, got, want)
		}
	}
}
