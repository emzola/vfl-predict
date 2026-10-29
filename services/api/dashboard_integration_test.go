package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"bet9ja-vfl/services/ingest"
)

func TestPhase4DashboardDataIntegration(t *testing.T) {
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

	// Use a unique numeric provider identity on every run so this test never
	// overwrites an unrelated disposable fixture set.
	seed := time.Now().UnixNano()
	seasonID := fmt.Sprintf("%d", seed%900000000+100000000)
	matchID := seed%900000000000 + 100000000000
	homeUID := seed%900000000 + 200000000
	awayUID := homeUID + 1
	now := time.Now().UTC().Truncate(time.Second)
	kickoff := now.Add(2 * time.Hour)

	if _, err := db.ExecContext(ctx, `INSERT INTO seasons (season_id, provider_season_id, tournament_id, provider_name, abbreviation, provider_year, current_flag, starts_at, ends_at, discovered_at, last_seen_at) VALUES ($1, $1, 14149, 'phase41-test', 'T', 'phase41', true, $2, $3, $4, $4)`, seasonID, now.Add(-time.Hour), now.Add(24*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	for uid, name := range map[int64]string{homeUID: "Fixture Test Home", awayUID: "Fixture Test Away"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO teams (team_uid, provider_team_id, name, medium_name, abbreviation, first_seen_at, last_seen_at) VALUES ($1, $1, $2, $2, $3, $4, $4)`, uid, name, name[:1], now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fixtures (match_id, provider_match_id, season_id, tournament_id, round_no, scheduled_at, provider_scheduled_uts, home_team_uid, away_team_uid, status, status_observed_at) VALUES ($1, $2, $3, 14149, 1, $4, $5, $6, $7, 'scheduled', $8)`, matchID, fmt.Sprintf("%d", matchID), seasonID, kickoff, kickoff.Unix(), homeUID, awayUID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO round_states (season_id, round_no, earliest_kickoff_at, betting_cutoff_at, state, last_fixture_observed_at, updated_at) VALUES ($1, 1, $2, $3, 'provisional', $4, $4)`, seasonID, kickoff, kickoff.Add(-30*time.Minute), now); err != nil {
		t.Fatal(err)
	}

	var rawPayloadID string
	if err := db.QueryRowContext(ctx, `INSERT INTO raw_payloads (endpoint_family, request_key, request_url, response_status, content_type, fetched_at, last_fetched_at, payload_sha256, payload_text, parser_version, schema_version, provenance) VALUES ('phase41-test', $1, 'https://test.invalid/fixture', 200, 'application/json', $2, $2, repeat('a', 64), '{"fixture":"phase41"}', 'phase41-test', 'phase41-test', '{"test_fixture":true}') RETURNING raw_payload_id::text`, fmt.Sprintf("%d", matchID), now).Scan(&rawPayloadID); err != nil {
		t.Fatal(err)
	}
	var marketDefinitionID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO market_definitions (provider_market_id, market_name) VALUES (1, '1X2') ON CONFLICT (provider_market_id, market_name) DO UPDATE SET market_name = EXCLUDED.market_name RETURNING market_definition_id`).Scan(&marketDefinitionID); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []struct {
		id   string
		name string
		odds string
	}{
		{id: "1", name: "Home", odds: "2.00"},
		{id: "X", name: "Draw", odds: "3.00"},
		{id: "2", name: "Away", odds: "4.00"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO odds_snapshots (match_id, market_definition_id, provider_market_id, outcome_id, outcome_name, raw_odds, decimal_odds, odds_valid, market_status, outcome_active, provider_result, source_timestamp_ms, source_timestamp_at, first_observed_at, last_observed_at, fetched_at, observation_kind, point_in_time_verified, raw_payload_id, provenance) VALUES ($1, $2, 1, $3, $4, $5, $6::numeric, true, 1, 1, -1, $7, $8, $8, $8, $8, 'live_observation', true, $9, '{"test_fixture":true}')`, matchID, marketDefinitionID, outcome.id, outcome.name, outcome.odds, outcome.odds, now.UnixMilli(), now, rawPayloadID); err != nil {
			t.Fatal(err)
		}
	}
	var oddsSnapshotID string
	if err := db.QueryRowContext(ctx, `SELECT odds_snapshot_id::text FROM odds_snapshots WHERE match_id = $1 ORDER BY outcome_id LIMIT 1`, matchID).Scan(&oddsSnapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO odds_snapshot_observations (odds_snapshot_id, raw_payload_id, observed_at, fetched_at, source_timestamp_ms, source_timestamp_at, observation_kind, point_in_time_verified, provenance) VALUES ($1, $2, $3, $3, $4, $3, 'live_observation', true, '{"test_fixture":true}')`, oddsSnapshotID, rawPayloadID, now, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}

	var modelDefinitionID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO model_definitions (model_key, version, description) VALUES ('phase41-fixture', $1, 'Deterministic Phase 4.1 integration fixture') ON CONFLICT (model_key, version) DO UPDATE SET description = EXCLUDED.description RETURNING model_definition_id`, seasonID).Scan(&modelDefinitionID); err != nil {
		t.Fatal(err)
	}
	var predictionRunID string
	if err := db.QueryRowContext(ctx, `INSERT INTO prediction_runs (season_id, round_no, model_definition_id, model_version, run_type, status, prediction_as_of, input_cutoff_at, earliest_kickoff_at, odds_observation_cutoff_at, input_dataset_version, prediction_policy, idempotency_key, generated_fixture_count, manifest) VALUES ($1, 1, $2, 'phase41-fixture', 'provisional', 'provisional', $3, $3, $4, $3, 'phase41-fixture', 'phase41-test', $5, 1, '{"test_fixture":true}') RETURNING prediction_run_id::text`, seasonID, modelDefinitionID, now, kickoff, seasonID).Scan(&predictionRunID); err != nil {
		t.Fatal(err)
	}
	probabilityPayload := `{"bookmaker":{"home":2,"draw":3,"away":4,"implied":{"home":0.5,"draw":0.3,"away":0.2},"overround":1.0},"secondary":{"over_2_5":0.51},"feature_values":{"home_form":0.7}}`
	if _, err := db.ExecContext(ctx, `INSERT INTO prediction_revisions (prediction_run_id, season_id, match_id, round_no, revision_no, status, prediction_as_of, cutoff_at, home_probability, draw_probability, away_probability, probability_payload, odds_snapshot_ids, odds_observed_at, provider_odds_timestamp_at, point_in_time_verified, eligible, created_at) VALUES ($1, $2, $3, 1, 1, 'provisional', $4, $4, 0.5, 0.3, 0.2, $5::jsonb, $6::jsonb, $4, $4, true, true, $4)`, predictionRunID, seasonID, matchID, now, probabilityPayload, fmt.Sprintf(`["%s"]`, oddsSnapshotID)); err != nil {
		t.Fatal(err)
	}

	config := DefaultAuthConfig()
	config.AuthorizedEmail = "owner@example.com"
	config.BaseURL = "http://localhost:3000"
	config.Mailer = &MemoryMailer{}
	server := NewServerWithAuthConfig(db, "", config)
	session := consumeTestLink(t, server, requestAuthorizedLink(t, server, "owner@example.com", "198.51.100.60:1000"), "198.51.100.60:1001")
	if session.StatusCode != http.StatusFound {
		t.Fatalf("test dashboard session status=%d", session.StatusCode)
	}

	assertJSON := func(path string, expectedStatus int) map[string]any {
		t.Helper()
		response := authenticatedRequest(t, server, http.MethodGet, path, session.Cookies, "", "http://localhost:3000")
		if response.Code != expectedStatus {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
		var decoded map[string]any
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatalf("GET %s JSON: %v", path, err)
		}
		return decoded
	}

	overview := assertJSON("/api/v1/dashboard/overview", http.StatusOK)
	if overview["current_season"] == nil {
		t.Fatalf("overview missing current season: %v", overview)
	}
	for _, path := range []string{
		"/api/v1/seasons",
		"/api/v1/seasons/" + seasonID + "/rounds",
		"/api/v1/predictions?season_id=" + seasonID + "&round=1",
		"/api/v1/dashboard/future-rounds",
		"/api/v1/dashboard/history",
		"/api/v1/matches/" + fmt.Sprintf("%d", matchID),
		"/api/v1/matches/" + fmt.Sprintf("%d", matchID) + "/odds",
		"/api/v1/performance",
		"/api/v1/paper-testing",
		"/api/v1/operations",
			"/api/v1/auto-betting",
		"/api/v1/settings",
		"/api/v1/auth/sessions",
	} {
		assertJSON(path, http.StatusOK)
	}

	patch := authenticatedJSONRequest(t, server, http.MethodPatch, "/api/v1/settings", `{"timezone":"UTC","theme":"dark","refresh_interval_seconds":60,"date_format":"locale"}`, session.Cookies, csrfCookieValue(session.Cookies), "http://localhost:3000")
	if patch.Code != http.StatusOK {
		t.Fatalf("PATCH /api/v1/settings status=%d body=%s", patch.Code, patch.Body.String())
	}
		patch = authenticatedJSONRequest(t, server, http.MethodPatch, "/api/v1/auto-betting", `{"enabled":true,"base_stake":5000}`, session.Cookies, csrfCookieValue(session.Cookies), "http://localhost:3000")
		if patch.Code != http.StatusOK {
			t.Fatalf("PATCH /api/v1/auto-betting status=%d body=%s", patch.Code, patch.Body.String())
		}

	history := assertJSON("/api/v1/dashboard/history", http.StatusOK)
	if values, ok := history["predictions"].([]any); !ok || len(values) == 0 {
		t.Fatalf("history did not return the deterministic prediction: %v", history)
	}
	future := assertJSON("/api/v1/dashboard/future-rounds", http.StatusOK)
	if values, ok := future["rounds"].([]any); !ok || len(values) == 0 {
		t.Fatalf("future rounds did not return the deterministic group: %v", future)
	}
}

func authenticatedJSONRequest(t *testing.T, server *Server, method, path, body string, cookies []*http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
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
