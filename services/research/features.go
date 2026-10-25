package research

import (
	"fmt"
	"math"
	"sort"
	"time"

	"bet9ja-vfl/services/provider"
)

// BuildFeatureVector creates features at an explicit cutoff. The caller must
// provide the cutoff used by the prediction policy; no feature function derives
// a cutoff from a result or from a post-match odds observation.
func BuildFeatureVector(dataset Dataset, target MatchRecord, cutoff time.Time, config FeatureConfig) (FeatureVector, error) {
	if target.MatchID == 0 || target.HomeTeamUID == 0 || target.AwayTeamUID == 0 {
		return FeatureVector{}, fmt.Errorf("target match is missing canonical identifiers")
	}
	if cutoff.IsZero() {
		return FeatureVector{}, fmt.Errorf("feature cutoff is required")
	}
	config = mergeFeatureConfig(DefaultFeatureConfig(), config)
	history := make([]MatchRecord, 0)
	for _, match := range dataset.Matches {
		if !match.Final() || !match.ScheduledAt.Before(cutoff) {
			continue
		}
		history = append(history, match)
	}
	sort.SliceStable(history, func(i, j int) bool {
		if !history[i].ScheduledAt.Equal(history[j].ScheduledAt) {
			return history[i].ScheduledAt.Before(history[j].ScheduledAt)
		}
		return history[i].MatchID < history[j].MatchID
	})

	values := make(map[string]float64)
	homeStats := summarizeTeam(history, target.HomeTeamUID)
	awayStats := summarizeTeam(history, target.AwayTeamUID)
	addTeamFeatures(values, "home_team", homeStats)
	addTeamFeatures(values, "away_team", awayStats)
	addRollingFeatures(values, "home_team", target.HomeTeamUID, recentTeamMatches(history, target.HomeTeamUID), 3, 5, 10)
	addRollingFeatures(values, "away_team", target.AwayTeamUID, recentTeamMatches(history, target.AwayTeamUID), 3, 5, 10)

	h2h := summarizeHeadToHead(history, target.HomeTeamUID, target.AwayTeamUID)
	values["h2h_home_win_rate"] = rate(h2h.homeWins, h2h.matches)
	values["h2h_draw_rate"] = rate(h2h.draws, h2h.matches)
	values["h2h_away_win_rate"] = rate(h2h.awayWins, h2h.matches)
	values["h2h_goal_difference_avg"] = average(h2h.goalDifference, h2h.matches)
	values["h2h_matches"] = float64(h2h.matches)

	positions := leaguePositions(history, target.SeasonID)
	values["home_current_position"] = float64(positions[target.HomeTeamUID])
	values["away_current_position"] = float64(positions[target.AwayTeamUID])
	if positions[target.HomeTeamUID] == 0 {
		values["home_current_position"] = 0
	}
	if positions[target.AwayTeamUID] == 0 {
		values["away_current_position"] = 0
	}

	previousSeason := previousSeasonFor(dataset, target)
	previousHome := summarizeTeam(previousSeason, target.HomeTeamUID)
	previousAway := summarizeTeam(previousSeason, target.AwayTeamUID)
	values["home_previous_season_points_per_match"] = average(previousHome.points, previousHome.matches)
	values["away_previous_season_points_per_match"] = average(previousAway.points, previousAway.matches)
	values["home_previous_season_goal_difference_avg"] = average(previousHome.goalDifference, previousHome.matches)
	values["away_previous_season_goal_difference_avg"] = average(previousAway.goalDifference, previousAway.matches)
	values["previous_season_available"] = boolFloat(len(previousSeason) > 0)

	values["home_rest_days"] = restDays(history, target.HomeTeamUID, cutoff)
	values["away_rest_days"] = restDays(history, target.AwayTeamUID, cutoff)
	values["season_round"] = float64(target.Round)

	provenance := map[string]string{
		"history_policy": "final_results_scheduled_strictly_before_cutoff",
		"cutoff":         cutoff.UTC().Format(time.RFC3339Nano),
		"odds_policy":    "verified_observation_at_or_before_cutoff_only",
	}
	pointInTimeVerified := true
	if line, ok := FindOneXTwoOddsAt(target, cutoff); ok {
		implied := line.ImpliedProbabilities()
		values["market_home_implied"] = implied.Home
		values["market_draw_implied"] = implied.Draw
		values["market_away_implied"] = implied.Away
		values["market_overround"] = line.Overround
		provenance["odds_observed_at"] = line.HomeQuote.ObservedAt.UTC().Format(time.RFC3339Nano)
		provenance["odds_source_timestamp_at"] = line.HomeQuote.SourceTimestampAt.UTC().Format(time.RFC3339Nano)
		provenance["odds_status"] = "verified"
	} else {
		values["market_home_implied"] = 0
		values["market_draw_implied"] = 0
		values["market_away_implied"] = 0
		values["market_overround"] = 0
		provenance["odds_status"] = "unavailable_at_cutoff"
		// Missing odds do not make result-derived features non-point-in-time.
		// The explicit provenance status above distinguishes an incomplete odds
		// group from a vector that used an unverified historical observation.
	}

	if config.MinimumHistoryMatches > 0 && len(history) < config.MinimumHistoryMatches {
		provenance["history_status"] = "below_minimum"
	} else {
		provenance["history_status"] = "eligible"
	}
	return FeatureVector{
		MatchID:             target.MatchID,
		SeasonID:            target.SeasonID,
		Round:               target.Round,
		CutoffAt:            cutoff.UTC(),
		GeneratedAt:         cutoff.UTC(),
		FeatureKey:          config.FeatureKey,
		FeatureVersion:      config.Version,
		Values:              values,
		PointInTimeVerified: pointInTimeVerified,
		Provenance:          provenance,
	}, nil
}

// BuildFeatureVectors excludes all matches from the target round by using the
// earliest scheduled match in that round minus the safety buffer. This avoids
// leaking a result from a same-round fixture into another same-round vector.
func BuildFeatureVectors(dataset Dataset, config FeatureConfig) ([]FeatureVector, error) {
	config = mergeFeatureConfig(DefaultFeatureConfig(), config)
	cutoffs := make(map[string]time.Time)
	for _, match := range dataset.Matches {
		key := fmt.Sprintf("%s/%d", match.SeasonID, match.Round)
		candidate := match.ScheduledAt.Add(-config.RoundSafetyBuffer)
		if existing, ok := cutoffs[key]; !ok || candidate.Before(existing) {
			cutoffs[key] = candidate
		}
	}
	result := make([]FeatureVector, 0, len(dataset.Matches))
	for _, match := range NewDataset(dataset.Matches, dataset.Seasons).Matches {
		key := fmt.Sprintf("%s/%d", match.SeasonID, match.Round)
		cutoff := cutoffs[key]
		vector, err := BuildFeatureVector(dataset, match, cutoff, config)
		if err != nil {
			return nil, err
		}
		result = append(result, vector)
	}
	return result, nil
}

func mergeFeatureConfig(defaults, provided FeatureConfig) FeatureConfig {
	if provided.FeatureKey != "" {
		defaults.FeatureKey = provided.FeatureKey
	}
	if provided.Version != "" {
		defaults.Version = provided.Version
	}
	if provided.RoundSafetyBuffer != 0 {
		defaults.RoundSafetyBuffer = provided.RoundSafetyBuffer
	}
	if provided.MinimumHistoryMatches != 0 {
		defaults.MinimumHistoryMatches = provided.MinimumHistoryMatches
	}
	defaults.IncludeUnverifiedOdds = provided.IncludeUnverifiedOdds
	return defaults
}

type teamSummary struct {
	matches        int
	homeMatches    int
	awayMatches    int
	wins           int
	draws          int
	losses         int
	homeWins       int
	homeDraws      int
	homeLosses     int
	awayWins       int
	awayDraws      int
	awayLosses     int
	goalsFor       int
	goalsAgainst   int
	points         float64
	goalDifference float64
	lastMatchAt    time.Time
}

func summarizeTeam(matches []MatchRecord, uid provider.TeamUID) teamSummary {
	var result teamSummary
	for _, match := range matches {
		if !match.HasTeam(uid) || !match.Final() {
			continue
		}
		result.matches++
		if !result.lastMatchAt.After(match.ScheduledAt) {
			result.lastMatchAt = match.ScheduledAt
		}
		var goalsFor, goalsAgainst int
		var outcome OutcomeKey
		if match.HomeTeamUID == uid {
			result.homeMatches++
			goalsFor, goalsAgainst = *match.HomeScore, *match.AwayScore
			outcome, _ = match.Outcome()
			switch outcome {
			case OutcomeHome:
				result.wins++
				result.homeWins++
			case OutcomeDraw:
				result.draws++
				result.homeDraws++
			case OutcomeAway:
				result.losses++
				result.homeLosses++
			}
		} else {
			result.awayMatches++
			goalsFor, goalsAgainst = *match.AwayScore, *match.HomeScore
			outcome, _ = match.Outcome()
			switch outcome {
			case OutcomeAway:
				result.wins++
				result.awayWins++
			case OutcomeDraw:
				result.draws++
				result.awayDraws++
			case OutcomeHome:
				result.losses++
				result.awayLosses++
			}
		}
		result.goalsFor += goalsFor
		result.goalsAgainst += goalsAgainst
		result.goalDifference += float64(goalsFor - goalsAgainst)
		switch outcome {
		case OutcomeHome, OutcomeAway:
			if (outcome == OutcomeHome && match.HomeTeamUID == uid) || (outcome == OutcomeAway && match.AwayTeamUID == uid) {
				result.points += 3
			}
		case OutcomeDraw:
			result.points++
		}
	}
	return result
}

func addTeamFeatures(values map[string]float64, prefix string, stats teamSummary) {
	values[prefix+"_matches"] = float64(stats.matches)
	values[prefix+"_win_rate"] = rate(stats.wins, stats.matches)
	values[prefix+"_draw_rate"] = rate(stats.draws, stats.matches)
	values[prefix+"_loss_rate"] = rate(stats.losses, stats.matches)
	values[prefix+"_goals_scored_avg"] = average(float64(stats.goalsFor), stats.matches)
	values[prefix+"_goals_conceded_avg"] = average(float64(stats.goalsAgainst), stats.matches)
	values[prefix+"_goal_difference_avg"] = average(stats.goalDifference, stats.matches)
	values[prefix+"_points_per_match"] = average(stats.points, stats.matches)
	values[prefix+"_home_win_rate"] = rate(stats.homeWins, stats.homeMatches)
	values[prefix+"_home_draw_rate"] = rate(stats.homeDraws, stats.homeMatches)
	values[prefix+"_home_loss_rate"] = rate(stats.homeLosses, stats.homeMatches)
	values[prefix+"_away_win_rate"] = rate(stats.awayWins, stats.awayMatches)
	values[prefix+"_away_draw_rate"] = rate(stats.awayDraws, stats.awayMatches)
	values[prefix+"_away_loss_rate"] = rate(stats.awayLosses, stats.awayMatches)
}

func recentTeamMatches(matches []MatchRecord, uid provider.TeamUID) []MatchRecord {
	result := make([]MatchRecord, 0)
	for _, match := range matches {
		if match.HasTeam(uid) && match.Final() {
			result = append(result, match)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].ScheduledAt.Equal(result[j].ScheduledAt) {
			return result[i].ScheduledAt.After(result[j].ScheduledAt)
		}
		return result[i].MatchID > result[j].MatchID
	})
	return result
}

func addRollingFeatures(values map[string]float64, prefix string, uid provider.TeamUID, matches []MatchRecord, windows ...int) {
	for _, window := range windows {
		if window <= 0 {
			continue
		}
		selected := matches
		if len(selected) > window {
			selected = selected[:window]
		}
		points, goalsFor, goalsAgainst := 0.0, 0.0, 0.0
		for _, match := range selected {
			outcome, _ := match.Outcome()
			if match.HomeTeamUID == uid {
				goalsFor += float64(*match.HomeScore)
				goalsAgainst += float64(*match.AwayScore)
				if outcome == OutcomeHome {
					points += 3
				} else if outcome == OutcomeDraw {
					points++
				}
			} else {
				goalsFor += float64(*match.AwayScore)
				goalsAgainst += float64(*match.HomeScore)
				if outcome == OutcomeAway {
					points += 3
				} else if outcome == OutcomeDraw {
					points++
				}
			}
		}
		values[fmt.Sprintf("%s_rolling_%d_points_avg", prefix, window)] = average(points, len(selected))
		values[fmt.Sprintf("%s_rolling_%d_goals_for_avg", prefix, window)] = average(goalsFor, len(selected))
		values[fmt.Sprintf("%s_rolling_%d_goals_against_avg", prefix, window)] = average(goalsAgainst, len(selected))
		values[fmt.Sprintf("%s_rolling_%d_matches", prefix, window)] = float64(len(selected))
	}
}

type headToHeadSummary struct {
	matches        int
	homeWins       int
	draws          int
	awayWins       int
	goalDifference float64
}

func summarizeHeadToHead(matches []MatchRecord, home, away provider.TeamUID) headToHeadSummary {
	var result headToHeadSummary
	for _, match := range matches {
		if !match.Final() || !((match.HomeTeamUID == home && match.AwayTeamUID == away) || (match.HomeTeamUID == away && match.AwayTeamUID == home)) {
			continue
		}
		result.matches++
		outcome, _ := match.Outcome()
		if outcome == OutcomeDraw {
			result.draws++
		} else if (outcome == OutcomeHome && match.HomeTeamUID == home) || (outcome == OutcomeAway && match.AwayTeamUID == home) {
			result.homeWins++
		} else {
			result.awayWins++
		}
		if match.HomeTeamUID == home {
			result.goalDifference += float64(*match.HomeScore - *match.AwayScore)
		} else {
			result.goalDifference += float64(*match.AwayScore - *match.HomeScore)
		}
	}
	return result
}

func leaguePositions(matches []MatchRecord, seasonID provider.SeasonID) map[provider.TeamUID]int {
	type standing struct {
		uid      provider.TeamUID
		points   int
		goalDiff int
		goalsFor int
	}
	standings := make(map[provider.TeamUID]*standing)
	for _, match := range matches {
		if match.SeasonID != seasonID || !match.Final() {
			continue
		}
		home := standings[match.HomeTeamUID]
		if home == nil {
			home = &standing{uid: match.HomeTeamUID}
			standings[match.HomeTeamUID] = home
		}
		away := standings[match.AwayTeamUID]
		if away == nil {
			away = &standing{uid: match.AwayTeamUID}
			standings[match.AwayTeamUID] = away
		}
		home.goalsFor += *match.HomeScore
		home.goalDiff += *match.HomeScore - *match.AwayScore
		away.goalsFor += *match.AwayScore
		away.goalDiff += *match.AwayScore - *match.HomeScore
		if *match.HomeScore > *match.AwayScore {
			home.points += 3
		} else if *match.HomeScore < *match.AwayScore {
			away.points += 3
		} else {
			home.points++
			away.points++
		}
	}
	ordered := make([]standing, 0, len(standings))
	for _, item := range standings {
		ordered = append(ordered, *item)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].points != ordered[j].points {
			return ordered[i].points > ordered[j].points
		}
		if ordered[i].goalDiff != ordered[j].goalDiff {
			return ordered[i].goalDiff > ordered[j].goalDiff
		}
		if ordered[i].goalsFor != ordered[j].goalsFor {
			return ordered[i].goalsFor > ordered[j].goalsFor
		}
		return ordered[i].uid < ordered[j].uid
	})
	result := make(map[provider.TeamUID]int, len(ordered))
	for index, item := range ordered {
		result[item.uid] = index + 1
	}
	return result
}

func previousSeasonFor(dataset Dataset, target MatchRecord) []MatchRecord {
	seasons := dataset.SeasonMap()
	current, exists := seasons[target.SeasonID]
	if !exists {
		current = SeasonRecord{ID: target.SeasonID, StartUTC: target.ScheduledAt}
	}
	var selectedID provider.SeasonID
	var selectedEnd time.Time
	for _, season := range dataset.Seasons {
		if season.ID == target.SeasonID || season.EndUTC.IsZero() || !season.EndUTC.Before(current.StartUTC) {
			continue
		}
		if selectedID == "" || season.EndUTC.After(selectedEnd) {
			selectedID = season.ID
			selectedEnd = season.EndUTC
		}
	}
	if selectedID == "" {
		return nil
	}
	result := make([]MatchRecord, 0)
	for _, match := range dataset.Matches {
		if match.SeasonID == selectedID && match.Final() && match.ScheduledAt.Before(current.StartUTC) {
			result = append(result, match)
		}
	}
	return result
}

func restDays(matches []MatchRecord, uid provider.TeamUID, cutoff time.Time) float64 {
	var last time.Time
	for _, match := range matches {
		if !match.HasTeam(uid) || !match.ScheduledAt.Before(cutoff) {
			continue
		}
		if last.IsZero() || match.ScheduledAt.After(last) {
			last = match.ScheduledAt
		}
	}
	if last.IsZero() {
		return 0
	}
	return math.Max(0, cutoff.Sub(last).Hours()/24)
}

func rate(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func average(numerator float64, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / float64(denominator)
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
