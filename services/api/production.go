package api

import (
	"errors"
	"net/url"
	"strings"
)

// ValidateProductionAuthConfig makes an accidentally incomplete production
// environment fail closed during API startup instead of failing only when the
// first login is attempted.
func ValidateProductionAuthConfig(cfg AuthConfig) error {
	if strings.TrimSpace(cfg.AuthorizedEmail) == "" {
		return errors.New("AUTHORIZED_EMAIL must be set in production")
	}
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return errors.New("APP_BASE_URL must be an absolute HTTPS URL in production")
	}
	if !cfg.CookieSecure {
		return errors.New("COOKIE_SECURE must be enabled in production")
	}
	mailer, ok := cfg.Mailer.(resendMailer)
	if !ok || strings.TrimSpace(mailer.apiKey) == "" || strings.TrimSpace(mailer.from) == "" {
		return errors.New("MAILER_MODE=resend with RESEND_API_KEY and EMAIL_FROM is required in production")
	}
	return nil
}
