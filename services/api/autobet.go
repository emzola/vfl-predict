package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bet9ja-vfl/services/autobet"
)

func (s *Server) autoBettingAPI(w http.ResponseWriter, r *http.Request) {
	session, ok := authSessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	service := autobet.NewService(s.DB)
	service.Now = s.now
	switch r.Method {
	case http.MethodGet:
		state, err := service.LoadDashboard(r.Context())
		if err != nil {
			writeServerError(w, "could not load auto betting")
			return
		}
		writeJSON(w, http.StatusOK, state)
	case http.MethodPatch:
		if !s.csrfRequestIsValid(r, session) {
			writeError(w, http.StatusForbidden, "csrf validation failed")
			return
		}
		var input struct {
			Enabled   bool    `json:"enabled"`
			BaseStake float64 `json:"base_stake"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid auto betting settings")
			return
		}
		if input.BaseStake <= 0 {
			writeError(w, http.StatusBadRequest, "base stake must be positive")
			return
		}
		if err := service.UpdateSettings(r.Context(), session.AccountID, input.Enabled, input.BaseStake); err != nil {
			writeServerError(w, "could not update auto betting settings")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) autoBettingInternalTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	service := autobet.NewService(s.DB)
	service.Now = s.now
	task, err := service.NextTask(r.Context())
	if err != nil {
		writeServerError(w, "could not load auto betting task")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) autoBettingInternalRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input struct {
		State           string `json:"state"`
		WorkerID        string `json:"worker_id"`
		CurrentSeasonID string `json:"current_season_id"`
		CurrentRound    int    `json:"current_round"`
		CurrentMatchID  int64  `json:"current_match_id"`
		CurrentURL      string `json:"current_url"`
		LastHeartbeatAt string `json:"last_heartbeat_at"`
		LastLoginAt     string `json:"last_login_at"`
		LastError       string `json:"last_error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime update")
		return
	}
	update := autobet.RuntimeUpdate{
		State:           strings.TrimSpace(input.State),
		WorkerID:        input.WorkerID,
		CurrentSeasonID: input.CurrentSeasonID,
		CurrentRound:    input.CurrentRound,
		CurrentMatchID:  input.CurrentMatchID,
		CurrentURL:      input.CurrentURL,
		LastError:       input.LastError,
	}
	if strings.TrimSpace(input.LastHeartbeatAt) != "" {
		value, err := parseRFC3339Value(input.LastHeartbeatAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid heartbeat time")
			return
		}
		update.LastHeartbeatAt = &value
	}
	if strings.TrimSpace(input.LastLoginAt) != "" {
		value, err := parseRFC3339Value(input.LastLoginAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid login time")
			return
		}
		update.LastLoginAt = &value
	}
	service := autobet.NewService(s.DB)
	service.Now = s.now
	if err := service.UpdateRuntime(r.Context(), update); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) autoBettingInternalSkip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	seasonID, round, ok := autoBettingPathParams(w, r)
	if !ok {
		return
	}
	var input struct {
		Reason       string   `json:"reason"`
		ObservedOdds *float64 `json:"observed_odds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid skip payload")
		return
	}
	service := autobet.NewService(s.DB)
	service.Now = s.now
	if err := service.RecordSkip(r.Context(), seasonID, round, input.Reason, input.ObservedOdds); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (s *Server) autoBettingInternalPlaced(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	seasonID, round, ok := autoBettingPathParams(w, r)
	if !ok {
		return
	}
	var input struct {
		QuotedOdds   float64 `json:"quoted_odds"`
		BetReference string  `json:"bet_reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid placed payload")
		return
	}
	if input.QuotedOdds <= 0 {
		writeError(w, http.StatusBadRequest, "quoted odds must be positive")
		return
	}
	service := autobet.NewService(s.DB)
	service.Now = s.now
	if err := service.RecordPlaced(r.Context(), seasonID, round, input.QuotedOdds, input.BetReference); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func autoBettingPathParams(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	seasonID := r.PathValue("seasonId")
	round, err := strconv.Atoi(r.PathValue("round"))
	if !isSeasonID(seasonID) || err != nil || round < 1 || round > 30 {
		writeError(w, http.StatusBadRequest, "invalid season or round")
		return "", 0, false
	}
	return seasonID, round, true
}

func parseRFC3339Value(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
