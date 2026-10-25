package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookieName = "vfl_session"
	csrfCookieName    = "vfl_csrf"
	primaryAccountID  = "primary"
)

var errInvalidAuthInput = errors.New("invalid authentication input")

type AuthConfig struct {
	AuthorizedEmail string
	BaseURL         string
	MagicLinkTTL    time.Duration
	SessionTTL      time.Duration
	RateLimitWindow time.Duration
	RateLimitMax    int
	CookieSecure    bool
	Mailer          MagicLinkMailer
}

type MagicLinkMailer interface {
	SendMagicLink(ctx context.Context, recipient, link string) error
}

type MagicLinkMessage struct {
	Recipient string
	Link      string
	SentAt    time.Time
}

// MemoryMailer is development/test-only and never writes links to logs.
type MemoryMailer struct {
	mu       sync.Mutex
	messages []MagicLinkMessage
}

func (m *MemoryMailer) SendMagicLink(_ context.Context, recipient, link string) error {
	if m == nil {
		return errors.New("memory mailer is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, MagicLinkMessage{Recipient: recipient, Link: link, SentAt: time.Now().UTC()})
	return nil
}

func (m *MemoryMailer) Last() (MagicLinkMessage, bool) {
	if m == nil {
		return MagicLinkMessage{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return MagicLinkMessage{}, false
	}
	return m.messages[len(m.messages)-1], true
}

type webhookMailer struct {
	url   string
	token string
	from  string
	http  *http.Client
}

func (m webhookMailer) SendMagicLink(ctx context.Context, recipient, link string) error {
	payload, err := json.Marshal(map[string]any{
		"from":    m.from,
		"to":      recipient,
		"subject": "Your Bet9ja VFL sign-in link",
		"text":    "Use this link to sign in to the private Bet9ja VFL dashboard. It expires shortly.",
		"html":    fmt.Sprintf("<p>Use your private dashboard sign-in link:</p><p><a href=\"%s\">Sign in</a></p>", htmlAttributeEscape(link)),
	})
	if err != nil {
		return fmt.Errorf("encode email request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create email request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(m.token) != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}
	client := m.http
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send email request: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("email provider returned status %d", response.StatusCode)
	}
	return nil
}

type unavailableMailer struct{}

func (unavailableMailer) SendMagicLink(context.Context, string, string) error {
	return errors.New("email delivery is not configured")
}

func DefaultAuthConfig() AuthConfig {
	return AuthConfig{
		BaseURL:         "http://127.0.0.1:3000",
		MagicLinkTTL:    10 * time.Minute,
		SessionTTL:      24 * time.Hour,
		RateLimitWindow: 15 * time.Minute,
		RateLimitMax:    5,
		Mailer:          &MemoryMailer{},
	}
}

func AuthConfigFromEnv() AuthConfig {
	cfg := DefaultAuthConfig()
	cfg.AuthorizedEmail = normalizeConfiguredEmail(os.Getenv("AUTHORIZED_EMAIL"))
	if value := strings.TrimSpace(os.Getenv("APP_BASE_URL")); value != "" {
		cfg.BaseURL = strings.TrimRight(value, "/")
	}
	if value := strings.TrimSpace(os.Getenv("MAGIC_LINK_TTL_MINUTES")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 60 {
			cfg.MagicLinkTTL = time.Duration(parsed) * time.Minute
		}
	}
	if value := strings.TrimSpace(os.Getenv("SESSION_TTL_HOURS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 720 {
			cfg.SessionTTL = time.Duration(parsed) * time.Hour
		}
	}
	if value := strings.TrimSpace(os.Getenv("AUTH_RATE_LIMIT_WINDOW_MINUTES")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 1440 {
			cfg.RateLimitWindow = time.Duration(parsed) * time.Minute
		}
	}
	if value := strings.TrimSpace(os.Getenv("AUTH_RATE_LIMIT_MAX")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 100 {
			cfg.RateLimitMax = parsed
		}
	}
	cfg.CookieSecure = strings.EqualFold(strings.TrimSpace(os.Getenv("COOKIE_SECURE")), "true") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")

	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAILER_MODE"))) {
	case "", "memory":
		cfg.Mailer = &MemoryMailer{}
	case "webhook":
		cfg.Mailer = webhookMailer{
			url:   strings.TrimSpace(os.Getenv("EMAIL_PROVIDER_URL")),
			token: os.Getenv("EMAIL_PROVIDER_TOKEN"),
			from:  strings.TrimSpace(os.Getenv("EMAIL_FROM")),
		}
	default:
		cfg.Mailer = unavailableMailer{}
	}
	return cfg
}

func normalizeConfiguredEmail(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if normalized, err := normalizeEmail(value); err == nil {
		return normalized
	}
	return value
}

func normalizeEmail(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || len(value) > 320 || strings.ContainsAny(value, "\r\n") {
		return "", errInvalidAuthInput
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || !strings.Contains(value, "@") {
		return "", errInvalidAuthInput
	}
	return value, nil
}

func htmlAttributeEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "\"", "&quot;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	return value
}

func secureRandomString(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashSecret(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func constantTimeStringEqual(left, right string) bool {
	return hmac.Equal([]byte(left), []byte(right))
}

func clientIP(r *http.Request) string {
	if r == nil || strings.TrimSpace(r.RemoteAddr) == "" {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) ensureAuthConfig() AuthConfig {
	cfg := s.Auth
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultAuthConfig().BaseURL
	}
	if cfg.MagicLinkTTL <= 0 {
		cfg.MagicLinkTTL = 10 * time.Minute
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 24 * time.Hour
	}
	if cfg.RateLimitWindow <= 0 {
		cfg.RateLimitWindow = 15 * time.Minute
	}
	if cfg.RateLimitMax <= 0 {
		cfg.RateLimitMax = 5
	}
	if cfg.Mailer == nil {
		cfg.Mailer = unavailableMailer{}
	}
	return cfg
}

func EnsureAuthorizedAccount(ctx context.Context, db *sql.DB, email string) error {
	if db == nil {
		return errors.New("database is nil")
	}
	normalized, err := normalizeEmail(email)
	if err != nil {
		return fmt.Errorf("authorized email: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin authorized account transaction: %w", err)
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRowContext(ctx, "SELECT normalized_email FROM auth_authorized_accounts WHERE account_id = $1 FOR UPDATE", primaryAccountID).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		existing = normalized
	} else if err != nil {
		return fmt.Errorf("load authorized account: %w", err)
	}
	if existing != normalized {
		if _, err := tx.ExecContext(ctx, "UPDATE auth_magic_link_tokens SET consumed_at = now() WHERE account_id = $1 AND consumed_at IS NULL", primaryAccountID); err != nil {
			return fmt.Errorf("invalidate old sign-in links: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE auth_sessions SET revoked_at = now() WHERE account_id = $1 AND revoked_at IS NULL", primaryAccountID); err != nil {
			return fmt.Errorf("revoke old sessions: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO auth_authorized_accounts (account_id, normalized_email, active) VALUES ($1, $2, true) ON CONFLICT (account_id) DO UPDATE SET normalized_email = EXCLUDED.normalized_email, active = true, updated_at = now()", primaryAccountID, normalized); err != nil {
		return fmt.Errorf("ensure authorized account: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO user_preferences (account_id) VALUES ($1) ON CONFLICT (account_id) DO NOTHING", primaryAccountID); err != nil {
		return fmt.Errorf("ensure user preferences: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit authorized account: %w", err)
	}
	return nil
}

func (s *Server) requestMagicLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !sameOriginOrAbsent(r, s.ensureAuthConfig().BaseURL) {
		writeError(w, http.StatusForbidden, "origin is not allowed")
		return
	}
	var input struct {
		Email    string
		Redirect string
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&input); err != nil {
		writeJSON(w, http.StatusAccepted, genericLoginResponse)
		return
	}
	redirectPath, err := safeRedirectPath(input.Redirect)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid redirect")
		return
	}
	normalized, normalizeErr := normalizeEmail(input.Email)
	cfg := s.ensureAuthConfig()
	if normalizeErr != nil {
		_ = s.recordAuthAudit(r.Context(), "login_rejected", "", "", false, map[string]any{"reason": "invalid_input"}, r)
		writeJSON(w, http.StatusAccepted, genericLoginResponse)
		return
	}
	if s.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "authentication service is unavailable")
		return
	}
	for _, key := range []string{"ip:" + hashSecret(clientIP(r)), "email:" + hashSecret(normalized)} {
		allowed, rateErr := s.consumeRateLimit(r.Context(), key, cfg)
		if rateErr != nil {
			writeError(w, http.StatusServiceUnavailable, "authentication service is unavailable")
			return
		}
		if !allowed {
			_ = s.recordAuthAudit(r.Context(), "login_rejected", "", hashSecret(normalized), false, map[string]any{"reason": "rate_limited"}, r)
			writeJSON(w, http.StatusAccepted, genericLoginResponse)
			return
		}
	}
	if !constantTimeStringEqual(normalized, cfg.AuthorizedEmail) || cfg.AuthorizedEmail == "" {
		_ = s.recordAuthAudit(r.Context(), "login_rejected", "", hashSecret(normalized), false, map[string]any{"reason": "not_authorized"}, r)
		writeJSON(w, http.StatusAccepted, genericLoginResponse)
		return
	}
	if err := EnsureAuthorizedAccount(r.Context(), s.DB, cfg.AuthorizedEmail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "authentication service is unavailable")
		return
	}
	token, err := secureRandomString(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authentication service is unavailable")
		return
	}
	now := s.now()
	tokenHash := hashSecret(token)
	if _, err := s.DB.ExecContext(r.Context(), "INSERT INTO auth_magic_link_tokens (token_hash, account_id, redirect_path, requested_at, expires_at) VALUES ($1, $2, $3, $4, $5)", tokenHash, primaryAccountID, redirectPath, now, now.Add(cfg.MagicLinkTTL)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "authentication service is unavailable")
		return
	}
	link := strings.TrimRight(cfg.BaseURL, "/") + "/api/v1/auth/verify?token=" + url.QueryEscape(token) + "&redirect=" + url.QueryEscape(redirectPath)
	if err := cfg.Mailer.SendMagicLink(r.Context(), normalized, link); err != nil {
		_, _ = s.DB.ExecContext(r.Context(), "UPDATE auth_magic_link_tokens SET consumed_at = $2 WHERE token_hash = $1 AND consumed_at IS NULL", tokenHash, now)
		_ = s.recordAuthAudit(r.Context(), "login_failed", primaryAccountID, hashSecret(normalized), false, map[string]any{"reason": "delivery_failed"}, r)
		writeJSON(w, http.StatusAccepted, genericLoginResponse)
		return
	}
	_ = s.recordAuthAudit(r.Context(), "login_sent", primaryAccountID, hashSecret(normalized), true, nil, r)
	writeJSON(w, http.StatusAccepted, genericLoginResponse)
}

var genericLoginResponse = map[string]string{"message": "If the address is authorized, a sign-in link will be sent."}

type authSession struct {
	PublicID     string
	SessionHash  string
	AccountID    string
	Email        string
	CSRFHash     string
	ExpiresAt    time.Time
	SessionValue string
}

func (s *Server) verifyMagicLink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if len(token) < 32 || len(token) > 128 {
		writeError(w, http.StatusUnauthorized, "invalid or expired sign-in link")
		return
	}
	redirectPath := "/dashboard"
	if candidate, err := safeRedirectPath(r.URL.Query().Get("redirect")); err == nil {
		redirectPath = candidate
	}
	if s.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "authentication service is unavailable")
		return
	}
	session, err := s.consumeMagicLink(r.Context(), hashSecret(token), redirectPath, r)
	if err != nil {
		_ = s.recordAuthAudit(r.Context(), "login_failed", "", "", false, map[string]any{"reason": "invalid_or_expired"}, r)
		writeError(w, http.StatusUnauthorized, "invalid or expired sign-in link")
		return
	}
	setAuthCookies(w, session.SessionValue, session.CSRFHash, s.ensureAuthConfig())
	_ = s.recordAuthAudit(r.Context(), "login_consumed", session.AccountID, hashSecret(session.Email), true, nil, r)
	http.Redirect(w, r, redirectPath, http.StatusFound)
}

func (s *Server) consumeMagicLink(ctx context.Context, tokenHash, redirectPath string, r *http.Request) (authSession, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return authSession{}, err
	}
	defer tx.Rollback()
	now := s.now()
	sessionValue, err := secureRandomString(32)
	if err != nil {
		return authSession{}, err
	}
	csrfValue, err := secureRandomString(32)
	if err != nil {
		return authSession{}, err
	}
	publicID, err := secureRandomUUID()
	if err != nil {
		return authSession{}, err
	}
	sessionHash := hashSecret(sessionValue)
	csrfHash := hashSecret(csrfValue)
	var accountID, storedRedirect string
	query := "UPDATE auth_magic_link_tokens SET consumed_at = $2, consumed_session_hash = $3 WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > $2 AND EXISTS (SELECT 1 FROM auth_authorized_accounts a WHERE a.account_id = auth_magic_link_tokens.account_id AND a.active) RETURNING account_id, redirect_path"
	if err := tx.QueryRowContext(ctx, query, tokenHash, now, sessionHash).Scan(&accountID, &storedRedirect); err != nil {
		return authSession{}, err
	}
	if safe, safeErr := safeRedirectPath(storedRedirect); safeErr == nil {
		redirectPath = safe
	}
	expires := now.Add(s.ensureAuthConfig().SessionTTL)
	query = "INSERT INTO auth_sessions (session_id, session_hash, account_id, csrf_token_hash, created_at, expires_at, last_seen_at, user_agent, client_ip_hash) VALUES ($1, $2, $3, $4, $5, $6, $5, $7, $8)"
	if _, err := tx.ExecContext(ctx, query, publicID, sessionHash, accountID, csrfHash, now, expires, truncate(r.UserAgent(), 512), hashSecret(clientIP(r))); err != nil {
		return authSession{}, err
	}
	var email string
	if err := tx.QueryRowContext(ctx, "SELECT normalized_email FROM auth_authorized_accounts WHERE account_id = $1", accountID).Scan(&email); err != nil {
		return authSession{}, err
	}
	query = "INSERT INTO auth_audit_events (event_type, account_id, email_hash, session_hash, client_ip_hash, success, details) VALUES ('session_created', $1, $2, $3, $4, true, $5::jsonb)"
	if _, err := tx.ExecContext(ctx, query, accountID, hashSecret(email), sessionHash, hashSecret(clientIP(r)), jsonString(map[string]any{"redirect": redirectPath})); err != nil {
		return authSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return authSession{}, err
	}
	return authSession{PublicID: publicID, SessionHash: sessionHash, AccountID: accountID, Email: email, CSRFHash: csrfValue, ExpiresAt: expires, SessionValue: sessionValue}, nil
}

func secureRandomUUID() (string, error) {
	value, err := secureRandomString(16)
	if err != nil {
		return "", err
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 16 {
		return "", errors.New("generated UUID bytes are invalid")
	}
	decoded[6] = (decoded[6] & 0x0f) | 0x40
	decoded[8] = (decoded[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", decoded[0:4], decoded[4:6], decoded[6:8], decoded[8:10], decoded[10:16]), nil
}

func (s *Server) sessionFromRequest(r *http.Request) (authSession, bool) {
	if s.DB == nil {
		return authSession{}, false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" || len(cookie.Value) > 128 {
		return authSession{}, false
	}
	var session authSession
	query := "SELECT s.session_id::text, s.session_hash, s.account_id, a.normalized_email, s.csrf_token_hash, s.expires_at FROM auth_sessions s JOIN auth_authorized_accounts a ON a.account_id = s.account_id WHERE s.session_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > $2 AND a.active"
	if err := s.DB.QueryRowContext(r.Context(), query, hashSecret(cookie.Value), s.now()).Scan(&session.PublicID, &session.SessionHash, &session.AccountID, &session.Email, &session.CSRFHash, &session.ExpiresAt); err != nil {
		return authSession{}, false
	}
	session.SessionValue = cookie.Value
	_, _ = s.DB.ExecContext(r.Context(), "UPDATE auth_sessions SET last_seen_at = $2 WHERE session_hash = $1 AND revoked_at IS NULL", session.SessionHash, s.now())
	return session, true
}

type authSessionContextKey struct{}

func withAuthSession(ctx context.Context, session authSession) context.Context {
	return context.WithValue(ctx, authSessionContextKey{}, session)
}

func authSessionFromContext(ctx context.Context) (authSession, bool) {
	session, ok := ctx.Value(authSessionContextKey{}).(authSession)
	return session, ok
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.sessionFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(withAuthSession(r.Context(), session)))
	})
}

func (s *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !sameOriginOrAbsent(r, s.ensureAuthConfig().BaseURL) {
			writeError(w, http.StatusForbidden, "origin is not allowed")
			return
		}
		session, ok := authSessionFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !s.csrfRequestIsValid(r, session) {
			writeError(w, http.StatusForbidden, "csrf validation failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) csrfRequestIsValid(r *http.Request, session authSession) bool {
	if !sameOriginOrAbsent(r, s.ensureAuthConfig().BaseURL) {
		return false
	}
	csrfCookie, err := r.Cookie(csrfCookieName)
	csrfHeader := r.Header.Get("X-CSRF-Token")
	return err == nil && csrfCookie.Value != "" && csrfHeader != "" &&
		constantTimeStringEqual(csrfCookie.Value, csrfHeader) &&
		constantTimeStringEqual(hashSecret(csrfHeader), session.CSRFHash)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := authSessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"email": session.Email, "session_expires_at": session.ExpiresAt.UTC()})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	session, ok := authSessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), "UPDATE auth_sessions SET revoked_at = $2 WHERE session_hash = $1 AND revoked_at IS NULL", session.SessionHash, s.now()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not end session")
		return
	}
	_ = s.recordAuthAudit(r.Context(), "logout", session.AccountID, hashSecret(session.Email), true, nil, r)
	clearAuthCookies(w, s.ensureAuthConfig())
	writeJSON(w, http.StatusOK, map[string]string{"message": "signed out"})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := authSessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT session_id::text, created_at, last_seen_at, expires_at, revoked_at FROM auth_sessions WHERE account_id = $1 ORDER BY created_at DESC LIMIT 20", session.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load sessions")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id string
		var created, lastSeen, expires time.Time
		var revoked sql.NullTime
		if err := rows.Scan(&id, &created, &lastSeen, &expires, &revoked); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load sessions")
			return
		}
		item := map[string]any{"session_id": id, "created_at": created.UTC(), "last_seen_at": lastSeen.UTC(), "expires_at": expires.UTC(), "current": id == session.PublicID}
		if revoked.Valid {
			item["revoked_at"] = revoked.Time.UTC()
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": items})
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	session, ok := authSessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	publicID := strings.TrimSpace(r.PathValue("sessionId"))
	if publicID == "" || len(publicID) > 64 {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	result, err := s.DB.ExecContext(r.Context(), "UPDATE auth_sessions SET revoked_at = $3 WHERE session_id = $1::uuid AND account_id = $2 AND revoked_at IS NULL", publicID, session.AccountID, s.now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	count, _ := result.RowsAffected()
	if count > 0 {
		_ = s.recordAuthAudit(r.Context(), "session_revoked", session.AccountID, hashSecret(session.Email), true, map[string]any{"target": "session"}, r)
	}
	if publicID == session.PublicID {
		clearAuthCookies(w, s.ensureAuthConfig())
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": count > 0})
}

func (s *Server) consumeRateLimit(ctx context.Context, key string, cfg AuthConfig) (bool, error) {
	var count int
	query := "INSERT INTO auth_login_rate_limits (rate_key, window_started_at, request_count) VALUES ($1, $2, 1) ON CONFLICT (rate_key) DO UPDATE SET window_started_at = CASE WHEN auth_login_rate_limits.window_started_at + ($3 * interval '1 second') <= $2 THEN $2 ELSE auth_login_rate_limits.window_started_at END, request_count = CASE WHEN auth_login_rate_limits.window_started_at + ($3 * interval '1 second') <= $2 THEN 1 ELSE auth_login_rate_limits.request_count + 1 END, updated_at = $2 RETURNING request_count"
	err := s.DB.QueryRowContext(ctx, query, key, s.now(), int(cfg.RateLimitWindow/time.Second)).Scan(&count)
	return count <= cfg.RateLimitMax, err
}

func (s *Server) recordAuthAudit(ctx context.Context, eventType, accountID, emailHash string, success bool, details map[string]any, r *http.Request) error {
	if s.DB == nil {
		return nil
	}
	query := "INSERT INTO auth_audit_events (event_type, account_id, email_hash, session_hash, client_ip_hash, success, details) VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7::jsonb)"
	_, err := s.DB.ExecContext(ctx, query, eventType, accountID, emailHash, "", hashSecret(clientIP(r)), success, jsonString(details))
	return err
}

func jsonString(value map[string]any) string {
	if value == nil {
		return "{}"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func safeRedirectPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/dashboard", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(value, "//") || path.Clean(parsed.Path) != parsed.Path || !strings.HasPrefix(parsed.Path, "/dashboard") {
		return "", errInvalidAuthInput
	}
	if parsed.Path == "/dashboard" || strings.HasPrefix(parsed.Path, "/dashboard/") {
		return parsed.RequestURI(), nil
	}
	return "", errInvalidAuthInput
}

func sameOriginOrAbsent(r *http.Request, base string) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	baseURL, err := url.Parse(strings.TrimRight(base, "/"))
	originURL, originErr := url.Parse(origin)
	return err == nil && originErr == nil && baseURL.Scheme != "" && baseURL.Host != "" && originURL.Scheme == baseURL.Scheme && strings.EqualFold(originURL.Host, baseURL.Host)
}

func setAuthCookies(w http.ResponseWriter, sessionValue, csrfValue string, cfg AuthConfig) {
	maxAge := int(cfg.SessionTTL.Seconds())
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: sessionValue, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: csrfValue, Path: "/", MaxAge: maxAge, HttpOnly: false, Secure: cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
}

func clearAuthCookies(w http.ResponseWriter, cfg AuthConfig) {
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: name == sessionCookieName, Secure: cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	}
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
