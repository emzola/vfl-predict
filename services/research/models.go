package research

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"bet9ja-vfl/services/provider"
)

type BaselineModel interface {
	Name() string
	Version() string
	Fit([]MatchRecord) error
	Predict(MatchRecord) Probability
}

type OverallFrequencyModel struct {
	Alpha  float64
	Counts [3]float64
}

func NewOverallFrequencyModel() *OverallFrequencyModel { return &OverallFrequencyModel{Alpha: 1} }

func (m *OverallFrequencyModel) Name() string    { return "overall_frequency" }
func (m *OverallFrequencyModel) Version() string { return "phase2.v1" }

func (m *OverallFrequencyModel) Fit(matches []MatchRecord) error {
	if m.Alpha <= 0 {
		m.Alpha = 1
	}
	m.Counts = [3]float64{}
	for _, match := range matches {
		outcome, ok := match.Outcome()
		if !ok {
			continue
		}
		m.Counts[outcomeIndex(outcome)]++
	}
	return nil
}

func (m *OverallFrequencyModel) Predict(MatchRecord) Probability {
	return NormalizeProbability(m.Counts[0]+m.Alpha, m.Counts[1]+m.Alpha, m.Counts[2]+m.Alpha)
}

// HomeDrawAwayFrequencyModel is kept as a named baseline because it is the
// explicit home/draw/away frequency benchmark in reports. It intentionally
// uses the same unconditional outcome sample as the overall-frequency
// benchmark; the distinction is a reporting label, not a claim of extra
// information.
type HomeDrawAwayFrequencyModel struct {
	Alpha  float64
	Counts [3]float64
}

func NewHomeDrawAwayFrequencyModel() *HomeDrawAwayFrequencyModel {
	return &HomeDrawAwayFrequencyModel{Alpha: 1}
}

func (m *HomeDrawAwayFrequencyModel) Name() string    { return "home_draw_away_frequency" }
func (m *HomeDrawAwayFrequencyModel) Version() string { return "phase2.v1" }

func (m *HomeDrawAwayFrequencyModel) Fit(matches []MatchRecord) error {
	if m.Alpha <= 0 {
		m.Alpha = 1
	}
	m.Counts = [3]float64{}
	for _, match := range matches {
		outcome, ok := match.Outcome()
		if ok {
			m.Counts[outcomeIndex(outcome)]++
		}
	}
	return nil
}

func (m *HomeDrawAwayFrequencyModel) Predict(MatchRecord) Probability {
	return NormalizeProbability(m.Counts[0]+m.Alpha, m.Counts[1]+m.Alpha, m.Counts[2]+m.Alpha)
}

type TeamStrengthModel struct {
	Ratings    map[provider.TeamUID]float64
	DrawRate   float64
	HomeAdv    float64
	Regularize float64
}

func NewTeamStrengthModel() *TeamStrengthModel {
	return &TeamStrengthModel{Ratings: make(map[provider.TeamUID]float64), Regularize: 0.25}
}

func (m *TeamStrengthModel) Name() string    { return "team_strength" }
func (m *TeamStrengthModel) Version() string { return "phase2.v1" }

func (m *TeamStrengthModel) Fit(matches []MatchRecord) error {
	type aggregate struct {
		matches, points, goalDifference int
	}
	aggregates := make(map[provider.TeamUID]*aggregate)
	draws := 0
	completed := 0
	homeAdvTotal := 0.0
	for _, match := range matches {
		if !match.Final() {
			continue
		}
		completed++
		if *match.HomeScore == *match.AwayScore {
			draws++
		}
		homeAdvTotal += float64(*match.HomeScore - *match.AwayScore)
		home := aggregates[match.HomeTeamUID]
		if home == nil {
			home = &aggregate{}
			aggregates[match.HomeTeamUID] = home
		}
		away := aggregates[match.AwayTeamUID]
		if away == nil {
			away = &aggregate{}
			aggregates[match.AwayTeamUID] = away
		}
		home.matches++
		away.matches++
		home.goalDifference += *match.HomeScore - *match.AwayScore
		away.goalDifference += *match.AwayScore - *match.HomeScore
		switch {
		case *match.HomeScore > *match.AwayScore:
			home.points += 3
		case *match.HomeScore < *match.AwayScore:
			away.points += 3
		default:
			home.points++
			away.points++
		}
	}
	m.Ratings = make(map[provider.TeamUID]float64, len(aggregates))
	for uid, value := range aggregates {
		pointsPerMatch := float64(value.points) / float64(maxInt(value.matches, 1))
		goalDifferencePerMatch := float64(value.goalDifference) / float64(maxInt(value.matches, 1))
		m.Ratings[uid] = 0.65*pointsPerMatch + 0.35*goalDifferencePerMatch
	}
	if completed > 0 {
		m.DrawRate = float64(draws) / float64(completed)
		m.HomeAdv = homeAdvTotal / float64(completed)
	} else {
		m.DrawRate = 1.0 / 3
		m.HomeAdv = 0
	}
	return nil
}

func (m *TeamStrengthModel) Predict(match MatchRecord) Probability {
	diff := m.Ratings[match.HomeTeamUID] - m.Ratings[match.AwayTeamUID]
	// Shrink ratings so a small historical sample cannot create a near-certain
	// probability. Draw is kept as an explicit mass rather than inferred as a
	// residual, which makes this baseline easy to audit.
	if m.Regularize <= 0 {
		m.Regularize = 0.25
	}
	strength := diff / (1 + m.Regularize)
	draw := clamp(m.DrawRate, 0.05, 0.60)
	drawScore := draw / (1 - draw)
	homeScore := math.Exp(clamp(m.HomeAdv*0.15+strength*0.20, -8, 8))
	awayScore := math.Exp(clamp(-strength*0.20, -8, 8))
	return NormalizeProbability(homeScore, drawScore, awayScore)
}

type PoissonGoalModel struct {
	BaseHomeGoals float64
	BaseAwayGoals float64
	AttackHome    map[provider.TeamUID]float64
	DefenseHome   map[provider.TeamUID]float64
	AttackAway    map[provider.TeamUID]float64
	DefenseAway   map[provider.TeamUID]float64
}

func NewPoissonGoalModel() *PoissonGoalModel {
	return &PoissonGoalModel{
		BaseHomeGoals: 1.2,
		BaseAwayGoals: 1.0,
		AttackHome:    make(map[provider.TeamUID]float64),
		DefenseHome:   make(map[provider.TeamUID]float64),
		AttackAway:    make(map[provider.TeamUID]float64),
		DefenseAway:   make(map[provider.TeamUID]float64),
	}
}

func (m *PoissonGoalModel) Name() string    { return "poisson" }
func (m *PoissonGoalModel) Version() string { return "phase2.v1" }

func (m *PoissonGoalModel) Fit(matches []MatchRecord) error {
	type teamGoals struct {
		homeGames, awayGames int
		homeFor, homeAgainst float64
		awayFor, awayAgainst float64
	}
	stats := make(map[provider.TeamUID]*teamGoals)
	get := func(uid provider.TeamUID) *teamGoals {
		value := stats[uid]
		if value == nil {
			value = &teamGoals{}
			stats[uid] = value
		}
		return value
	}
	var totalHome, totalAway float64
	completed := 0
	for _, match := range matches {
		if !match.Final() {
			continue
		}
		completed++
		homeGoals, awayGoals := float64(*match.HomeScore), float64(*match.AwayScore)
		totalHome += homeGoals
		totalAway += awayGoals
		home := get(match.HomeTeamUID)
		away := get(match.AwayTeamUID)
		home.homeGames++
		home.homeFor += homeGoals
		home.homeAgainst += awayGoals
		away.awayGames++
		away.awayFor += awayGoals
		away.awayAgainst += homeGoals
	}
	if completed == 0 {
		m.BaseHomeGoals = 1.2
		m.BaseAwayGoals = 1.0
		m.AttackHome = make(map[provider.TeamUID]float64)
		m.DefenseHome = make(map[provider.TeamUID]float64)
		m.AttackAway = make(map[provider.TeamUID]float64)
		m.DefenseAway = make(map[provider.TeamUID]float64)
		return nil
	}
	// Add-one smoothing prevents a team with a short or missing history from
	// producing a zero scoring rate.
	m.BaseHomeGoals = (totalHome + 1) / float64(completed+1)
	m.BaseAwayGoals = (totalAway + 1) / float64(completed+1)
	m.AttackHome = make(map[provider.TeamUID]float64, len(stats))
	m.DefenseHome = make(map[provider.TeamUID]float64, len(stats))
	m.AttackAway = make(map[provider.TeamUID]float64, len(stats))
	m.DefenseAway = make(map[provider.TeamUID]float64, len(stats))
	for uid, value := range stats {
		homeFor := (value.homeFor + 1) / float64(value.homeGames+1)
		homeAgainst := (value.homeAgainst + 1) / float64(value.homeGames+1)
		awayFor := (value.awayFor + 1) / float64(value.awayGames+1)
		awayAgainst := (value.awayAgainst + 1) / float64(value.awayGames+1)
		m.AttackHome[uid] = homeFor / m.BaseHomeGoals
		m.DefenseHome[uid] = homeAgainst / m.BaseAwayGoals
		m.AttackAway[uid] = awayFor / m.BaseAwayGoals
		m.DefenseAway[uid] = awayAgainst / m.BaseHomeGoals
	}
	return nil
}

func (m *PoissonGoalModel) ExpectedGoals(match MatchRecord) (float64, float64) {
	baseHome := positiveOr(m.BaseHomeGoals, 1.2)
	baseAway := positiveOr(m.BaseAwayGoals, 1.0)
	homeAttack := positiveOr(m.AttackHome[match.HomeTeamUID], 1)
	awayDefense := positiveOr(m.DefenseAway[match.AwayTeamUID], 1)
	awayAttack := positiveOr(m.AttackAway[match.AwayTeamUID], 1)
	homeDefense := positiveOr(m.DefenseHome[match.HomeTeamUID], 1)
	return clamp(baseHome*homeAttack*awayDefense, 0.05, 8), clamp(baseAway*awayAttack*homeDefense, 0.05, 8)
}

func (m *PoissonGoalModel) Predict(match MatchRecord) Probability {
	homeLambda, awayLambda := m.ExpectedGoals(match)
	return poissonOutcomeProbabilities(homeLambda, awayLambda, 20)
}

type GoalPrediction struct {
	Probability       Probability        `json:"probability"`
	LambdaHome        float64            `json:"lambda_home"`
	LambdaAway        float64            `json:"lambda_away"`
	ExpectedHomeGoals float64            `json:"expected_home_goals,omitempty"`
	ExpectedAwayGoals float64            `json:"expected_away_goals,omitempty"`
	Over25            float64            `json:"over_2_5"`
	Under25           float64            `json:"under_2_5"`
	BothTeamsToScore  float64            `json:"both_teams_to_score"`
	NeitherTeamScores float64            `json:"neither_team_scores"`
	CorrectScores     map[string]float64 `json:"correct_scores"`
	ResidualTail      float64            `json:"correct_score_residual_tail"`
}

func (m *PoissonGoalModel) PredictGoals(match MatchRecord, maxScore int) GoalPrediction {
	if maxScore < 1 {
		maxScore = 10
	}
	homeLambda, awayLambda := m.ExpectedGoals(match)
	return scorelineGoalPrediction(homeLambda, awayLambda, maxScore)
}

type BookmakerImpliedModel struct {
	AllowRetrospective bool
}

func NewBookmakerImpliedModel(allowRetrospective bool) *BookmakerImpliedModel {
	return &BookmakerImpliedModel{AllowRetrospective: allowRetrospective}
}

func (m *BookmakerImpliedModel) Name() string            { return "bookmaker_implied" }
func (m *BookmakerImpliedModel) Version() string         { return "phase2.v1" }
func (m *BookmakerImpliedModel) Fit([]MatchRecord) error { return nil }

func (m *BookmakerImpliedModel) Predict(match MatchRecord) Probability {
	if line, ok := FindOneXTwoOdds(match, m.AllowRetrospective); ok {
		return probabilitiesFromOdds(line)
	}
	return NormalizeProbability(1, 1, 1)
}

// BookmakerFirstModel is the production prediction policy. It uses a complete
// bookmaker 1X2 line only when the quotes satisfy the selected provenance
// policy; otherwise it falls back to the fitted Poisson model. The engine
// supplies target-match odds already bounded by the prediction cutoff, while
// the strict default here still requires point-in-time verification.
type BookmakerFirstModel struct {
	Statistical        *PoissonGoalModel
	AllowRetrospective bool
}

func NewBookmakerFirstModel(allowRetrospective bool) *BookmakerFirstModel {
	return &BookmakerFirstModel{
		Statistical:        NewPoissonGoalModel(),
		AllowRetrospective: allowRetrospective,
	}
}

func (m *BookmakerFirstModel) Name() string    { return "bookmaker_first" }
func (m *BookmakerFirstModel) Version() string { return "phase5.v1" }

func (m *BookmakerFirstModel) Fit(matches []MatchRecord) error {
	if m.Statistical == nil {
		m.Statistical = NewPoissonGoalModel()
	}
	return m.Statistical.Fit(matches)
}

func (m *BookmakerFirstModel) UsesBookmaker(match MatchRecord) bool {
	_, ok := FindOneXTwoOdds(match, m.AllowRetrospective)
	return ok
}

func (m *BookmakerFirstModel) Predict(match MatchRecord) Probability {
	if line, ok := FindOneXTwoOdds(match, m.AllowRetrospective); ok {
		return line.ImpliedProbabilities()
	}
	if m.Statistical == nil {
		m.Statistical = NewPoissonGoalModel()
	}
	return m.Statistical.Predict(match)
}

func (m *BookmakerFirstModel) PoissonGoals(match MatchRecord, maxScore int) GoalPrediction {
	if m.Statistical == nil {
		m.Statistical = NewPoissonGoalModel()
	}
	return m.Statistical.PredictGoals(match, maxScore)
}

type CombinedModel struct {
	Statistical        BaselineModel
	BookmakerWeight    float64
	AllowRetrospective bool
	lastOddsAvailable  bool
}

func NewCombinedModel(allowRetrospective bool) *CombinedModel {
	return &CombinedModel{
		Statistical:        NewPoissonGoalModel(),
		BookmakerWeight:    0.5,
		AllowRetrospective: allowRetrospective,
	}
}

func (m *CombinedModel) Name() string    { return "statistical_bookmaker_combination" }
func (m *CombinedModel) Version() string { return "phase2.v1" }

func (m *CombinedModel) Fit(matches []MatchRecord) error {
	if m.Statistical == nil {
		m.Statistical = NewPoissonGoalModel()
	}
	return m.Statistical.Fit(matches)
}

func (m *CombinedModel) Predict(match MatchRecord) Probability {
	statistical := m.Statistical.Predict(match)
	line, ok := FindOneXTwoOdds(match, m.AllowRetrospective)
	m.lastOddsAvailable = ok
	if !ok {
		return statistical
	}
	weight := clamp(m.BookmakerWeight, 0, 1)
	market := line.ImpliedProbabilities()
	return NormalizeProbability(
		(1-weight)*statistical.Home+weight*market.Home,
		(1-weight)*statistical.Draw+weight*market.Draw,
		(1-weight)*statistical.Away+weight*market.Away,
	)
}

func NewBaselineModel(name string, allowRetrospectiveOdds bool) (BaselineModel, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "overall_frequency", "overall":
		return NewOverallFrequencyModel(), nil
	case "home_draw_away_frequency", "hda_frequency":
		return NewHomeDrawAwayFrequencyModel(), nil
	case "team_strength":
		return NewTeamStrengthModel(), nil
	case "poisson":
		return NewPoissonGoalModel(), nil
	case "bookmaker_implied", "bookmaker":
		return NewBookmakerImpliedModel(allowRetrospectiveOdds), nil
	case "bookmaker_first", "market_first", "production":
		return NewBookmakerFirstModel(allowRetrospectiveOdds), nil
	case "statistical_bookmaker_combination", "combined":
		return NewCombinedModel(allowRetrospectiveOdds), nil
	case DynamicBayesianModelName, "dynamic_bayesian", "phase6":
		return NewDynamicBayesianModel(), nil
	default:
		return nil, fmt.Errorf("unknown baseline model %q", name)
	}
}

func BaselineModelNames() []string {
	return []string{"overall_frequency", "home_draw_away_frequency", "team_strength", "poisson", "bookmaker_implied", "statistical_bookmaker_combination", DynamicBayesianModelName}
}

func outcomeIndex(outcome OutcomeKey) int {
	switch outcome {
	case OutcomeHome:
		return 0
	case OutcomeDraw:
		return 1
	default:
		return 2
	}
}

func poissonGoalPrediction(homeLambda, awayLambda float64, maxScore int) GoalPrediction {
	return scorelineGoalPrediction(homeLambda, awayLambda, maxScore)
}

func poissonOutcomeProbabilities(homeLambda, awayLambda float64, maxScore int) Probability {
	homePMF := poissonPMF(homeLambda, maxScore)
	awayPMF := poissonPMF(awayLambda, maxScore)
	home, draw, away := 0.0, 0.0, 0.0
	for homeGoals := 0; homeGoals <= maxScore; homeGoals++ {
		for awayGoals := 0; awayGoals <= maxScore; awayGoals++ {
			mass := homePMF[homeGoals] * awayPMF[awayGoals]
			switch {
			case homeGoals > awayGoals:
				home += mass
			case homeGoals == awayGoals:
				draw += mass
			default:
				away += mass
			}
		}
	}
	return NormalizeProbability(home, draw, away)
}

func poissonPMF(lambda float64, maxScore int) []float64 {
	if maxScore < 0 {
		return nil
	}
	lambda = positiveOr(lambda, 1)
	result := make([]float64, maxScore+1)
	result[0] = math.Exp(-lambda)
	for score := 1; score <= maxScore; score++ {
		result[score] = result[score-1] * lambda / float64(score)
	}
	return result
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func positiveOr(value, fallback float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return fallback
	}
	return value
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func sortedTeamIDs(matches []MatchRecord) []provider.TeamUID {
	seen := make(map[provider.TeamUID]struct{})
	for _, match := range matches {
		seen[match.HomeTeamUID] = struct{}{}
		seen[match.AwayTeamUID] = struct{}{}
	}
	result := make([]provider.TeamUID, 0, len(seen))
	for uid := range seen {
		result = append(result, uid)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
