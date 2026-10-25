package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"bet9ja-vfl/services/ingest"
)

func TestPhase4AuthMigrationTables(t *testing.T) {
	databaseURL := os.Getenv("PHASE4_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PHASE4_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, "../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"auth_authorized_accounts", "auth_magic_link_tokens", "auth_sessions", "auth_login_rate_limits", "auth_audit_events", "user_preferences"} {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s is missing", table)
		}
	}
}

func TestPhase4AuthFlow(t *testing.T) {
	databaseURL := os.Getenv("PHASE4_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PHASE4_DATABASE_URL is not set")
	}
	if os.Getenv("PHASE4_ALLOW_MUTATING_TESTS") != "1" {
		t.Skip("set PHASE4_ALLOW_MUTATING_TESTS=1 only for a disposable database")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, "../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `TRUNCATE auth_audit_events, auth_login_rate_limits, auth_magic_link_tokens, auth_sessions, user_preferences, auth_authorized_accounts CASCADE`); err != nil {
		t.Fatalf("reset disposable authentication tables: %v", err)
	}

	mailer := &MemoryMailer{}
	config := DefaultAuthConfig()
	config.AuthorizedEmail = "owner@example.com"
	config.BaseURL = "http://localhost:3000"
	config.Mailer = mailer
	config.SessionTTL = 30 * time.Minute
	config.RateLimitMax = 100
	server := NewServerWithAuthConfig(db, "", config)

	unauthorizedStatus, unauthorizedBody, unauthorizedLink := requestTestMagicLink(t, server, "intruder@example.com", "/dashboard", "198.51.100.10:1000")
	authorizedStatus, authorizedBody, authorizedLink := requestTestMagicLink(t, server, "OWNER@example.com", "/dashboard/predictions?round=4", "198.51.100.11:1000")
	if unauthorizedStatus != http.StatusAccepted || authorizedStatus != http.StatusAccepted {
		t.Fatalf("request-link statuses unauthorized=%d authorized=%d", unauthorizedStatus, authorizedStatus)
	}
	if unauthorizedBody != authorizedBody || unauthorizedLink != "" || authorizedLink == "" {
		t.Fatalf("login response enumeration or mail behavior changed: unauthorized=%q authorized=%q unauthorized_link=%q authorized_link_present=%t", unauthorizedBody, authorizedBody, unauthorizedLink, authorizedLink != "")
	}

	firstSession := consumeTestLink(t, server, authorizedLink, "198.51.100.11:1001")
	if firstSession.StatusCode != http.StatusFound || firstSession.Location != "/dashboard/predictions?round=4" {
		t.Fatalf("verified link status=%d location=%q", firstSession.StatusCode, firstSession.Location)
	}
	if firstSession.CacheControl != "no-store" || firstSession.ReferrerPolicy != "no-referrer" {
		t.Fatalf("verification response headers cache=%q referrer=%q", firstSession.CacheControl, firstSession.ReferrerPolicy)
	}
	if len(firstSession.Cookies) != 2 || firstSession.Cookies[0].Name != sessionCookieName || firstSession.Cookies[1].Name != csrfCookieName {
		t.Fatalf("verification cookies=%v", firstSession.Cookies)
	}

	if got := authenticatedRequest(t, server, http.MethodGet, "/api/v1/auth/me", firstSession.Cookies, "", "http://localhost:3000").Code; got != http.StatusOK {
		t.Fatalf("session validation status=%d", got)
	}
	if got := authenticatedRequest(t, server, http.MethodGet, "/api/v1/dashboard/overview", nil, "", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated dashboard status=%d", got)
	}
	if got := authenticatedRequest(t, server, http.MethodGet, "/api/v1/dashboard/overview", firstSession.Cookies, "", "http://localhost:3000").Code; got == http.StatusUnauthorized {
		t.Fatalf("authenticated dashboard was rejected")
	}

	if got := authenticatedRequest(t, server, http.MethodPost, "/api/v1/auth/logout", firstSession.Cookies, "", "http://localhost:3000").Code; got != http.StatusForbidden {
		t.Fatalf("logout without CSRF status=%d", got)
	}
	if got := authenticatedRequest(t, server, http.MethodPost, "/api/v1/auth/logout", firstSession.Cookies, "wrong-token", "http://localhost:3000").Code; got != http.StatusForbidden {
		t.Fatalf("logout with wrong CSRF status=%d", got)
	}
	if got := authenticatedRequest(t, server, http.MethodPost, "/api/v1/auth/logout", firstSession.Cookies, csrfCookieValue(firstSession.Cookies), "https://evil.example").Code; got != http.StatusForbidden {
		t.Fatalf("logout from foreign origin status=%d", got)
	}

	// Two independent sessions prove that revoking one session does not revoke
	// the other, and that the session cookie values are not fixed or reused.
	secondLink := requestAuthorizedLink(t, server, "owner@example.com", "198.51.100.20:1000")
	secondSession := consumeTestLink(t, server, secondLink, "198.51.100.20:1001")
	thirdLink := requestAuthorizedLink(t, server, "owner@example.com", "198.51.100.21:1000")
	thirdSession := consumeTestLink(t, server, thirdLink, "198.51.100.21:1001")
	if firstSession.Cookies[0].Value == secondSession.Cookies[0].Value || secondSession.Cookies[0].Value == thirdSession.Cookies[0].Value {
		t.Fatal("session cookie value was reused")
	}

	var sessions struct {
		Sessions []struct {
			ID      string `json:"session_id"`
			Current bool   `json:"current"`
		} `json:"sessions"`
	}
	response := authenticatedRequest(t, server, http.MethodGet, "/api/v1/auth/sessions", secondSession.Cookies, "", "http://localhost:3000")
	if response.Code != http.StatusOK {
		t.Fatalf("list sessions status=%d", response.Code)
	}
	if err := json.NewDecoder(response.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	var revokeID string
	for _, item := range sessions.Sessions {
		if !item.Current {
			revokeID = item.ID
			break
		}
	}
	if revokeID == "" {
		t.Fatalf("expected a second session to revoke: %+v", sessions.Sessions)
	}
	response = authenticatedRequest(t, server, http.MethodPost, "/api/v1/auth/sessions/"+url.PathEscape(revokeID)+"/revoke", secondSession.Cookies, csrfCookieValue(secondSession.Cookies), "http://localhost:3000")
	if response.Code != http.StatusOK {
		t.Fatalf("revoke session status=%d body=%s", response.Code, response.Body.String())
	}
	if got := authenticatedRequest(t, server, http.MethodGet, "/api/v1/auth/me", thirdSession.Cookies, "", "http://localhost:3000").Code; got != http.StatusUnauthorized {
		t.Fatalf("revoked session status=%d", got)
	}
	if got := authenticatedRequest(t, server, http.MethodGet, "/api/v1/auth/me", secondSession.Cookies, "", "http://localhost:3000").Code; got != http.StatusOK {
		t.Fatalf("unrevoked session status=%d", got)
	}

	logoutResponse := authenticatedRequest(t, server, http.MethodPost, "/api/v1/auth/logout", secondSession.Cookies, csrfCookieValue(secondSession.Cookies), "http://localhost:3000")
	if logoutResponse.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logoutResponse.Code, logoutResponse.Body.String())
	}
	if got := authenticatedRequest(t, server, http.MethodGet, "/api/v1/auth/me", secondSession.Cookies, "", "http://localhost:3000").Code; got != http.StatusUnauthorized {
		t.Fatalf("logged-out session status=%d", got)
	}

	// Replay is rejected after the atomic update has consumed the token.
	replayLink := requestAuthorizedLink(t, server, "owner@example.com", "198.51.100.30:1000")
	replayFirst := consumeTestLink(t, server, replayLink, "198.51.100.30:1001")
	if replayFirst.StatusCode != http.StatusFound {
		t.Fatalf("first replay-test redemption status=%d", replayFirst.StatusCode)
	}
	if replay := consumeTestLink(t, server, replayLink, "198.51.100.30:1002"); replay.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replayed link status=%d", replay.StatusCode)
	}

	// Expiry is checked in the same transaction as consumption.
	expiredLink := requestAuthorizedLink(t, server, "owner@example.com", "198.51.100.31:1000")
	expiredToken := tokenFromLink(t, expiredLink)
	if _, err := db.ExecContext(ctx, "UPDATE auth_magic_link_tokens SET requested_at = now() - interval '2 seconds', expires_at = now() - interval '1 second' WHERE token_hash = $1", hashSecret(expiredToken)); err != nil {
		t.Fatal(err)
	}
	if expired := consumeTestLink(t, server, expiredLink, "198.51.100.31:1001"); expired.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired link status=%d", expired.StatusCode)
	}

	// Two simultaneous redemptions must produce exactly one session.
	concurrentLink := requestAuthorizedLink(t, server, "owner@example.com", "198.51.100.32:1000")
	var concurrentResponses [2]testAuthResponse
	var wait sync.WaitGroup
	wait.Add(2)
	for index := range concurrentResponses {
		go func(index int) {
			defer wait.Done()
			concurrentResponses[index] = consumeTestLink(t, server, concurrentLink, fmt.Sprintf("198.51.100.32:%d", 1100+index))
		}(index)
	}
	wait.Wait()
	var successfulRedemptions, rejectedRedemptions int
	for _, item := range concurrentResponses {
		switch item.StatusCode {
		case http.StatusFound:
			successfulRedemptions++
		case http.StatusUnauthorized:
			rejectedRedemptions++
		default:
			t.Fatalf("concurrent redemption status=%d", item.StatusCode)
		}
	}
	if successfulRedemptions != 1 || rejectedRedemptions != 1 {
		t.Fatalf("concurrent redemption results success=%d rejected=%d", successfulRedemptions, rejectedRedemptions)
	}

	// Rate limits remain generic and do not issue another token after the
	// configured per-IP limit is reached.
	limitedMailer := &MemoryMailer{}
	limitedConfig := config
	limitedConfig.Mailer = limitedMailer
	limitedConfig.RateLimitMax = 1
	if _, err := db.ExecContext(ctx, "TRUNCATE auth_login_rate_limits"); err != nil {
		t.Fatal(err)
	}
	limitedServer := NewServerWithAuthConfig(db, "", limitedConfig)
	firstLimitedLink := requestAuthorizedLink(t, limitedServer, "owner@example.com", "198.51.100.40:1000")
	secondLimitedStatus, secondLimitedBody, secondLimitedLink := requestTestMagicLink(t, limitedServer, "owner@example.com", "/dashboard", "198.51.100.40:1000")
	if secondLimitedStatus != http.StatusAccepted || secondLimitedBody != authorizedBody || secondLimitedLink != "" || firstLimitedLink == "" {
		t.Fatalf("rate limit response status=%d body=%q link=%q", secondLimitedStatus, secondLimitedBody, secondLimitedLink)
	}

	// Expire an otherwise valid session and verify it is rejected on every
	// protected request.
	expiringLink := requestAuthorizedLink(t, server, "owner@example.com", "198.51.100.50:1000")
	expiringSession := consumeTestLink(t, server, expiringLink, "198.51.100.50:1001")
	if _, err := db.ExecContext(ctx, "UPDATE auth_sessions SET created_at = now() - interval '2 seconds', expires_at = now() - interval '1 second' WHERE session_hash = $1", hashSecret(expiringSession.Cookies[0].Value)); err != nil {
		t.Fatal(err)
	}
	if got := authenticatedRequest(t, server, http.MethodGet, "/api/v1/auth/me", expiringSession.Cookies, "", "http://localhost:3000").Code; got != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d", got)
	}

	var auditCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM auth_audit_events").Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount < 8 {
		t.Fatalf("audit event count=%d, expected authentication lifecycle records", auditCount)
	}
	rows, err := db.QueryContext(ctx, "SELECT details::text FROM auth_audit_events")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var details string
		if err := rows.Scan(&details); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(details, tokenFromLink(t, replayLink)) || strings.Contains(details, tokenFromLink(t, concurrentLink)) {
			t.Fatalf("authentication token appeared in audit details: %s", details)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

type testAuthResponse struct {
	StatusCode     int
	Location       string
	CacheControl   string
	ReferrerPolicy string
	Cookies        []*http.Cookie
	Body           string
}

func requestTestMagicLink(t *testing.T, server *Server, email, redirect, remoteAddr string) (int, string, string) {
	t.Helper()
	mailer, hasMemoryMailer := server.ensureAuthConfig().Mailer.(*MemoryMailer)
	messageCount := 0
	if hasMemoryMailer {
		messageCount = mailer.Count()
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/request-link", strings.NewReader(fmt.Sprintf(`{"email":%q,"redirect":%q}`, email, redirect)))
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	link := ""
	if hasMemoryMailer && mailer.Count() > messageCount {
		if message, exists := mailer.Last(); exists && message.Recipient == strings.ToLower(email) {
			link = message.Link
		}
	}
	return response.Code, response.Body.String(), link
}

func requestAuthorizedLink(t *testing.T, server *Server, email, remoteAddr string) string {
	t.Helper()
	_, _, link := requestTestMagicLink(t, server, email, "/dashboard", remoteAddr)
	if link == "" {
		t.Fatal("authorized request did not produce a captured development link")
	}
	return link
}

func tokenFromLink(t *testing.T, link string) string {
	t.Helper()
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatalf("link has no token: %q", link)
	}
	return token
}

func consumeTestLink(t *testing.T, server *Server, link, remoteAddr string) testAuthResponse {
	t.Helper()
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil)
	request.RemoteAddr = remoteAddr
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return testAuthResponse{StatusCode: response.Code, Location: response.Header().Get("Location"), CacheControl: response.Header().Get("Cache-Control"), ReferrerPolicy: response.Header().Get("Referrer-Policy"), Cookies: response.Result().Cookies(), Body: response.Body.String()}
}

func authenticatedRequest(t *testing.T, server *Server, method, path string, cookies []*http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func csrfCookieValue(cookies []*http.Cookie) string {
	for _, cookie := range cookies {
		if cookie.Name == csrfCookieName {
			return cookie.Value
		}
	}
	return ""
}
