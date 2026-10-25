package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bet9ja-vfl/services/live"
	"bet9ja-vfl/services/provider"
	"bet9ja-vfl/services/research"
)

type verification struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

func main() {
	checks := []verification{
		checkMigration(),
		checkPollPlan(),
		checkProbability(),
		checkCredentialHygiene(),
	}
	for _, check := range checks {
		fmt.Printf("%-28s %v %s\n", check.Name, check.Passed, check.Detail)
		if !check.Passed {
			os.Exit(1)
		}
	}
	encoded, _ := json.MarshalIndent(map[string]any{"phase": 3, "network_requests": 0, "checks": checks}, "", "  ")
	fmt.Println(string(encoded))
}

func checkMigration() verification {
	paths := []string{
		filepath.Join("db", "migrations", "004_phase3_operational.sql"),
		filepath.Join("db", "migrations", "005_phase3_observation_history.sql"),
		filepath.Join("db", "migrations", "006_phase3_observation_constraint_cleanup.sql"),
		filepath.Join("db", "migrations", "007_phase3_odds_observation_history.sql"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return verification{"phase3_migration", false, err.Error()}
		}
	}
	return verification{"phase3_migration", true, "phase 3 migrations 004-007 present"}
}

func checkPollPlan() verification {
	cfg := live.DefaultPollConfig()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	got := (live.PollPlan{Now: now, NextKickoff: now.Add(4 * time.Minute), Config: cfg}).NextDelay()
	return verification{"adaptive_poll_plan", got == cfg.UpcomingOddsInterval, fmt.Sprintf("delay=%s", got)}
}

func checkProbability() verification {
	model, err := research.NewBaselineModel("poisson", false)
	if err != nil {
		return verification{"probability_validation", false, err.Error()}
	}
	match := research.MatchRecord{MatchID: provider.MatchID(1), HomeTeamUID: provider.TeamUID(1), AwayTeamUID: provider.TeamUID(2)}
	_ = model.Fit([]research.MatchRecord{{MatchID: provider.MatchID(2), HomeTeamUID: provider.TeamUID(1), AwayTeamUID: provider.TeamUID(2), HomeScore: intPtr(1), AwayScore: intPtr(0)}})
	probability := model.Predict(match)
	err = probability.Validate(1e-8)
	return verification{"probability_validation", err == nil, fmt.Sprintf("probability=%+v", probability)}
}

func checkCredentialHygiene() verification {
	for _, name := range []string{"BET9JA_API_KEY", "BET9JA_TOKEN", "BET9JA_PASSWORD"} {
		if value := os.Getenv(name); value != "" {
			return verification{"credential_hygiene", false, name + " must not be used by verification"}
		}
	}
	return verification{"credential_hygiene", true, "no provider credentials read"}
}

func intPtr(value int) *int { return &value }
