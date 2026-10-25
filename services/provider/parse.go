package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type CanonicalSeason struct {
	ID              SeasonID
	TournamentID    TournamentID
	CurrentSeasonID *SeasonID
	Name            string
	Abbreviation    string
	StartUTC        time.Time
	EndUTC          time.Time
}

type FixturePayload struct {
	QueryURL string
	Season   CanonicalSeason
	Matches  []Fixture
}

type TeamRef struct {
	UID          TeamUID
	ProviderID   int64
	Name         string
	MediumName   string
	Abbreviation string
}

type Score struct {
	Home int64 `json:"home"`
	Away int64 `json:"away"`
}

type MatchStatus string

const (
	MatchStatusScheduled  MatchStatus = "scheduled"
	MatchStatusInProgress MatchStatus = "in_progress"
	MatchStatusCompleted  MatchStatus = "completed"
	MatchStatusCanceled   MatchStatus = "canceled"
	MatchStatusPostponed  MatchStatus = "postponed"
	MatchStatusUnknown    MatchStatus = "unknown"
)

type Fixture struct {
	MatchID         MatchID
	ProviderMatchID string
	SeasonID        SeasonID
	TournamentID    TournamentID
	Round           RoundNumber
	ScheduledUTS    int64
	ScheduledAt     time.Time
	Home            TeamRef
	Away            TeamRef
	Score           *Score
	HalfTimeScore   *Score
	FullTimeScore   *Score
	PeriodsKind     PeriodsKind
	ProviderStatus  json.RawMessage
	Postponed       bool
	Canceled        bool
	Walkover        bool
	ToBeAnnounced   bool
	InLiveScore     bool
}

func ParseFixtures(payload []byte) (FixturePayload, error) {
	var envelope ProviderFixtureEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return FixturePayload{}, fmt.Errorf("decode fixtures envelope: %w", err)
	}
	if strings.TrimSpace(envelope.QueryURL) == "" {
		return FixturePayload{}, fmt.Errorf("fixtures envelope is missing queryUrl")
	}
	if len(envelope.Doc) != 1 {
		return FixturePayload{}, fmt.Errorf("fixtures envelope must contain exactly one doc, got %d", len(envelope.Doc))
	}
	doc := envelope.Doc[0]
	if doc.Data.ID.String() == "" {
		return FixturePayload{}, fmt.Errorf("fixtures document is missing data._id")
	}
	if len(doc.Data.Matches) == 0 {
		return FixturePayload{}, fmt.Errorf("fixtures document contains no matches")
	}
	tournamentID, err := NewTournamentID(doc.Data.TournamentID)
	if err != nil {
		return FixturePayload{}, fmt.Errorf("season %s: %w", doc.Data.ID, err)
	}
	seasonID := SeasonID(doc.Data.ID.String())
	start, err := providerTimeUTC(doc.Data.Start, "season start")
	if err != nil {
		return FixturePayload{}, err
	}
	end, err := providerTimeUTC(doc.Data.End, "season end")
	if err != nil {
		return FixturePayload{}, err
	}
	season := CanonicalSeason{
		ID:           seasonID,
		TournamentID: tournamentID,
		Name:         doc.Data.Name,
		Abbreviation: doc.Data.Abbreviation,
		StartUTC:     start,
		EndUTC:       end,
	}
	if value := doc.Data.CurrentSeasonID.String(); value != "" {
		current := SeasonID(value)
		season.CurrentSeasonID = &current
	}

	matches := make([]Fixture, 0, len(doc.Data.Matches))
	seenIDs := make(map[MatchID]struct{}, len(doc.Data.Matches))
	for index, raw := range doc.Data.Matches {
		fixture, err := normalizeFixture(raw, seasonID, tournamentID)
		if err != nil {
			return FixturePayload{}, fmt.Errorf("match[%d]: %w", index, err)
		}
		if _, exists := seenIDs[fixture.MatchID]; exists {
			return FixturePayload{}, fmt.Errorf("duplicate match ID %s", fixture.MatchID)
		}
		seenIDs[fixture.MatchID] = struct{}{}
		matches = append(matches, fixture)
	}
	return FixturePayload{QueryURL: envelope.QueryURL, Season: season, Matches: matches}, nil
}

func normalizeFixture(raw ProviderMatch, seasonID SeasonID, tournamentID TournamentID) (Fixture, error) {
	matchID, err := NewMatchID(raw.ID)
	if err != nil {
		return Fixture{}, err
	}
	rawSeasonID := strconv.FormatInt(raw.SeasonID, 10)
	if raw.SeasonID <= 0 || rawSeasonID != seasonID.String() {
		return Fixture{}, fmt.Errorf("_seasonid %q does not match season %q", rawSeasonID, seasonID)
	}
	round, err := NewRoundNumber(raw.Round)
	if err != nil {
		return Fixture{}, err
	}
	scheduledAt, err := providerTimeUTC(raw.Time, "fixture time")
	if err != nil {
		return Fixture{}, err
	}
	if raw.Teams == nil {
		return Fixture{}, fmt.Errorf("missing teams")
	}
	home, err := normalizeFixtureTeam(raw.Teams.Home)
	if err != nil {
		return Fixture{}, fmt.Errorf("home team: %w", err)
	}
	away, err := normalizeFixtureTeam(raw.Teams.Away)
	if err != nil {
		return Fixture{}, fmt.Errorf("away team: %w", err)
	}
	if raw.Result == nil {
		return Fixture{}, fmt.Errorf("missing result object")
	}
	finalScore, err := normalizeNullableScore(raw.Result.Home, raw.Result.Away)
	if err != nil {
		return Fixture{}, fmt.Errorf("result: %w", err)
	}
	var halfTimeScore, fullTimeScore *Score
	if raw.Periods.Scores != nil {
		halfTimeScore, err = normalizeProviderScore(raw.Periods.Scores.P1)
		if err != nil {
			return Fixture{}, fmt.Errorf("periods.p1: %w", err)
		}
		fullTimeScore, err = normalizeProviderScore(raw.Periods.Scores.FT)
		if err != nil {
			return Fixture{}, fmt.Errorf("periods.ft: %w", err)
		}
	}
	return Fixture{
		MatchID:         matchID,
		ProviderMatchID: strconv.FormatInt(raw.ID, 10),
		SeasonID:        seasonID,
		TournamentID:    tournamentID,
		Round:           round,
		ScheduledUTS:    raw.Time.UTS,
		ScheduledAt:     scheduledAt,
		Home:            home,
		Away:            away,
		Score:           finalScore,
		HalfTimeScore:   halfTimeScore,
		FullTimeScore:   fullTimeScore,
		PeriodsKind:     raw.Periods.Kind,
		ProviderStatus:  append(json.RawMessage(nil), raw.Status...),
		Postponed:       raw.Postponed,
		Canceled:        raw.Canceled,
		Walkover:        raw.Walkover,
		ToBeAnnounced:   raw.ToBeAnnounced,
		InLiveScore:     raw.InLiveScore,
	}, nil
}

func normalizeFixtureTeam(raw ProviderFixtureTeam) (TeamRef, error) {
	uid, err := NewTeamUID(raw.UID)
	if err != nil {
		return TeamRef{}, err
	}
	if strings.TrimSpace(raw.Name) == "" {
		return TeamRef{}, fmt.Errorf("team %s is missing name", uid)
	}
	return TeamRef{
		UID:          uid,
		ProviderID:   raw.ID,
		Name:         raw.Name,
		MediumName:   raw.MediumName,
		Abbreviation: raw.Abbr,
	}, nil
}

func normalizeNullableScore(home, away *int64) (*Score, error) {
	if home == nil && away == nil {
		return nil, nil
	}
	if home == nil || away == nil {
		return nil, fmt.Errorf("home and away scores must both be null or both be present")
	}
	if *home < 0 || *away < 0 {
		return nil, fmt.Errorf("scores must be non-negative")
	}
	return &Score{Home: *home, Away: *away}, nil
}

func normalizeProviderScore(raw *ProviderScore) (*Score, error) {
	if raw == nil {
		return nil, nil
	}
	if raw.Home < 0 || raw.Away < 0 {
		return nil, fmt.Errorf("scores must be non-negative")
	}
	return &Score{Home: raw.Home, Away: raw.Away}, nil
}

func providerTimeUTC(raw ProviderTime, label string) (time.Time, error) {
	if raw.UTS <= 0 {
		return time.Time{}, fmt.Errorf("%s has invalid uts %d", label, raw.UTS)
	}
	return time.Unix(raw.UTS, 0).UTC(), nil
}

// StatusAsOf deliberately fails safe. The sample status field is null even
// for scored fixtures, so it is not treated as a live/completed signal. A
// score becomes completed only after kickoff and a caller-provided
// confirmation window have passed.
func (f Fixture) StatusAsOf(now time.Time, settlementConfirmationWindow time.Duration) MatchStatus {
	if f.Canceled {
		return MatchStatusCanceled
	}
	if f.Postponed {
		return MatchStatusPostponed
	}
	if f.Walkover {
		return MatchStatusUnknown
	}
	if f.Score == nil {
		if f.ToBeAnnounced || f.ScheduledAt.After(now) {
			return MatchStatusScheduled
		}
		return MatchStatusUnknown
	}
	if f.ScheduledAt.After(now) {
		return MatchStatusUnknown
	}
	if settlementConfirmationWindow < 0 {
		settlementConfirmationWindow = 0
	}
	if now.Before(f.ScheduledAt.Add(settlementConfirmationWindow)) {
		return MatchStatusUnknown
	}
	return MatchStatusCompleted
}

type OddsSnapshot struct {
	QueryURL              string
	SourceTimestampMillis int64
	SourceTimestampUTC    time.Time
	Records               []OddsRecord
	ValidationIssues      []OddsValidationIssue
}

type OddsValidationIssue struct {
	MatchID   MatchID
	MarketID  int64
	OutcomeID string
	RawOdds   string
	Reason    string
}

type OddsRecord struct {
	MatchID              MatchID
	ProviderMatchID      string
	TournamentID         TournamentID
	SportID              string
	HomeTeamUID          TeamUID
	AwayTeamUID          TeamUID
	HomeTeamProviderID   string
	AwayTeamProviderID   string
	HomeTeamName         string
	AwayTeamName         string
	HomeTeamAbbreviation string
	AwayTeamAbbreviation string
	Markets              []Market
}

type Market struct {
	ID                 int64
	Name               string
	ProviderSpecifiers *string
	Specifiers         string
	Status             int64
	Favourite          *int64
	Outcomes           []Outcome
}

type Outcome struct {
	ID             string
	Name           string
	Abbreviation   string
	RawOdds        string
	Odds           *Decimal
	OddsError      string
	Active         int64
	ProviderResult int64
}

func (m Market) Identity(matchID MatchID, outcome Outcome) MarketIdentity {
	return MarketIdentity{MatchID: matchID, MarketID: m.ID, Specifiers: m.Specifiers, OutcomeID: outcome.ID}
}

func (o Outcome) IsActive() bool { return o.Active == 1 }

func (o Outcome) IsUsable() bool { return o.Odds != nil && o.OddsError == "" }

func (m Market) IsActive() bool { return m.Status == 1 }

func ParseOdds(payload []byte) (OddsSnapshot, error) {
	var envelope ProviderOddsEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return OddsSnapshot{}, fmt.Errorf("decode odds envelope: %w", err)
	}
	if strings.TrimSpace(envelope.QueryURL) == "" {
		return OddsSnapshot{}, fmt.Errorf("odds envelope is missing queryUrl")
	}
	if len(envelope.Doc) != 1 {
		return OddsSnapshot{}, fmt.Errorf("odds envelope must contain exactly one doc, got %d", len(envelope.Doc))
	}
	doc := envelope.Doc[0]
	if doc.Timestamp <= 0 {
		return OddsSnapshot{}, fmt.Errorf("odds document has invalid millisecond timestamp %d", doc.Timestamp)
	}
	if len(doc.Data.Odds) == 0 {
		return OddsSnapshot{}, fmt.Errorf("odds document contains no match records")
	}
	records := make([]OddsRecord, 0, len(doc.Data.Odds))
	validationIssues := make([]OddsValidationIssue, 0)
	seenIDs := make(map[MatchID]struct{}, len(doc.Data.Odds))
	for index, raw := range doc.Data.Odds {
		record, issues, err := normalizeOddsRecord(raw)
		if err != nil {
			return OddsSnapshot{}, fmt.Errorf("odds[%d]: %w", index, err)
		}
		if _, exists := seenIDs[record.MatchID]; exists {
			return OddsSnapshot{}, fmt.Errorf("duplicate odds match ID %s", record.MatchID)
		}
		seenIDs[record.MatchID] = struct{}{}
		records = append(records, record)
		validationIssues = append(validationIssues, issues...)
	}
	return OddsSnapshot{
		QueryURL:              envelope.QueryURL,
		SourceTimestampMillis: doc.Timestamp,
		SourceTimestampUTC:    time.UnixMilli(doc.Timestamp).UTC(),
		Records:               records,
		ValidationIssues:      validationIssues,
	}, nil
}

func normalizeOddsRecord(raw ProviderOddsRecord) (OddsRecord, []OddsValidationIssue, error) {
	matchID, err := ParseProviderMatchID(raw.ID)
	if err != nil {
		return OddsRecord{}, nil, err
	}
	tournamentID, err := ParseProviderTournamentID(raw.TournamentID)
	if err != nil {
		return OddsRecord{}, nil, err
	}
	homeUID, err := ParseProviderTeamUID(raw.Teams.Home.ID)
	if err != nil {
		return OddsRecord{}, nil, fmt.Errorf("home team: %w", err)
	}
	awayUID, err := ParseProviderTeamUID(raw.Teams.Away.ID)
	if err != nil {
		return OddsRecord{}, nil, fmt.Errorf("away team: %w", err)
	}
	if len(raw.Markets) == 0 {
		return OddsRecord{}, nil, fmt.Errorf("match %s contains no markets", matchID)
	}
	markets := make([]Market, 0, len(raw.Markets))
	validationIssues := make([]OddsValidationIssue, 0)
	for index, rawMarket := range raw.Markets {
		if rawMarket.ID <= 0 {
			return OddsRecord{}, nil, fmt.Errorf("market[%d] has invalid ID %d", index, rawMarket.ID)
		}
		if len(rawMarket.Outcomes) == 0 {
			return OddsRecord{}, nil, fmt.Errorf("market[%d] has no outcomes", index)
		}
		specifiers, err := NormalizeSpecifiers(rawMarket.Specifiers)
		if err != nil {
			return OddsRecord{}, nil, fmt.Errorf("market[%d] specifiers: %w", index, err)
		}
		outcomes := make([]Outcome, 0, len(rawMarket.Outcomes))
		for outcomeIndex, rawOutcome := range rawMarket.Outcomes {
			if strings.TrimSpace(rawOutcome.ID) == "" {
				return OddsRecord{}, nil, fmt.Errorf("market[%d] outcome[%d] has empty ID", index, outcomeIndex)
			}
			odds, err := ParseDecimal(rawOutcome.Odds)
			oddsError := ""
			var parsedOdds *Decimal
			if err == nil {
				parsedOdds = &odds
			}
			if err != nil {
				oddsError = err.Error()
				validationIssues = append(validationIssues, OddsValidationIssue{
					MatchID:   matchID,
					MarketID:  rawMarket.ID,
					OutcomeID: rawOutcome.ID,
					RawOdds:   rawOutcome.Odds,
					Reason:    err.Error(),
				})
			}
			outcomes = append(outcomes, Outcome{
				ID:             rawOutcome.ID,
				Name:           rawOutcome.Name,
				Abbreviation:   rawOutcome.Abbr,
				RawOdds:        rawOutcome.Odds,
				Odds:           parsedOdds,
				OddsError:      oddsError,
				Active:         rawOutcome.Active,
				ProviderResult: rawOutcome.Result,
			})
		}
		var rawSpecifiers *string
		if rawMarket.Specifiers != nil {
			value := *rawMarket.Specifiers
			rawSpecifiers = &value
		}
		markets = append(markets, Market{
			ID:                 rawMarket.ID,
			Name:               rawMarket.Name,
			ProviderSpecifiers: rawSpecifiers,
			Specifiers:         specifiers,
			Status:             rawMarket.Status,
			Favourite:          rawMarket.Favourite,
			Outcomes:           outcomes,
		})
	}
	return OddsRecord{
		MatchID:              matchID,
		ProviderMatchID:      raw.ID,
		TournamentID:         tournamentID,
		SportID:              raw.SportID,
		HomeTeamUID:          homeUID,
		AwayTeamUID:          awayUID,
		HomeTeamProviderID:   raw.Teams.Home.ID,
		AwayTeamProviderID:   raw.Teams.Away.ID,
		HomeTeamName:         raw.Teams.Home.Name,
		AwayTeamName:         raw.Teams.Away.Name,
		HomeTeamAbbreviation: raw.Teams.Home.Abbr,
		AwayTeamAbbreviation: raw.Teams.Away.Abbr,
		Markets:              markets,
	}, validationIssues, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
