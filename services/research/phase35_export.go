package research

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"bet9ja-vfl/services/provider"
)

// Phase35Dataset is the versioned interchange contract between the canonical
// Go research loader and the Python LightGBM research service. It deliberately
// carries provenance alongside every row so retrospective odds cannot be
// mistaken for timestamp-faithful observations.
type Phase35Dataset struct {
	SchemaVersion        string              `json:"schema_version"`
	GeneratedAt          time.Time           `json:"generated_at"`
	DatasetFingerprint   string              `json:"dataset_fingerprint"`
	FeatureKey           string              `json:"feature_key"`
	FeatureVersion       string              `json:"feature_version"`
	OddsPolicy           string              `json:"odds_policy"`
	TrainingPolicy       string              `json:"training_policy"`
	Seasons              []Phase35Season     `json:"seasons"`
	Rows                 []Phase35Row        `json:"rows"`
	ExcludedSeasonIDs    []provider.SeasonID `json:"excluded_incomplete_season_ids"`
	BaselineModelVersion string              `json:"baseline_model_version"`
}

type Phase35Season struct {
	ID       provider.SeasonID `json:"season_id"`
	StartUTC time.Time         `json:"start_utc"`
	EndUTC   time.Time         `json:"end_utc"`
}

type Phase35Bookmaker struct {
	Home                float64   `json:"home"`
	Draw                float64   `json:"draw"`
	Away                float64   `json:"away"`
	Overround           float64   `json:"overround"`
	ObservedAt          time.Time `json:"observed_at"`
	SourceTimestampAt   time.Time `json:"source_timestamp_at"`
	PointInTimeVerified bool      `json:"point_in_time_verified"`
	ObservationKind     string    `json:"observation_kind"`
}

type Phase35Row struct {
	MatchID               provider.MatchID       `json:"match_id"`
	SeasonID              provider.SeasonID      `json:"season_id"`
	Round                 int                    `json:"round"`
	ScheduledAt           time.Time              `json:"scheduled_at"`
	CutoffAt              time.Time              `json:"cutoff_at"`
	HomeTeamUID           provider.TeamUID       `json:"home_team_uid"`
	AwayTeamUID           provider.TeamUID       `json:"away_team_uid"`
	Actual                OutcomeKey             `json:"actual"`
	Features              map[string]float64     `json:"features"`
	Bookmaker             *Phase35Bookmaker      `json:"bookmaker,omitempty"`
	BaselineProbabilities map[string]Probability `json:"baseline_probabilities"`
}

// ExportPhase35Dataset builds a complete-season, as-of feature export. The
// feature builder remains the single source of truth for cutoff filtering;
// this function only adds stable IDs and an explicitly labeled odds line.
func ExportPhase35Dataset(ctx context.Context, db *sql.DB, allowRetrospectiveOdds bool) (Phase35Dataset, error) {
	if db == nil {
		return Phase35Dataset{}, fmt.Errorf("database is nil")
	}
	raw, err := LoadDataset(ctx, db)
	if err != nil {
		return Phase35Dataset{}, err
	}
	dataset, excluded := raw.CompleteSeasons()
	featureConfig := DefaultFeatureConfig()
	features, err := BuildFeatureVectors(dataset, featureConfig)
	if err != nil {
		return Phase35Dataset{}, fmt.Errorf("build phase 3.5 features: %w", err)
	}

	backtestConfig := DefaultBacktestConfig()
	backtestConfig.AllowRetrospectiveOdds = allowRetrospectiveOdds
	backtestConfig.Models = BaselineModelNames()
	backtestConfig.BootstrapReplicates = 0
	baselineResult, err := RunBacktest(dataset, backtestConfig)
	if err != nil {
		return Phase35Dataset{}, fmt.Errorf("build baseline predictions: %w", err)
	}
	baselineByMatch := make(map[string]map[string]Probability)
	for _, prediction := range baselineResult.Predictions {
		key := phase35MatchKey(prediction.SeasonID, prediction.MatchID)
		byModel := baselineByMatch[key]
		if byModel == nil {
			byModel = make(map[string]Probability)
			baselineByMatch[key] = byModel
		}
		byModel[prediction.ModelName] = prediction.Probability
	}

	matchByID := make(map[string]MatchRecord, len(dataset.Matches))
	for _, match := range dataset.Matches {
		matchByID[phase35MatchKey(match.SeasonID, match.MatchID)] = match
	}
	rows := make([]Phase35Row, 0, len(features))
	for _, vector := range features {
		match, ok := matchByID[phase35MatchKey(vector.SeasonID, vector.MatchID)]
		if !ok {
			return Phase35Dataset{}, fmt.Errorf("feature %s/%s has no match row", vector.SeasonID, vector.MatchID)
		}
		actual, ok := match.Outcome()
		if !ok {
			return Phase35Dataset{}, fmt.Errorf("feature %s/%s has no final outcome", vector.SeasonID, vector.MatchID)
		}
		values := make(map[string]float64, len(vector.Values)+3)
		for key, value := range vector.Values {
			values[key] = value
		}
		values["home_team_uid"] = float64(match.HomeTeamUID)
		values["away_team_uid"] = float64(match.AwayTeamUID)
		line, hasOdds := match.OddsLine(allowRetrospectiveOdds)
		var bookmaker *Phase35Bookmaker
		if hasOdds {
			implied := line.ImpliedProbabilities()
			bookmaker = &Phase35Bookmaker{
				Home:                implied.Home,
				Draw:                implied.Draw,
				Away:                implied.Away,
				Overround:           line.Overround,
				ObservedAt:          line.HomeQuote.ObservedAt.UTC(),
				SourceTimestampAt:   line.HomeQuote.SourceTimestampAt.UTC(),
				PointInTimeVerified: line.PointInTimeVerified,
				ObservationKind:     line.ObservationKind,
			}
		}
		baseline := baselineByMatch[phase35MatchKey(match.SeasonID, match.MatchID)]
		if baseline == nil {
			baseline = map[string]Probability{}
		}
		rows = append(rows, Phase35Row{
			MatchID:               match.MatchID,
			SeasonID:              match.SeasonID,
			Round:                 match.Round,
			ScheduledAt:           match.ScheduledAt.UTC(),
			CutoffAt:              vector.CutoffAt.UTC(),
			HomeTeamUID:           match.HomeTeamUID,
			AwayTeamUID:           match.AwayTeamUID,
			Actual:                actual,
			Features:              values,
			Bookmaker:             bookmaker,
			BaselineProbabilities: baseline,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].ScheduledAt.Equal(rows[j].ScheduledAt) {
			return rows[i].ScheduledAt.Before(rows[j].ScheduledAt)
		}
		return rows[i].MatchID < rows[j].MatchID
	})
	seasons := make([]Phase35Season, 0, len(dataset.Seasons))
	for _, season := range dataset.Seasons {
		seasons = append(seasons, Phase35Season{ID: season.ID, StartUTC: season.StartUTC.UTC(), EndUTC: season.EndUTC.UTC()})
	}
	sort.SliceStable(seasons, func(i, j int) bool {
		if !seasons[i].StartUTC.Equal(seasons[j].StartUTC) {
			return seasons[i].StartUTC.Before(seasons[j].StartUTC)
		}
		return seasons[i].ID < seasons[j].ID
	})
	oddsPolicy := "verified_point_in_time_only"
	if allowRetrospectiveOdds {
		oddsPolicy = "retrospective_unverified_availability_explicit_opt_in"
	}
	return Phase35Dataset{
		SchemaVersion:        "phase35.dataset.v1",
		GeneratedAt:          time.Now().UTC(),
		DatasetFingerprint:   dataset.Fingerprint(),
		FeatureKey:           featureConfig.FeatureKey,
		FeatureVersion:       featureConfig.Version,
		OddsPolicy:           oddsPolicy,
		TrainingPolicy:       "season_chronological_train_validation_final_test; feature results strictly before round cutoff",
		Seasons:              seasons,
		Rows:                 rows,
		ExcludedSeasonIDs:    excluded,
		BaselineModelVersion: "phase2.v1",
	}, nil
}

func phase35MatchKey(seasonID provider.SeasonID, matchID provider.MatchID) string {
	return seasonID.String() + "/" + matchID.String()
}

// WritePhase35Dataset is kept separate from database loading so callers can
// test the serialized interchange contract without a provider request.
func WritePhase35Dataset(path string, dataset Phase35Dataset) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create phase 3.5 dataset %s: %w", path, err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(dataset); err != nil {
		return fmt.Errorf("encode phase 3.5 dataset: %w", err)
	}
	return nil
}
