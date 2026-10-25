package research

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"bet9ja-vfl/services/provider"
)

// OutcomeKey is the canonical 1X2 outcome identifier used by the research
// package. It is intentionally independent of provider outcome IDs.
type OutcomeKey string

const (
	OutcomeHome OutcomeKey = "home"
	OutcomeDraw OutcomeKey = "draw"
	OutcomeAway OutcomeKey = "away"
)

type SeasonRecord struct {
	ID       provider.SeasonID `json:"id"`
	StartUTC time.Time         `json:"start_utc"`
	EndUTC   time.Time         `json:"end_utc"`
}

// OddsQuote retains the timestamps and provenance needed to keep retrospective
// odds separate from verified point-in-time observations. Odds is a float only
// after provider.Decimal validation; persistence remains decimal/numeric in
// PostgreSQL.
type OddsQuote struct {
	MarketID            int64     `json:"market_id"`
	MarketName          string    `json:"market_name"`
	Specifiers          string    `json:"specifiers"`
	OutcomeID           string    `json:"outcome_id"`
	OutcomeName         string    `json:"outcome_name"`
	OutcomeAbbreviation string    `json:"outcome_abbreviation"`
	Odds                float64   `json:"odds"`
	RawOdds             string    `json:"raw_odds"`
	SourceTimestampAt   time.Time `json:"source_timestamp_at"`
	ObservedAt          time.Time `json:"observed_at"`
	PointInTimeVerified bool      `json:"point_in_time_verified"`
	ObservationKind     string    `json:"observation_kind"`
	Active              bool      `json:"active"`
}

type MatchRecord struct {
	MatchID      provider.MatchID  `json:"match_id"`
	SeasonID     provider.SeasonID `json:"season_id"`
	Round        int               `json:"round"`
	ScheduledAt  time.Time         `json:"scheduled_at"`
	HomeTeamUID  provider.TeamUID  `json:"home_team_uid"`
	AwayTeamUID  provider.TeamUID  `json:"away_team_uid"`
	HomeTeamName string            `json:"home_team_name"`
	AwayTeamName string            `json:"away_team_name"`
	HomeScore    *int              `json:"home_score"`
	AwayScore    *int              `json:"away_score"`
	Canceled     bool              `json:"canceled"`
	Postponed    bool              `json:"postponed"`
	Walkover     bool              `json:"walkover"`
	Odds         []OddsQuote       `json:"odds"`
}

func (m MatchRecord) Final() bool {
	return m.HomeScore != nil && m.AwayScore != nil && !m.Canceled && !m.Postponed && !m.Walkover &&
		*m.HomeScore >= 0 && *m.AwayScore >= 0
}

func (m MatchRecord) Outcome() (OutcomeKey, bool) {
	if !m.Final() {
		return "", false
	}
	if *m.HomeScore > *m.AwayScore {
		return OutcomeHome, true
	}
	if *m.HomeScore < *m.AwayScore {
		return OutcomeAway, true
	}
	return OutcomeDraw, true
}

func (m MatchRecord) HasTeam(uid provider.TeamUID) bool {
	return m.HomeTeamUID == uid || m.AwayTeamUID == uid
}

func (m MatchRecord) OddsLine(allowUnverified bool) (OddsLine, bool) {
	return FindOneXTwoOdds(m, allowUnverified)
}

type Dataset struct {
	Matches []MatchRecord
	Seasons []SeasonRecord
}

func NewDataset(matches []MatchRecord, seasons []SeasonRecord) Dataset {
	copyMatches := append([]MatchRecord(nil), matches...)
	copySeasons := append([]SeasonRecord(nil), seasons...)
	sort.SliceStable(copyMatches, func(i, j int) bool {
		if !copyMatches[i].ScheduledAt.Equal(copyMatches[j].ScheduledAt) {
			return copyMatches[i].ScheduledAt.Before(copyMatches[j].ScheduledAt)
		}
		return copyMatches[i].MatchID < copyMatches[j].MatchID
	})
	sort.SliceStable(copySeasons, func(i, j int) bool {
		if !copySeasons[i].StartUTC.Equal(copySeasons[j].StartUTC) {
			return copySeasons[i].StartUTC.Before(copySeasons[j].StartUTC)
		}
		return copySeasons[i].ID < copySeasons[j].ID
	})
	return Dataset{Matches: copyMatches, Seasons: copySeasons}
}

func (d Dataset) SortedMatches() []MatchRecord {
	return NewDataset(d.Matches, nil).Matches
}

func (d Dataset) SeasonMap() map[provider.SeasonID]SeasonRecord {
	result := make(map[provider.SeasonID]SeasonRecord, len(d.Seasons))
	for _, season := range d.Seasons {
		result[season.ID] = season
	}
	for _, match := range d.Matches {
		if _, exists := result[match.SeasonID]; exists {
			continue
		}
		result[match.SeasonID] = SeasonRecord{ID: match.SeasonID, StartUTC: match.ScheduledAt, EndUTC: match.ScheduledAt}
	}
	return result
}

// CompleteSeasons returns only seasons that satisfy the VFL structural policy
// used for historical evaluation: 30 rounds, eight fixtures per round, and a
// final score for every fixture. Incomplete rows remain in the ingestion
// database and coverage reports but are not silently mixed into model fitting.
func (d Dataset) CompleteSeasons() (Dataset, []provider.SeasonID) {
	type seasonCoverage struct {
		fixturesByRound map[int]int
		finals          int
	}
	coverage := make(map[provider.SeasonID]*seasonCoverage)
	for _, match := range d.Matches {
		value := coverage[match.SeasonID]
		if value == nil {
			value = &seasonCoverage{fixturesByRound: make(map[int]int)}
			coverage[match.SeasonID] = value
		}
		value.fixturesByRound[match.Round]++
		if match.Final() {
			value.finals++
		}
	}
	complete := make(map[provider.SeasonID]struct{})
	incomplete := make([]provider.SeasonID, 0)
	for seasonID, value := range coverage {
		if len(value.fixturesByRound) == 30 && value.finals == 240 {
			valid := true
			for round := 1; round <= 30; round++ {
				if value.fixturesByRound[round] != 8 {
					valid = false
					break
				}
			}
			if valid {
				complete[seasonID] = struct{}{}
				continue
			}
		}
		incomplete = append(incomplete, seasonID)
	}
	matches := make([]MatchRecord, 0)
	for _, match := range d.Matches {
		if _, exists := complete[match.SeasonID]; exists {
			matches = append(matches, match)
		}
	}
	seasons := make([]SeasonRecord, 0)
	for _, season := range d.Seasons {
		if _, exists := complete[season.ID]; exists {
			seasons = append(seasons, season)
		}
	}
	sort.Slice(incomplete, func(i, j int) bool { return incomplete[i] < incomplete[j] })
	return NewDataset(matches, seasons), incomplete
}

// Fingerprint is deterministic over the normalized dataset and is used to
// tie feature, model, prediction, and evaluation artifacts to their inputs.
func (d Dataset) Fingerprint() string {
	type fingerprintOdds struct {
		MarketID            int64
		Specifiers          string
		OutcomeID           string
		Odds                string
		SourceTimestampAt   string
		ObservedAt          string
		PointInTimeVerified bool
		ObservationKind     string
	}
	type fingerprintMatch struct {
		MatchID     string
		SeasonID    string
		Round       int
		ScheduledAt string
		HomeTeamUID string
		AwayTeamUID string
		HomeScore   *int
		AwayScore   *int
		Canceled    bool
		Postponed   bool
		Walkover    bool
		Odds        []fingerprintOdds
	}
	type fingerprintSeason struct {
		ID       string
		StartUTC string
		EndUTC   string
	}
	value := struct {
		Matches []fingerprintMatch
		Seasons []fingerprintSeason
	}{
		Matches: make([]fingerprintMatch, 0, len(d.Matches)),
		Seasons: make([]fingerprintSeason, 0, len(d.Seasons)),
	}
	for _, match := range NewDataset(d.Matches, d.Seasons).Matches {
		odds := make([]fingerprintOdds, 0, len(match.Odds))
		for _, quote := range match.Odds {
			odds = append(odds, fingerprintOdds{
				MarketID:            quote.MarketID,
				Specifiers:          quote.Specifiers,
				OutcomeID:           quote.OutcomeID,
				Odds:                quote.RawOdds,
				SourceTimestampAt:   quote.SourceTimestampAt.UTC().Format(time.RFC3339Nano),
				ObservedAt:          quote.ObservedAt.UTC().Format(time.RFC3339Nano),
				PointInTimeVerified: quote.PointInTimeVerified,
				ObservationKind:     quote.ObservationKind,
			})
		}
		sort.SliceStable(odds, func(i, j int) bool {
			if odds[i].MarketID != odds[j].MarketID {
				return odds[i].MarketID < odds[j].MarketID
			}
			if odds[i].Specifiers != odds[j].Specifiers {
				return odds[i].Specifiers < odds[j].Specifiers
			}
			return odds[i].OutcomeID < odds[j].OutcomeID
		})
		value.Matches = append(value.Matches, fingerprintMatch{
			MatchID:     match.MatchID.String(),
			SeasonID:    match.SeasonID.String(),
			Round:       match.Round,
			ScheduledAt: match.ScheduledAt.UTC().Format(time.RFC3339Nano),
			HomeTeamUID: match.HomeTeamUID.String(),
			AwayTeamUID: match.AwayTeamUID.String(),
			HomeScore:   match.HomeScore,
			AwayScore:   match.AwayScore,
			Canceled:    match.Canceled,
			Postponed:   match.Postponed,
			Walkover:    match.Walkover,
			Odds:        odds,
		})
	}
	for _, season := range NewDataset(d.Matches, d.Seasons).Seasons {
		value.Seasons = append(value.Seasons, fingerprintSeason{
			ID:       season.ID.String(),
			StartUTC: season.StartUTC.UTC().Format(time.RFC3339Nano),
			EndUTC:   season.EndUTC.UTC().Format(time.RFC3339Nano),
		})
	}
	bytes, _ := json.Marshal(value)
	digest := sha256.Sum256(bytes)
	return hex.EncodeToString(digest[:])
}

type Probability struct {
	Home float64 `json:"home"`
	Draw float64 `json:"draw"`
	Away float64 `json:"away"`
}

func (p Probability) Values() [3]float64 { return [3]float64{p.Home, p.Draw, p.Away} }

func (p Probability) At(outcome OutcomeKey) float64 {
	switch outcome {
	case OutcomeHome:
		return p.Home
	case OutcomeDraw:
		return p.Draw
	case OutcomeAway:
		return p.Away
	default:
		return 0
	}
}

func (p Probability) Validate(tolerance float64) error {
	if tolerance <= 0 {
		tolerance = 1e-9
	}
	for _, value := range p.Values() {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < -tolerance || value > 1+tolerance {
			return fmt.Errorf("invalid probability value %v", value)
		}
	}
	if math.Abs(p.Home+p.Draw+p.Away-1) > tolerance {
		return fmt.Errorf("probabilities sum to %.12f, want 1", p.Home+p.Draw+p.Away)
	}
	return nil
}

func NormalizeProbability(home, draw, away float64) Probability {
	values := []float64{home, draw, away}
	for index, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			values[index] = 0
		}
	}
	sum := values[0] + values[1] + values[2]
	if sum <= 0 {
		return Probability{Home: 1.0 / 3, Draw: 1.0 / 3, Away: 1.0 / 3}
	}
	return Probability{Home: values[0] / sum, Draw: values[1] / sum, Away: values[2] / sum}
}

// OddsLine is the normalized, mutually exclusive 1X2 market used by the
// bookmaker baseline and the research-only betting analysis.
type OddsLine struct {
	Home                float64   `json:"home"`
	Draw                float64   `json:"draw"`
	Away                float64   `json:"away"`
	HomeQuote           OddsQuote `json:"home_quote"`
	DrawQuote           OddsQuote `json:"draw_quote"`
	AwayQuote           OddsQuote `json:"away_quote"`
	Overround           float64   `json:"overround"`
	PointInTimeVerified bool      `json:"point_in_time_verified"`
	ObservationKind     string    `json:"observation_kind"`
}

func (line OddsLine) ImpliedProbabilities() Probability {
	return NormalizeProbability(1/line.Home, 1/line.Draw, 1/line.Away)
}

func (line OddsLine) AllQuotesVerified() bool {
	return line.HomeQuote.PointInTimeVerified && line.DrawQuote.PointInTimeVerified && line.AwayQuote.PointInTimeVerified
}

type FeatureConfig struct {
	FeatureKey        string
	Version           string
	RoundSafetyBuffer time.Duration
	// IncludeUnverifiedOdds is retained as an explicit compatibility field but
	// is never honored by BuildFeatureVector. Retrospective odds belong only in
	// the separately labeled bookmaker/backtest track.
	IncludeUnverifiedOdds bool
	MinimumHistoryMatches int
}

func DefaultFeatureConfig() FeatureConfig {
	return FeatureConfig{
		FeatureKey:            "point_in_time_vfl_features",
		Version:               "phase2.v1",
		RoundSafetyBuffer:     30 * time.Second,
		MinimumHistoryMatches: 0,
	}
}

type FeatureVector struct {
	MatchID             provider.MatchID   `json:"match_id"`
	SeasonID            provider.SeasonID  `json:"season_id"`
	Round               int                `json:"round"`
	CutoffAt            time.Time          `json:"cutoff_at"`
	GeneratedAt         time.Time          `json:"generated_at"`
	FeatureKey          string             `json:"feature_key"`
	FeatureVersion      string             `json:"feature_version"`
	Values              map[string]float64 `json:"values"`
	PointInTimeVerified bool               `json:"point_in_time_verified"`
	Provenance          map[string]string  `json:"provenance"`
}

func (v FeatureVector) EqualValues(other FeatureVector) bool {
	if v.MatchID != other.MatchID || v.SeasonID != other.SeasonID || v.Round != other.Round || !v.CutoffAt.Equal(other.CutoffAt) {
		return false
	}
	if len(v.Values) != len(other.Values) {
		return false
	}
	for key, value := range v.Values {
		if otherValue, exists := other.Values[key]; !exists || math.Abs(value-otherValue) > 1e-12 {
			return false
		}
	}
	return true
}

func (v FeatureVector) SortedKeys() []string {
	keys := make([]string, 0, len(v.Values))
	for key := range v.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizedText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
