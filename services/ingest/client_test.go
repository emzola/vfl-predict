package ingest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProviderURLPreservesLiteralQuestionSlash(t *testing.T) {
	got, err := ProviderURL(DefaultHistoricalBaseURL, "stats_season_fixtures2/3149425")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://vgls-vs001.akamaized.net/vfl/feeds/?/bet9javirtuals/en/America:Los_Angeles/gismo/stats_season_fixtures2/3149425"
	if got != want {
		t.Fatalf("provider URL = %q, want %q", got, want)
	}
}

func TestProviderClientRetriesAndSendsCacheValidators(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.Header.Get("If-None-Match") != `"etag-1"` || r.Header.Get("If-Modified-Since") == "" {
			t.Errorf("cache validators were not forwarded: etag=%q modified=%q", r.Header.Get("If-None-Match"), r.Header.Get("If-Modified-Since"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"queryUrl":"test","doc":[{"data":{"seasons":[]}}]}`)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.RequestSpacing = 0
	cfg.MaxRetries = 1
	client := NewProviderClient(cfg, nil)
	client.BaseDelay = 0
	client.Sleep = func(context.Context, time.Duration) error { return nil }
	response, err := client.Get(context.Background(), server.URL+"/", "test", CacheValidators{
		ETag:         `"etag-1"`,
		LastModified: "Sun, 25 Oct 2026 00:00:00 GMT",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || strings.TrimSpace(string(response.Body)) == "" {
		t.Fatalf("response = %#v", response)
	}
	if requests != 2 {
		t.Fatalf("request count = %d, want 2", requests)
	}
}

func TestProviderClientAcceptsNotModifiedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	cfg := DefaultConfig()
	cfg.RequestSpacing = 0
	cfg.MaxRetries = 0
	client := NewProviderClient(cfg, nil)
	response, err := client.Get(context.Background(), server.URL+"/", "test", CacheValidators{})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", response.StatusCode)
	}
}
