package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// resendMailer is the production transactional-email adapter. It deliberately
// keeps the provider response body out of errors and logs so API keys and
// provider diagnostics cannot accidentally become application telemetry.
type resendMailer struct {
	url    string
	apiKey string
	from   string
	http   *http.Client
}

func (m resendMailer) SendMagicLink(ctx context.Context, recipient, link string) error {
	if strings.TrimSpace(m.url) == "" || strings.TrimSpace(m.apiKey) == "" || strings.TrimSpace(m.from) == "" {
		return errors.New("resend delivery is not configured")
	}
	payload, err := json.Marshal(map[string]any{
		"from":    m.from,
		"to":      []string{recipient},
		"subject": "Your Bet9ja VFL sign-in link",
		"text":    "Use this link to sign in to the private Bet9ja VFL dashboard. It expires shortly.",
		"html":    fmt.Sprintf("<p>Use your private dashboard sign-in link:</p><p><a href=\"%s\">Sign in</a></p>", htmlAttributeEscape(link)),
	})
	if err != nil {
		return fmt.Errorf("encode resend request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := m.http
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send resend request: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("resend provider returned status %d", response.StatusCode)
	}
	return nil
}
