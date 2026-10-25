package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Server exposes only operational, read-only endpoints. The command that
// starts it binds to loopback by default; an optional token is required for
// deployments that place the service behind a private network interface.
type Server struct {
	DB            *sql.DB
	InternalToken string
	Now           func() time.Time
	Auth          AuthConfig
}

func NewServer(db *sql.DB, internalToken string) *Server {
	return NewServerWithAuthConfig(db, internalToken, AuthConfigFromEnv())
}

func NewServerWithAuthConfig(db *sql.DB, internalToken string, auth AuthConfig) *Server {
	return &Server{DB: db, InternalToken: strings.TrimSpace(internalToken), Now: time.Now, Auth: auth}
}

func (s *Server) Handler() http.Handler {
	root := http.NewServeMux()
	root.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	internal := http.NewServeMux()
	internal.HandleFunc("/internal/v1/current", s.current)
	internal.HandleFunc("/internal/v1/upcoming", s.upcoming)
	internal.HandleFunc("/internal/v1/predictions/provisional", s.predictions("provisional"))
	internal.HandleFunc("/internal/v1/predictions/frozen", s.predictions("frozen"))
	internal.HandleFunc("/internal/v1/predictions/history", s.predictionHistory)
	internal.HandleFunc("/internal/v1/metrics/models", s.modelMetrics)
	internal.HandleFunc("/internal/v1/metrics/forward", s.forwardMetrics)
	internal.HandleFunc("/internal/v1/health/feed", s.feedHealth)
	root.Handle("/internal/v1/", s.guard(internal))

	root.HandleFunc("/api/v1/auth/request-link", s.requestMagicLink)
	root.HandleFunc("/api/v1/auth/verify", s.verifyMagicLink)
	root.Handle("/api/v1/auth/me", s.requireAuth(http.HandlerFunc(s.me)))
	root.Handle("/api/v1/auth/logout", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.logout))))
	root.Handle("/api/v1/auth/sessions", s.requireAuth(http.HandlerFunc(s.listSessions)))
	root.Handle("/api/v1/auth/sessions/{sessionId}/revoke", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.revokeSession))))

	authenticated := func(handler http.Handler) http.Handler {
		return s.requireAuth(handler)
	}
	root.Handle("/api/v1/dashboard/", authenticated(http.HandlerFunc(s.dashboardRouter)))
	root.Handle("/api/v1/seasons", authenticated(http.HandlerFunc(s.seasons)))
	root.Handle("/api/v1/seasons/", authenticated(http.HandlerFunc(s.seasonRouter)))
	root.Handle("/api/v1/predictions", authenticated(http.HandlerFunc(s.predictionsAPI)))
	root.Handle("/api/v1/predictions/", authenticated(http.HandlerFunc(s.predictionRouter)))
	root.Handle("/api/v1/seasons/{seasonId}/rounds/{round}/fixtures", authenticated(http.HandlerFunc(s.roundFixturesAPI)))
	root.Handle("/api/v1/matches/{matchId}/odds", authenticated(http.HandlerFunc(s.matchOddsRouter)))
	root.Handle("/api/v1/matches/", authenticated(http.HandlerFunc(s.matchRouter)))
	root.Handle("/api/v1/performance", authenticated(http.HandlerFunc(s.performanceAPI)))
	root.Handle("/api/v1/paper-testing", authenticated(http.HandlerFunc(s.paperTestingAPI)))
	root.Handle("/api/v1/operations", authenticated(http.HandlerFunc(s.operationsAPI)))
	root.Handle("/api/v1/settings", authenticated(http.HandlerFunc(s.settingsAPI)))
	root.HandleFunc("/api/v1/system/health", s.systemHealth)
	return s.securityHeaders(root)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.ensureAuthConfig().CookieSecure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "GET only")
			return
		}
		if s.InternalToken != "" {
			if !constantTimeEqual(r.Header.Get("X-Internal-Token"), s.InternalToken) {
				writeError(w, http.StatusUnauthorized, "internal access token required")
				return
			}
		} else if !isLoopbackRequest(r) {
			writeError(w, http.StatusForbidden, "operational API is restricted to loopback without an internal token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackRequest(r *http.Request) bool {
	if r.RemoteAddr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	var different byte
	for index := range left {
		different |= left[index] ^ right[index]
	}
	return different == 0
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Server) current(w http.ResponseWriter, r *http.Request) {
	if s.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "database is unavailable")
		return
	}
	var result struct {
		SeasonID string `json:"season_id"`
		Round    any    `json:"round"`
		Start    any    `json:"season_start"`
		End      any    `json:"season_end"`
	}
	var round sql.NullInt64
	var start, end sql.NullTime
	err := s.DB.QueryRowContext(r.Context(), `
	SELECT season_id, starts_at, ends_at FROM seasons
	WHERE season_id = COALESCE(
	    (SELECT season_id FROM seasons WHERE current_flag = true ORDER BY last_seen_at DESC LIMIT 1),
	    (SELECT season_id FROM live_feed_observations WHERE endpoint_family = 'live_season_fixtures' AND parse_status = 'accepted' ORDER BY observed_at DESC LIMIT 1)
	)`).Scan(&result.SeasonID, &start, &end)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"season_id": nil, "round": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.DB.QueryRowContext(r.Context(), `
SELECT round_no FROM round_states
WHERE season_id = $1
  AND state NOT IN ('settled', 'missed_cutoff')
  AND earliest_kickoff_at >= now()
ORDER BY earliest_kickoff_at LIMIT 1`, result.SeasonID).Scan(&round)
	if round.Valid {
		result.Round = round.Int64
	}
	if start.Valid {
		result.Start = start.Time.UTC()
	}
	if end.Valid {
		result.End = end.Time.UTC()
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) upcoming(w http.ResponseWriter, r *http.Request) {
	if s.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "database is unavailable")
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
SELECT f.season_id, f.round_no, f.match_id, f.scheduled_at, f.home_team_uid, ht.name,
       f.away_team_uid, at.name, f.status, f.result_home, f.result_away
FROM fixtures f JOIN teams ht ON ht.team_uid = f.home_team_uid JOIN teams at ON at.team_uid = f.away_team_uid
WHERE f.season_id = COALESCE(
    (SELECT season_id FROM seasons WHERE current_flag = true ORDER BY last_seen_at DESC LIMIT 1),
    (SELECT season_id FROM live_feed_observations WHERE endpoint_family = 'live_season_fixtures' AND parse_status = 'accepted' ORDER BY observed_at DESC LIMIT 1)
)
  AND f.scheduled_at >= now() - interval '2 hours'
  AND f.status NOT IN ('completed', 'canceled')
ORDER BY f.scheduled_at, f.match_id LIMIT 64`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var season, home, away, status string
		var round int
		var match, homeUID, awayUID int64
		var scheduled time.Time
		var homeScore, awayScore sql.NullInt64
		if err := rows.Scan(&season, &round, &match, &scheduled, &homeUID, &home, &awayUID, &away, &status, &homeScore, &awayScore); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		item := map[string]any{"season_id": season, "round": round, "match_id": match, "scheduled_at": scheduled.UTC(), "home_team_uid": homeUID, "home_team": home, "away_team_uid": awayUID, "away_team": away, "status": status}
		if homeScore.Valid {
			item["home_score"] = homeScore.Int64
		}
		if awayScore.Valid {
			item["away_score"] = awayScore.Int64
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) predictions(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.DB == nil {
			writeError(w, http.StatusServiceUnavailable, "database is unavailable")
			return
		}
		rows, err := s.DB.QueryContext(r.Context(), `
SELECT run.prediction_run_id::text, run.season_id, run.round_no, run.model_version, run.status,
       run.prediction_as_of, run.input_cutoff_at, pr.match_id, pr.home_probability,
       pr.draw_probability, pr.away_probability, pr.probability_payload, pr.odds_observed_at,
       pr.point_in_time_verified
FROM prediction_runs run JOIN prediction_revisions pr ON pr.prediction_run_id = run.prediction_run_id
WHERE run.status = $1 ORDER BY run.prediction_as_of DESC, pr.match_id`, status)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()
		items, scanErr := scanPredictionRows(rows)
		if scanErr != nil {
			writeError(w, http.StatusInternalServerError, scanErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func (s *Server) predictionHistory(w http.ResponseWriter, r *http.Request) {
	if s.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "database is unavailable")
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT prediction_run_id::text, season_id, round_no, run_type, status, model_version,
		       prediction_as_of, input_cutoff_at, generated_fixture_count, COALESCE(failure_reason, '')
FROM prediction_runs ORDER BY prediction_as_of DESC LIMIT 200`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, season, runType, status, model, reason string
		var round, count int
		var asOf, cutoff time.Time
		if err := rows.Scan(&id, &season, &round, &runType, &status, &model, &asOf, &cutoff, &count, &reason); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, map[string]any{"run_id": id, "season_id": season, "round": round, "run_type": runType, "status": status, "model_version": model, "prediction_as_of": asOf.UTC(), "cutoff_at": cutoff.UTC(), "generated_fixture_count": count, "failure_reason": reason})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) modelMetrics(w http.ResponseWriter, r *http.Request) {
	queryMetrics(w, r, s.DB, `
SELECT backtest_run_id::text, model_definition_id, scope_type, COALESCE(scope_season_id, ''),
       COALESCE(scope_round_no, 0), COALESCE(market_key, ''), metric_key, metric_value,
       sample_count, confidence_low, confidence_high
FROM evaluation_metrics ORDER BY backtest_run_id, model_definition_id, scope_type, metric_key`)
}

func (s *Server) forwardMetrics(w http.ResponseWriter, r *http.Request) {
	queryMetrics(w, r, s.DB, `
SELECT forward_result_id::text, prediction_run_id::text, season_id, round_no, market_key,
       metric_key, metric_value, sample_count, eligible_odds_count, odds_policy
FROM forward_paper_results ORDER BY created_at DESC LIMIT 500`)
}

func queryMetrics(w http.ResponseWriter, r *http.Request, db *sql.DB, query string) {
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, "database is unavailable")
		return
	}
	rows, err := db.QueryContext(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		item := make(map[string]any, len(columns))
		for index, column := range columns {
			item[column] = normalizeSQLValue(values[index])
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) feedHealth(w http.ResponseWriter, r *http.Request) {
	if s.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "database is unavailable")
		return
	}
	var latest, job time.Time
	var observations, failures int
	err := s.DB.QueryRowContext(r.Context(), `
SELECT COALESCE(max(observed_at), 'epoch'::timestamptz), count(*) FROM live_feed_observations`).Scan(&latest, &observations)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.DB.QueryRowContext(r.Context(), `SELECT COALESCE(max(started_at), 'epoch'::timestamptz), count(*) FILTER (WHERE status = 'failed') FROM job_execution_history`).Scan(&job, &failures)
	writeJSON(w, http.StatusOK, map[string]any{"latest_feed_observation": latest.UTC(), "observation_count": observations, "latest_job_start": job.UTC(), "failed_job_count": failures, "checked_at": s.now()})
}

func scanPredictionRows(rows *sql.Rows) ([]map[string]any, error) {
	items := make([]map[string]any, 0)
	for rows.Next() {
		var runID, season, model, status string
		var round, matchID int
		var asOf, cutoff time.Time
		var home, draw, away float64
		var payload []byte
		var oddsAt sql.NullTime
		var verified bool
		if err := rows.Scan(&runID, &season, &round, &model, &status, &asOf, &cutoff, &matchID, &home, &draw, &away, &payload, &oddsAt, &verified); err != nil {
			return nil, err
		}
		item := map[string]any{"run_id": runID, "season_id": season, "round": round, "model_version": model, "status": status, "prediction_as_of": asOf.UTC(), "cutoff_at": cutoff.UTC(), "match_id": matchID, "probability": map[string]float64{"home": home, "draw": draw, "away": away}, "point_in_time_verified": verified}
		var decoded any
		if len(payload) > 0 && json.Unmarshal(payload, &decoded) == nil {
			item["payload"] = decoded
		}
		if oddsAt.Valid {
			item["odds_observed_at"] = oddsAt.Time.UTC()
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeSQLValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case string:
		if parsed, err := strconv.ParseFloat(typed, 64); err == nil && strings.ContainsAny(typed, ".") {
			return parsed
		}
		return typed
	default:
		return value
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"code": strconv.Itoa(status), "message": message, "error": message})
}
