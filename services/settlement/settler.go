package settlement

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type Report struct {
	RunID             string  `json:"run_id,omitempty"`
	SettledCount      int     `json:"settled_count"`
	SkippedUnfinished int     `json:"skipped_unfinished"`
	CorrectionCount   int     `json:"correction_count"`
	LogLoss           float64 `json:"log_loss"`
	Brier             float64 `json:"brier"`
	Accuracy          float64 `json:"accuracy"`
	Stake             float64 `json:"hypothetical_stake"`
	Return            float64 `json:"hypothetical_return"`
	ROI               float64 `json:"hypothetical_roi"`
	OddsEligibleCount int     `json:"point_in_time_odds_count"`
}

type Settler struct {
	DB  *sql.DB
	Now func() time.Time
}

func NewSettler(db *sql.DB) *Settler { return &Settler{DB: db, Now: time.Now} }

func (s *Settler) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

type pendingPrediction struct {
	RevisionID string
	RunID      string
	SeasonID   string
	Round      int
	MatchID    int64
	RunType    string
	Status     string
	Cutoff     time.Time
	Scheduled  time.Time
	Home       float64
	Draw       float64
	Away       float64
	HomeScore  sql.NullInt64
	AwayScore  sql.NullInt64
	HomeName   string
	AwayName   string
}

func (s *Settler) Settle(ctx context.Context, runID string) (Report, error) {
	if s == nil || s.DB == nil {
		return Report{}, errors.New("settler database is nil")
	}
	var query string
	args := []any{"frozen", "settled"}
	if strings.TrimSpace(runID) != "" {
		query = `
SELECT pr.prediction_revision_id::text, pr.prediction_run_id::text, pr.season_id, pr.round_no,
       pr.match_id, run.run_type, pr.status, pr.cutoff_at, f.scheduled_at,
       pr.home_probability::double precision, pr.draw_probability::double precision,
       pr.away_probability::double precision, f.result_home, f.result_away,
       ht.name, at.name
FROM prediction_revisions pr
JOIN prediction_runs run ON run.prediction_run_id = pr.prediction_run_id
JOIN fixtures f ON f.match_id = pr.match_id
JOIN teams ht ON ht.team_uid = f.home_team_uid
JOIN teams at ON at.team_uid = f.away_team_uid
WHERE pr.prediction_run_id = $1::uuid AND run.status IN ($2, $3)
ORDER BY pr.match_id`
		args = []any{runID, "frozen", "settled"}
	} else {
		query = `
SELECT pr.prediction_revision_id::text, pr.prediction_run_id::text, pr.season_id, pr.round_no,
       pr.match_id, run.run_type, pr.status, pr.cutoff_at, f.scheduled_at,
       pr.home_probability::double precision, pr.draw_probability::double precision,
       pr.away_probability::double precision, f.result_home, f.result_away,
       ht.name, at.name
FROM prediction_revisions pr
JOIN prediction_runs run ON run.prediction_run_id = pr.prediction_run_id
JOIN fixtures f ON f.match_id = pr.match_id
JOIN teams ht ON ht.team_uid = f.home_team_uid
JOIN teams at ON at.team_uid = f.away_team_uid
WHERE run.status IN ($1, $2)
ORDER BY pr.prediction_run_id, pr.match_id`
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return Report{}, fmt.Errorf("load pending predictions: %w", err)
	}
	defer rows.Close()
	report := Report{RunID: runID}
	var all []pendingPrediction
	for rows.Next() {
		var prediction pendingPrediction
		if err := rows.Scan(&prediction.RevisionID, &prediction.RunID, &prediction.SeasonID, &prediction.Round,
			&prediction.MatchID, &prediction.RunType, &prediction.Status, &prediction.Cutoff, &prediction.Scheduled,
			&prediction.Home, &prediction.Draw, &prediction.Away, &prediction.HomeScore, &prediction.AwayScore,
			&prediction.HomeName, &prediction.AwayName); err != nil {
			return report, err
		}
		if !prediction.HomeScore.Valid || !prediction.AwayScore.Valid {
			report.SkippedUnfinished++
			continue
		}
		all = append(all, prediction)
	}
	if err := rows.Err(); err != nil {
		return report, err
	}
	for _, prediction := range all {
		if err := s.settleOne(ctx, prediction, &report); err != nil {
			return report, fmt.Errorf("settle revision %s: %w", prediction.RevisionID, err)
		}
	}
	if report.SettledCount > 0 {
		report.LogLoss /= float64(report.SettledCount)
		report.Brier /= float64(report.SettledCount)
		report.Accuracy /= float64(report.SettledCount)
		if report.Stake > 0 {
			report.ROI = (report.Return - report.Stake) / report.Stake
		}
	}
	return report, nil
}

func (s *Settler) settleOne(ctx context.Context, prediction pendingPrediction, report *Report) error {
	homeScore, awayScore := int(prediction.HomeScore.Int64), int(prediction.AwayScore.Int64)
	actual, logLoss, brier, correct := ComputePredictionMetrics(prediction.Home, prediction.Draw, prediction.Away, homeScore, awayScore)
	p := map[string]float64{"home": prediction.Home, "draw": prediction.Draw, "away": prediction.Away}
	predicted := argmax(p)
	odds, oddsOK, oddsObserved := s.findEligibleOdds(ctx, prediction)
	stake, returnValue := 0.0, 0.0
	if oddsOK {
		stake = 1
		returnValue = odds[predicted]
		if !correct {
			returnValue = 0
		}
		report.OddsEligibleCount++
	}
	correctionVersion, exists, previousHome, previousAway, err := s.latestSettlement(ctx, prediction.RevisionID)
	if err != nil {
		return err
	}
	if exists && previousHome == homeScore && previousAway == awayScore {
		return nil
	}
	if !exists {
		correctionVersion = 1
	} else {
		correctionVersion++
		report.CorrectionCount++
	}
	track := "forward_observed"
	if prediction.RunType == "backtest" || prediction.RunType == "historical_retrospective" {
		track = "historical_retrospective"
	}
	provenance, _ := json.Marshal(map[string]any{
		"settlement_track":           track,
		"prediction_run_id":          prediction.RunID,
		"odds_observed_at":           nullableTime(oddsObserved),
		"odds_eligible":              oddsOK,
		"post_kickoff_odds_excluded": true,
	})
	_, err = s.DB.ExecContext(ctx, `
INSERT INTO prediction_settlements (
    prediction_revision_id, correction_version, settlement_track, final_home_score,
    final_away_score, actual_outcome, log_loss, brier_score, correct, quoted_decimal_odds,
    hypothetical_stake, hypothetical_return, hypothetical_profit, evaluated_at,
    result_observed_at, provenance
) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, 0), NULLIF($11, 0), NULLIF($12, 0), $13,
          $14, $14, $15::jsonb)
ON CONFLICT (prediction_revision_id, correction_version) DO NOTHING`, prediction.RevisionID,
		correctionVersion, track, homeScore, awayScore, actual, logLoss, brier, correct,
		oddsValue(odds, predicted, oddsOK), stake, returnValue, returnValue-stake, s.now(),
		string(provenance))
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
		UPDATE prediction_revisions SET status = 'settled'
		WHERE prediction_revision_id = $1::uuid`, prediction.RevisionID)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
UPDATE prediction_runs SET status = 'settled', updated_at = now()
WHERE prediction_run_id = $1::uuid AND status = 'frozen'`, prediction.RunID)
	if err != nil {
		return err
	}
	if err := s.persistMetrics(ctx, prediction, logLoss, brier, correct, stake, returnValue); err != nil {
		return err
	}
	report.SettledCount++
	report.LogLoss += logLoss
	report.Brier += brier
	if correct {
		report.Accuracy++
	}
	report.Stake += stake
	report.Return += returnValue
	return nil
}

// ComputePredictionMetrics is the deterministic forward-settlement arithmetic
// shared by the database settler and unit tests.
func ComputePredictionMetrics(home, draw, away float64, homeScore, awayScore int) (string, float64, float64, bool) {
	actual := "draw"
	if homeScore > awayScore {
		actual = "home"
	} else if homeScore < awayScore {
		actual = "away"
	}
	probabilities := map[string]float64{"home": home, "draw": draw, "away": away}
	logLoss := -math.Log(math.Max(probabilities[actual], 1e-15))
	brier := math.Pow(home-boolFloat(actual == "home"), 2) +
		math.Pow(draw-boolFloat(actual == "draw"), 2) +
		math.Pow(away-boolFloat(actual == "away"), 2)
	return actual, logLoss, brier, argmax(probabilities) == actual
}

func (s *Settler) latestSettlement(ctx context.Context, revisionID string) (int, bool, int, int, error) {
	var version, home, away int
	err := s.DB.QueryRowContext(ctx, `
SELECT correction_version, final_home_score, final_away_score
FROM prediction_settlements WHERE prediction_revision_id = $1::uuid
ORDER BY correction_version DESC LIMIT 1`, revisionID).Scan(&version, &home, &away)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, 0, 0, nil
	}
	return version, err == nil, home, away, err
}

func (s *Settler) persistMetrics(ctx context.Context, prediction pendingPrediction, logLoss, brier float64, correct bool, stake, returnValue float64) error {
	var aggregate struct {
		LogLoss, Brier, Accuracy, Stake, Return, Profit float64
		Count, OddsCount                                int
	}
	err := s.DB.QueryRowContext(ctx, `
WITH latest AS (
    SELECT DISTINCT ON (prediction_revision_id)
           log_loss, brier_score, correct, hypothetical_stake,
           hypothetical_return, hypothetical_profit, quoted_decimal_odds
    FROM prediction_settlements
    WHERE prediction_revision_id IN (SELECT prediction_revision_id FROM prediction_revisions WHERE prediction_run_id = $1::uuid)
    ORDER BY prediction_revision_id, correction_version DESC
)
SELECT COALESCE(sum(log_loss), 0), COALESCE(sum(brier_score), 0),
       COALESCE(sum(correct::integer), 0), COALESCE(sum(hypothetical_stake), 0),
       COALESCE(sum(hypothetical_return), 0), COALESCE(sum(hypothetical_profit), 0),
       count(*)::integer, count(*) FILTER (WHERE quoted_decimal_odds IS NOT NULL)::integer
FROM latest`, prediction.RunID).Scan(&aggregate.LogLoss, &aggregate.Brier, &aggregate.Accuracy,
		&aggregate.Stake, &aggregate.Return, &aggregate.Profit, &aggregate.Count, &aggregate.OddsCount)
	if err != nil {
		return err
	}
	metrics := map[string]float64{
		"log_loss": aggregate.LogLoss, "brier": aggregate.Brier, "accuracy": aggregate.Accuracy,
		"stake": aggregate.Stake, "return": aggregate.Return, "profit": aggregate.Profit,
	}
	for key, value := range metrics {
		_, err := s.DB.ExecContext(ctx, `
INSERT INTO forward_paper_results (
    prediction_run_id, season_id, round_no, market_key, metric_key, metric_value,
    sample_count, eligible_odds_count, odds_policy
) VALUES ($1::uuid, $2, $3, '1x2', $4, $5, $6, $7, $8)
		ON CONFLICT (prediction_run_id, market_key, metric_key) DO UPDATE SET
    metric_value = EXCLUDED.metric_value,
    sample_count = EXCLUDED.sample_count,
    eligible_odds_count = EXCLUDED.eligible_odds_count,
    odds_policy = EXCLUDED.odds_policy`,
			prediction.RunID, prediction.SeasonID, prediction.Round, key, value, aggregate.Count, aggregate.OddsCount, "verified_fetched_at_before_cutoff")
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Settler) findEligibleOdds(ctx context.Context, prediction pendingPrediction) (map[string]float64, bool, time.Time) {
	rows, err := s.DB.QueryContext(ctx, `
WITH eligible AS (
    SELECT DISTINCT ON (o.odds_snapshot_id)
           o.odds_snapshot_id, o.outcome_id, o.outcome_name,
           o.decimal_odds::double precision AS decimal_odds,
           COALESCE(oo.observed_at, o.last_observed_at) AS observed_at,
           COALESCE(oo.source_timestamp_at, o.source_timestamp_at) AS source_timestamp_at
    FROM odds_snapshots o
    LEFT JOIN odds_snapshot_observations oo ON oo.odds_snapshot_id = o.odds_snapshot_id
    WHERE o.match_id = $1 AND o.provider_market_id = 1 AND o.specifiers = '' AND o.odds_valid = true
      AND COALESCE(oo.point_in_time_verified, o.point_in_time_verified, false) = true
      AND COALESCE(oo.observed_at, o.last_observed_at) <= $2
    ORDER BY o.odds_snapshot_id, COALESCE(oo.observed_at, o.last_observed_at) DESC,
             COALESCE(oo.source_timestamp_at, o.source_timestamp_at) DESC
)
SELECT outcome_id, lower(outcome_name), decimal_odds, observed_at
FROM eligible
ORDER BY observed_at DESC, source_timestamp_at DESC`, prediction.MatchID, prediction.Cutoff)
	if err != nil {
		return nil, false, time.Time{}
	}
	defer rows.Close()
	odds := make(map[string]float64)
	var observed time.Time
	for rows.Next() {
		var id, name string
		var value float64
		var seen time.Time
		if rows.Scan(&id, &name, &value, &seen) != nil {
			return nil, false, time.Time{}
		}
		key := classify(id, name, prediction.HomeName, prediction.AwayName)
		if key != "" {
			if _, exists := odds[key]; !exists {
				odds[key] = value
				if observed.IsZero() || seen.After(observed) {
					observed = seen
				}
			}
		}
	}
	return odds, len(odds) == 3, observed
}

func classify(id, name, home, away string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	name = strings.ToLower(strings.TrimSpace(name))
	if id == "1" || id == "h" || strings.Contains(name, "home") || strings.Contains(name, strings.ToLower(home)) {
		return "home"
	}
	if id == "2" || id == "a" || strings.Contains(name, "away") || strings.Contains(name, strings.ToLower(away)) {
		return "away"
	}
	if id == "3" || id == "x" || id == "d" || id == "draw" || strings.Contains(name, "draw") || strings.Contains(name, "tie") {
		return "draw"
	}
	return ""
}

func argmax(values map[string]float64) string {
	best := "home"
	for _, key := range []string{"draw", "away"} {
		if values[key] > values[best] {
			best = key
		}
	}
	return best
}

func oddsValue(values map[string]float64, key string, ok bool) any {
	if !ok || values[key] <= 0 {
		return nil
	}
	return values[key]
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
