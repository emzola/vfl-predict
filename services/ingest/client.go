package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxProviderResponseBytes = 32 << 20

type CacheValidators struct {
	ETag         string
	LastModified string
}

type Response struct {
	URL          string
	StatusCode   int
	Body         []byte
	FetchedAt    time.Time
	ContentType  string
	CacheControl string
	ETag         string
	LastModified string
	RetryAfter   string
}

type HTTPStatusError struct {
	URL        string
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("provider request %s returned HTTP %d", e.URL, e.StatusCode)
}

type RequestBudget struct {
	mu      sync.Mutex
	next    time.Time
	spacing time.Duration
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
}

func NewRequestBudget(spacing time.Duration) *RequestBudget {
	return &RequestBudget{
		spacing: spacing,
		now:     time.Now,
		sleep:   sleepContext,
	}
}

func (b *RequestBudget) Wait(ctx context.Context) error {
	if b == nil || b.spacing <= 0 {
		return nil
	}
	now := b.now()
	b.mu.Lock()
	slot := now
	if b.next.After(slot) {
		slot = b.next
	}
	b.next = slot.Add(b.spacing)
	b.mu.Unlock()
	return b.sleep(ctx, slot.Sub(now))
}

type ProviderClient struct {
	HTTPClient *http.Client
	Budget     *RequestBudget
	MaxRetries int
	BaseDelay  time.Duration
	UserAgent  string
	Now        func() time.Time
	Sleep      func(context.Context, time.Duration) error
}

func NewProviderClient(cfg Config, transport http.RoundTripper) *ProviderClient {
	httpClient := &http.Client{Timeout: cfg.RequestTimeout}
	if transport != nil {
		httpClient.Transport = transport
	}
	client := &ProviderClient{
		HTTPClient: httpClient,
		Budget:     NewRequestBudget(cfg.RequestSpacing),
		MaxRetries: cfg.MaxRetries,
		BaseDelay:  cfg.RetryBaseDelay,
		UserAgent:  cfg.UserAgent,
		Now:        time.Now,
		Sleep:      sleepContext,
	}
	if client.MaxRetries < 0 {
		client.MaxRetries = 0
	}
	if client.BaseDelay <= 0 {
		client.BaseDelay = time.Second
	}
	return client
}

func ProviderURL(baseURL, relativePath string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", errors.New("provider base URL is empty")
	}
	if strings.Contains(baseURL, "@") {
		return "", errors.New("provider base URL must not contain embedded credentials")
	}
	joined := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(relativePath, "/")
	parsed, err := url.Parse(joined)
	if err != nil {
		return "", fmt.Errorf("parse provider URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("provider URL scheme %q is not allowed", parsed.Scheme)
	}
	if parsed.User != nil {
		return "", errors.New("provider URL must not contain embedded credentials")
	}
	return joined, nil
}

func (c *ProviderClient) Get(ctx context.Context, baseURL, relativePath string, validators CacheValidators) (*Response, error) {
	if c == nil || c.HTTPClient == nil {
		return nil, errors.New("provider client is not initialized")
	}
	requestURL, err := ProviderURL(baseURL, relativePath)
	if err != nil {
		return nil, err
	}
	maxRetries := c.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if c.Budget != nil {
			if err := c.Budget.Wait(ctx); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build provider request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		if c.UserAgent != "" {
			req.Header.Set("User-Agent", c.UserAgent)
		}
		if validators.ETag != "" {
			req.Header.Set("If-None-Match", validators.ETag)
		}
		if validators.LastModified != "" {
			req.Header.Set("If-Modified-Since", validators.LastModified)
		}
		response, requestErr := c.doOnce(ctx, req)
		if requestErr != nil {
			if attempt == maxRetries {
				return response, requestErr
			}
			if err := c.waitRetry(ctx, attempt, ""); err != nil {
				return response, err
			}
			continue
		}
		if response.StatusCode == http.StatusNotModified {
			return response, nil
		}
		if retryableStatus(response.StatusCode) {
			if attempt == maxRetries {
				return response, &HTTPStatusError{URL: requestURL, StatusCode: response.StatusCode}
			}
			if err := c.waitRetry(ctx, attempt, response.RetryAfter); err != nil {
				return response, err
			}
			continue
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return response, &HTTPStatusError{URL: requestURL, StatusCode: response.StatusCode}
		}
		return response, nil
	}
	return nil, errors.New("provider request retry loop exhausted")
}

func (c *ProviderClient) doOnce(ctx context.Context, req *http.Request) (*Response, error) {
	response, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil {
		return &Response{URL: req.URL.String(), StatusCode: response.StatusCode, FetchedAt: c.now(), RetryAfter: response.Header.Get("Retry-After")}, err
	}
	if len(body) > maxProviderResponseBytes {
		return &Response{URL: req.URL.String(), StatusCode: response.StatusCode, FetchedAt: c.now(), RetryAfter: response.Header.Get("Retry-After")}, fmt.Errorf("provider response exceeds %d bytes", maxProviderResponseBytes)
	}
	return &Response{
		URL:          req.URL.String(),
		StatusCode:   response.StatusCode,
		Body:         body,
		FetchedAt:    c.now(),
		ContentType:  response.Header.Get("Content-Type"),
		CacheControl: response.Header.Get("Cache-Control"),
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
		RetryAfter:   response.Header.Get("Retry-After"),
	}, nil
}

func (c *ProviderClient) waitRetry(ctx context.Context, attempt int, retryAfter string) error {
	delay := c.BaseDelay
	if delay <= 0 {
		delay = time.Second
	}
	if attempt > 0 {
		factor := math.Pow(2, float64(attempt))
		delay = time.Duration(float64(delay) * factor)
	}
	if retryAfterDelay, ok := parseRetryAfter(retryAfter, c.now()); ok && retryAfterDelay > delay {
		delay = retryAfterDelay
	}
	return c.sleep(ctx, delay)
}

func (c *ProviderClient) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *ProviderClient) sleep(ctx context.Context, delay time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, delay)
	}
	return sleepContext(ctx, delay)
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if when.Before(now) {
		return 0, true
	}
	return when.Sub(now), true
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
