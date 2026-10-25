package ingest

import (
	"os"
	"strconv"
	"strings"
	"time"

	"bet9ja-vfl/services/provider"
)

const (
	DefaultHistoricalBaseURL = "https://vgls-vs001.akamaized.net/vfl/feeds/?/bet9javirtuals/en/America:Los_Angeles/gismo/"
	DefaultLiveBaseURL       = "https://vglslive-vs001.akamaized.net/vfl/feeds/?/bet9ja/en/Europe:Berlin/gismo/"
	DefaultTournamentID      = provider.TournamentID(14149)
	ParserVersion            = "phase1-provider-parser-2026-10-25"
	SchemaVersion            = "phase1-observed-2026-10-25"
)

type Config struct {
	HistoricalBaseURL         string
	LiveBaseURL               string
	TournamentID              provider.TournamentID
	RequestSpacing            time.Duration
	RequestTimeout            time.Duration
	RetryBaseDelay            time.Duration
	MaxRetries                int
	MaxSeasons                int
	SettlementConfirmation    time.Duration
	ConnectivityEnabled       bool
	UserAgent                 string
	HistoricalObservationKind string
}

func DefaultConfig() Config {
	return Config{
		HistoricalBaseURL:         DefaultHistoricalBaseURL,
		LiveBaseURL:               DefaultLiveBaseURL,
		TournamentID:              DefaultTournamentID,
		RequestSpacing:            10 * time.Second,
		RequestTimeout:            30 * time.Second,
		RetryBaseDelay:            2 * time.Second,
		MaxRetries:                3,
		MaxSeasons:                1,
		SettlementConfirmation:    120 * time.Second,
		ConnectivityEnabled:       false,
		UserAgent:                 "bet9ja-vfl-historical-ingest/phase1",
		HistoricalObservationKind: "historical_returned_after_settlement",
	}
}

func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if value := strings.TrimSpace(os.Getenv("BET9JA_HISTORICAL_BASE_URL")); value != "" {
		cfg.HistoricalBaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("BET9JA_LIVE_BASE_URL")); value != "" {
		cfg.LiveBaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("BET9JA_TOURNAMENT_ID")); value != "" {
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil && parsed > 0 {
			cfg.TournamentID = provider.TournamentID(parsed)
		}
	}
	if value := strings.TrimSpace(os.Getenv("BET9JA_REQUEST_SPACING_SECONDS")); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed >= 0 {
			cfg.RequestSpacing = time.Duration(parsed * float64(time.Second))
		}
	} else if value := strings.TrimSpace(os.Getenv("BET9JA_REQUESTS_PER_SECOND")); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
			spacing := time.Duration(float64(time.Second) / parsed)
			if spacing > 0 {
				cfg.RequestSpacing = spacing
			}
		}
	}
	if value := strings.TrimSpace(os.Getenv("BET9JA_REQUEST_TIMEOUT_SECONDS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.RequestTimeout = time.Duration(parsed) * time.Second
		}
	}
	if value := strings.TrimSpace(os.Getenv("BET9JA_MAX_RETRIES")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			cfg.MaxRetries = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("BET9JA_MAX_SEASONS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.MaxSeasons = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("BET9JA_CONNECTIVITY_ENABLED")); value != "" {
		cfg.ConnectivityEnabled = strings.EqualFold(value, "true") || value == "1"
	}
	return cfg
}
