package research

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"bet9ja-vfl/services/provider"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// LoadDataset reads normalized Phase 1 records. Invalid odds are intentionally
// excluded here because they remain available in raw_payloads and validation
// quarantine; a research dataset must never treat an invalid price as a quote.
func LoadDataset(ctx context.Context, db *sql.DB) (Dataset, error) {
	if db == nil {
		return Dataset{}, fmt.Errorf("database is nil")
	}
	seasons, err := loadSeasons(ctx, db)
	if err != nil {
		return Dataset{}, err
	}

	rows, err := db.QueryContext(ctx, `
SELECT
    f.match_id, f.season_id, f.round_no, f.scheduled_at,
    f.home_team_uid, f.away_team_uid,
    f.result_home, f.result_away,
    f.canceled, f.postponed, f.walkover,
    COALESCE(o.provider_market_id, 0), COALESCE(md.market_name, ''), COALESCE(o.specifiers, ''),
    COALESCE(o.outcome_id, ''), COALESCE(o.outcome_name, ''), COALESCE(o.outcome_abbreviation, ''),
    COALESCE(o.raw_odds, ''), COALESCE(o.decimal_odds::text, ''),
    COALESCE(oo.source_timestamp_at, o.source_timestamp_at),
    COALESCE(oo.observed_at, o.last_observed_at),
    COALESCE(oo.point_in_time_verified, o.point_in_time_verified, false),
    COALESCE(oo.observation_kind, o.observation_kind, ''), COALESCE(o.outcome_active, 0),
    COALESCE(ht.name, ''), COALESCE(at.name, '')
FROM fixtures f
JOIN teams ht ON ht.team_uid = f.home_team_uid
JOIN teams at ON at.team_uid = f.away_team_uid
LEFT JOIN odds_snapshots o ON o.match_id = f.match_id AND o.odds_valid = true
LEFT JOIN market_definitions md ON md.market_definition_id = o.market_definition_id
LEFT JOIN odds_snapshot_observations oo ON oo.odds_snapshot_id = o.odds_snapshot_id
ORDER BY f.scheduled_at, f.match_id, o.provider_market_id, o.specifiers, o.outcome_id,
         COALESCE(oo.observed_at, o.last_observed_at)`)
	if err != nil {
		return Dataset{}, fmt.Errorf("load fixtures and odds: %w", err)
	}
	defer rows.Close()
	matches := make([]MatchRecord, 0)
	indices := make(map[provider.MatchID]int)
	for rows.Next() {
		var matchIDValue, homeUIDValue, awayUIDValue int64
		var seasonID string
		var round int
		var scheduledAt time.Time
		var homeScore, awayScore sql.NullInt64
		var canceled, postponed, walkover bool
		var marketID int64
		var marketName, specifiers, outcomeID, outcomeName, outcomeAbbreviation, rawOdds, decimalOdds string
		var sourceTimestampAt, observedAt sql.NullTime
		var pointInTimeVerified bool
		var observationKind string
		var active int64
		var homeName, awayName string
		if err := rows.Scan(
			&matchIDValue, &seasonID, &round, &scheduledAt, &homeUIDValue, &awayUIDValue,
			&homeScore, &awayScore, &canceled, &postponed, &walkover,
			&marketID, &marketName, &specifiers, &outcomeID, &outcomeName, &outcomeAbbreviation,
			&rawOdds, &decimalOdds, &sourceTimestampAt, &observedAt, &pointInTimeVerified,
			&observationKind, &active, &homeName, &awayName); err != nil {
			return Dataset{}, fmt.Errorf("scan fixture/odds: %w", err)
		}
		matchID, err := provider.NewMatchID(matchIDValue)
		if err != nil {
			return Dataset{}, err
		}
		homeUID, err := provider.NewTeamUID(homeUIDValue)
		if err != nil {
			return Dataset{}, err
		}
		awayUID, err := provider.NewTeamUID(awayUIDValue)
		if err != nil {
			return Dataset{}, err
		}
		index, exists := indices[matchID]
		if !exists {
			match := MatchRecord{
				MatchID:      matchID,
				SeasonID:     provider.SeasonID(seasonID),
				Round:        round,
				ScheduledAt:  scheduledAt.UTC(),
				HomeTeamUID:  homeUID,
				AwayTeamUID:  awayUID,
				HomeTeamName: homeName,
				AwayTeamName: awayName,
				Canceled:     canceled,
				Postponed:    postponed,
				Walkover:     walkover,
			}
			if homeScore.Valid {
				value := int(homeScore.Int64)
				match.HomeScore = &value
			}
			if awayScore.Valid {
				value := int(awayScore.Int64)
				match.AwayScore = &value
			}
			matches = append(matches, match)
			index = len(matches) - 1
			indices[matchID] = index
		}
		if marketID == 0 || strings.TrimSpace(decimalOdds) == "" || strings.TrimSpace(outcomeID) == "" {
			continue
		}
		odds, err := strconv.ParseFloat(decimalOdds, 64)
		if err != nil || odds <= 0 {
			continue
		}
		matches[index].Odds = append(matches[index].Odds, OddsQuote{
			MarketID:            marketID,
			MarketName:          marketName,
			Specifiers:          specifiers,
			OutcomeID:           outcomeID,
			OutcomeName:         outcomeName,
			OutcomeAbbreviation: outcomeAbbreviation,
			Odds:                odds,
			RawOdds:             rawOdds,
			SourceTimestampAt:   nullTime(sourceTimestampAt),
			ObservedAt:          nullTime(observedAt),
			PointInTimeVerified: pointInTimeVerified,
			ObservationKind:     observationKind,
			Active:              active == 1,
		})
	}
	if err := rows.Err(); err != nil {
		return Dataset{}, fmt.Errorf("read fixture/odds: %w", err)
	}
	return NewDataset(matches, seasons), nil
}

// LoadFixtureDataset loads the normalized season and fixture/result records
// without joining odds history. The live prediction path uses this bounded
// loader because fitting the dynamic model does not require every historical
// odds observation. Research and backtest callers that explicitly need the
// complete odds history should continue to use LoadDataset.
func LoadFixtureDataset(ctx context.Context, db *sql.DB) (Dataset, error) {
	if db == nil {
		return Dataset{}, fmt.Errorf("database is nil")
	}
	seasons, err := loadSeasons(ctx, db)
	if err != nil {
		return Dataset{}, err
	}
	matches, err := loadFixtureMatches(ctx, db)
	if err != nil {
		return Dataset{}, err
	}
	return NewDataset(matches, seasons), nil
}

func loadSeasons(ctx context.Context, db *sql.DB) ([]SeasonRecord, error) {
	seasonRows, err := db.QueryContext(ctx, `
SELECT season_id, starts_at, ends_at
FROM seasons
ORDER BY starts_at NULLS LAST, season_id`)
	if err != nil {
		return nil, fmt.Errorf("load seasons: %w", err)
	}
	defer seasonRows.Close()
	seasons := make([]SeasonRecord, 0)
	for seasonRows.Next() {
		var id string
		var startsAt, endsAt sql.NullTime
		if err := seasonRows.Scan(&id, &startsAt, &endsAt); err != nil {
			return nil, fmt.Errorf("scan season: %w", err)
		}
		seasons = append(seasons, SeasonRecord{ID: provider.SeasonID(id), StartUTC: nullTime(startsAt), EndUTC: nullTime(endsAt)})
	}
	if err := seasonRows.Err(); err != nil {
		return nil, fmt.Errorf("read seasons: %w", err)
	}
	return seasons, nil
}

func loadFixtureMatches(ctx context.Context, db *sql.DB) ([]MatchRecord, error) {
	rows, err := db.QueryContext(ctx, `
SELECT
    f.match_id, f.season_id, f.round_no, f.scheduled_at,
    f.home_team_uid, f.away_team_uid,
    f.result_home, f.result_away,
    f.canceled, f.postponed, f.walkover,
    COALESCE(ht.name, ''), COALESCE(at.name, '')
FROM fixtures f
JOIN teams ht ON ht.team_uid = f.home_team_uid
JOIN teams at ON at.team_uid = f.away_team_uid
ORDER BY f.scheduled_at, f.match_id`)
	if err != nil {
		return nil, fmt.Errorf("load fixtures: %w", err)
	}
	defer rows.Close()
	matches := make([]MatchRecord, 0)
	for rows.Next() {
		var matchIDValue, homeUIDValue, awayUIDValue int64
		var seasonID string
		var round int
		var scheduledAt time.Time
		var homeScore, awayScore sql.NullInt64
		var canceled, postponed, walkover bool
		var homeName, awayName string
		if err := rows.Scan(
			&matchIDValue, &seasonID, &round, &scheduledAt, &homeUIDValue, &awayUIDValue,
			&homeScore, &awayScore, &canceled, &postponed, &walkover, &homeName, &awayName); err != nil {
			return nil, fmt.Errorf("scan fixture: %w", err)
		}
		matchID, err := provider.NewMatchID(matchIDValue)
		if err != nil {
			return nil, err
		}
		homeUID, err := provider.NewTeamUID(homeUIDValue)
		if err != nil {
			return nil, err
		}
		awayUID, err := provider.NewTeamUID(awayUIDValue)
		if err != nil {
			return nil, err
		}
		match := MatchRecord{
			MatchID:      matchID,
			SeasonID:     provider.SeasonID(seasonID),
			Round:        round,
			ScheduledAt:  scheduledAt.UTC(),
			HomeTeamUID:  homeUID,
			AwayTeamUID:  awayUID,
			HomeTeamName: homeName,
			AwayTeamName: awayName,
			Canceled:     canceled,
			Postponed:    postponed,
			Walkover:     walkover,
		}
		if homeScore.Valid {
			value := int(homeScore.Int64)
			match.HomeScore = &value
		}
		if awayScore.Valid {
			value := int(awayScore.Int64)
			match.AwayScore = &value
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read fixtures: %w", err)
	}
	return matches, nil
}

// LoadOddsForMatches loads odds only for the supplied canonical match IDs.
// It intentionally preserves the same observation/provenance semantics as
// LoadDataset, but keeps the live prediction query bounded to one target
// round instead of the entire historical odds table.
func LoadOddsForMatches(ctx context.Context, db *sql.DB, matchIDs []provider.MatchID) (map[provider.MatchID][]OddsQuote, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	unique := make(map[provider.MatchID]struct{}, len(matchIDs))
	for _, matchID := range matchIDs {
		if matchID != 0 {
			unique[matchID] = struct{}{}
		}
	}
	result := make(map[provider.MatchID][]OddsQuote, len(unique))
	if len(unique) == 0 {
		return result, nil
	}
	ids := make([]provider.MatchID, 0, len(unique))
	for matchID := range unique {
		ids = append(ids, matchID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for index, matchID := range ids {
		placeholders[index] = fmt.Sprintf("$%d", index+1)
		args[index] = int64(matchID)
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT
    o.match_id,
    o.provider_market_id, COALESCE(md.market_name, ''), COALESCE(o.specifiers, ''),
    COALESCE(o.outcome_id, ''), COALESCE(o.outcome_name, ''), COALESCE(o.outcome_abbreviation, ''),
    COALESCE(o.raw_odds, ''), COALESCE(o.decimal_odds::text, ''),
    COALESCE(oo.source_timestamp_at, o.source_timestamp_at),
    COALESCE(oo.observed_at, o.last_observed_at),
    COALESCE(oo.point_in_time_verified, o.point_in_time_verified, false),
    COALESCE(oo.observation_kind, o.observation_kind, ''), COALESCE(o.outcome_active, 0)
FROM odds_snapshots o
LEFT JOIN market_definitions md ON md.market_definition_id = o.market_definition_id
LEFT JOIN odds_snapshot_observations oo ON oo.odds_snapshot_id = o.odds_snapshot_id
WHERE o.match_id IN (%s) AND o.odds_valid = true
ORDER BY o.match_id, o.provider_market_id, o.specifiers, o.outcome_id,
         COALESCE(oo.observed_at, o.last_observed_at)`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("load target odds: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var matchIDValue, marketID int64
		var marketName, specifiers, outcomeID, outcomeName, outcomeAbbreviation, rawOdds, decimalOdds string
		var sourceTimestampAt, observedAt sql.NullTime
		var pointInTimeVerified bool
		var observationKind string
		var active int64
		if err := rows.Scan(&matchIDValue, &marketID, &marketName, &specifiers, &outcomeID, &outcomeName,
			&outcomeAbbreviation, &rawOdds, &decimalOdds, &sourceTimestampAt, &observedAt,
			&pointInTimeVerified, &observationKind, &active); err != nil {
			return nil, fmt.Errorf("scan target odds: %w", err)
		}
		matchID, err := provider.NewMatchID(matchIDValue)
		if err != nil {
			return nil, err
		}
		if marketID == 0 || strings.TrimSpace(decimalOdds) == "" || strings.TrimSpace(outcomeID) == "" {
			continue
		}
		odds, err := strconv.ParseFloat(decimalOdds, 64)
		if err != nil || odds <= 0 {
			continue
		}
		result[matchID] = append(result[matchID], OddsQuote{
			MarketID:            marketID,
			MarketName:          marketName,
			Specifiers:          specifiers,
			OutcomeID:           outcomeID,
			OutcomeName:         outcomeName,
			OutcomeAbbreviation: outcomeAbbreviation,
			Odds:                odds,
			RawOdds:             rawOdds,
			SourceTimestampAt:   nullTime(sourceTimestampAt),
			ObservedAt:          nullTime(observedAt),
			PointInTimeVerified: pointInTimeVerified,
			ObservationKind:     observationKind,
			Active:              active == 1,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read target odds: %w", err)
	}
	return result, nil
}

func nullTime(value sql.NullTime) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}
