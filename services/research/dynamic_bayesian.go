package research

import (
	"fmt"
	"math"
	"sort"
	"time"

	"bet9ja-vfl/services/provider"
)

const (
	// DynamicBayesianModelName is the explicit Phase 6 candidate key. It is
	// intentionally not the default production policy until promotion gates
	// have been satisfied.
	DynamicBayesianModelName    = "dynamic_bayesian_team_strength"
	DynamicBayesianModelVersion = "phase6.v1"
)

// DynamicBayesianConfig contains the deterministic approximate-Bayesian
// filtering parameters. The model uses a penalized Poisson log-likelihood
// updated online; the diagonal precisions provide an inexpensive posterior
// uncertainty estimate without per-round MCMC.
type DynamicBayesianConfig struct {
	SeasonCarryover             float64 `json:"season_carryover"`
	RatingK                     float64 `json:"rating_k"`
	RatingTemperature           float64 `json:"rating_temperature"`
	RatingHomeAdvantage         float64 `json:"rating_home_advantage"`
	RatingGoalDifferenceWeight  float64 `json:"rating_goal_difference_weight"`
	RatingGoalDifferenceCap     float64 `json:"rating_goal_difference_cap"`
	RatingToGoalScale           float64 `json:"rating_to_goal_scale"`
	AttackLearningRate          float64 `json:"attack_learning_rate"`
	DefenseLearningRate         float64 `json:"defense_learning_rate"`
	InterceptLearningRate       float64 `json:"intercept_learning_rate"`
	HomeAdvantageLearningRate   float64 `json:"home_advantage_learning_rate"`
	AttackRegularization        float64 `json:"attack_regularization"`
	DefenseRegularization       float64 `json:"defense_regularization"`
	InterceptRegularization     float64 `json:"intercept_regularization"`
	HomeAdvantageRegularization float64 `json:"home_advantage_regularization"`
	InitialIntercept            float64 `json:"initial_intercept"`
	InitialHomeAdvantage        float64 `json:"initial_home_advantage"`
	InitialVariance             float64 `json:"initial_variance"`
	ProcessVariance             float64 `json:"process_variance"`
	MaxScore                    int     `json:"max_score"`
}

func DefaultDynamicBayesianConfig() DynamicBayesianConfig {
	return DynamicBayesianConfig{
		SeasonCarryover:             0.20,
		RatingK:                     0.18,
		RatingTemperature:           1.00,
		RatingHomeAdvantage:         0.08,
		RatingGoalDifferenceWeight:  0.20,
		RatingGoalDifferenceCap:     1.50,
		RatingToGoalScale:           0.08,
		AttackLearningRate:          0.035,
		DefenseLearningRate:         0.035,
		InterceptLearningRate:       0.012,
		HomeAdvantageLearningRate:   0.012,
		AttackRegularization:        0.20,
		DefenseRegularization:       0.20,
		InterceptRegularization:     0.04,
		HomeAdvantageRegularization: 0.04,
		InitialIntercept:            math.Log(1.10),
		InitialHomeAdvantage:        math.Log(1.05),
		InitialVariance:             1.00,
		ProcessVariance:             0.03,
		// Zero requests the adaptive scoreline grid. Fixed grids remain
		// available to legacy Poisson callers for compatibility.
		MaxScore: 0,
	}
}

func (c DynamicBayesianConfig) normalize() DynamicBayesianConfig {
	defaults := DefaultDynamicBayesianConfig()
	if c.SeasonCarryover <= 0 || c.SeasonCarryover > 1 {
		c.SeasonCarryover = defaults.SeasonCarryover
	}
	if c.RatingK <= 0 {
		c.RatingK = defaults.RatingK
	}
	if c.RatingTemperature <= 0 {
		c.RatingTemperature = defaults.RatingTemperature
	}
	if c.RatingGoalDifferenceCap <= 0 {
		c.RatingGoalDifferenceCap = defaults.RatingGoalDifferenceCap
	}
	if c.RatingGoalDifferenceWeight < 0 {
		c.RatingGoalDifferenceWeight = defaults.RatingGoalDifferenceWeight
	}
	if c.RatingToGoalScale < 0 {
		c.RatingToGoalScale = defaults.RatingToGoalScale
	}
	if c.AttackLearningRate <= 0 {
		c.AttackLearningRate = defaults.AttackLearningRate
	}
	if c.DefenseLearningRate <= 0 {
		c.DefenseLearningRate = defaults.DefenseLearningRate
	}
	if c.InterceptLearningRate <= 0 {
		c.InterceptLearningRate = defaults.InterceptLearningRate
	}
	if c.HomeAdvantageLearningRate <= 0 {
		c.HomeAdvantageLearningRate = defaults.HomeAdvantageLearningRate
	}
	if c.AttackRegularization <= 0 {
		c.AttackRegularization = defaults.AttackRegularization
	}
	if c.DefenseRegularization <= 0 {
		c.DefenseRegularization = defaults.DefenseRegularization
	}
	if c.InterceptRegularization <= 0 {
		c.InterceptRegularization = defaults.InterceptRegularization
	}
	if c.HomeAdvantageRegularization <= 0 {
		c.HomeAdvantageRegularization = defaults.HomeAdvantageRegularization
	}
	if c.InitialIntercept == 0 {
		c.InitialIntercept = defaults.InitialIntercept
	}
	if c.InitialHomeAdvantage == 0 {
		c.InitialHomeAdvantage = defaults.InitialHomeAdvantage
	}
	if c.InitialVariance <= 0 {
		c.InitialVariance = defaults.InitialVariance
	}
	if c.ProcessVariance <= 0 {
		c.ProcessVariance = defaults.ProcessVariance
	}
	if c.MaxScore < 0 {
		c.MaxScore = defaults.MaxScore
	}
	return c
}

// DynamicTeamState is the auditable state for one canonical team identifier.
// Attack and defense are residual effects around the dynamic rating prior;
// they are not a second probability model that is averaged with the rating.
type DynamicTeamState struct {
	TeamUID         provider.TeamUID `json:"team_uid"`
	Rating          float64          `json:"rating"`
	RatingVariance  float64          `json:"rating_variance"`
	AttackResidual  float64          `json:"attack_residual"`
	DefenseResidual float64          `json:"defense_residual"`
	AttackVariance  float64          `json:"attack_variance"`
	DefenseVariance float64          `json:"defense_variance"`
	Matches         int              `json:"matches"`
	SeasonMatches   int              `json:"season_matches"`
}

// DynamicBayesianDiagnostics is stored with a prediction revision. The
// displayed ratings are descriptive state evidence; all market probabilities
// still originate from the scoreline matrix.
type DynamicBayesianDiagnostics struct {
	HomeRating             float64 `json:"home_rating"`
	AwayRating             float64 `json:"away_rating"`
	HomeRatingUncertainty  float64 `json:"home_rating_uncertainty"`
	AwayRatingUncertainty  float64 `json:"away_rating_uncertainty"`
	HomeAttackStrength     float64 `json:"home_attack_strength"`
	AwayAttackStrength     float64 `json:"away_attack_strength"`
	HomeDefenseStrength    float64 `json:"home_defense_strength"`
	AwayDefenseStrength    float64 `json:"away_defense_strength"`
	HomeAttackUncertainty  float64 `json:"home_attack_uncertainty"`
	AwayAttackUncertainty  float64 `json:"away_attack_uncertainty"`
	HomeDefenseUncertainty float64 `json:"home_defense_uncertainty"`
	AwayDefenseUncertainty float64 `json:"away_defense_uncertainty"`
	ExpectedHomeGoals      float64 `json:"expected_home_goals"`
	ExpectedAwayGoals      float64 `json:"expected_away_goals"`
	LeagueScoringIntercept float64 `json:"league_scoring_intercept"`
	HomeAdvantage          float64 `json:"home_advantage"`
	SeasonMatchesUsed      int     `json:"season_matches_used"`
	TotalMatchesUsed       int     `json:"total_matches_used"`
	StateAsOf              string  `json:"state_as_of,omitempty"`
}

// DynamicBayesianModel is a single coherent model: dynamic ratings provide
// evolving priors/covariates for a regularized Poisson attack/defense
// likelihood, and the posterior predictive rates produce one scoreline
// distribution. There is no post-hoc averaging of independent predictions.
type DynamicBayesianModel struct {
	Config          DynamicBayesianConfig
	States          map[provider.TeamUID]DynamicTeamState
	LeagueIntercept float64
	HomeAdvantage   float64
	CurrentSeason   provider.SeasonID
	LastMatchAt     time.Time
	TotalMatches    int
	SeasonMatches   int
	fitted          bool
}

func NewDynamicBayesianModel() *DynamicBayesianModel {
	config := DefaultDynamicBayesianConfig()
	return &DynamicBayesianModel{
		Config:          config,
		States:          make(map[provider.TeamUID]DynamicTeamState),
		LeagueIntercept: config.InitialIntercept,
		HomeAdvantage:   config.InitialHomeAdvantage,
	}
}

func (m *DynamicBayesianModel) Name() string    { return DynamicBayesianModelName }
func (m *DynamicBayesianModel) Version() string { return DynamicBayesianModelVersion }

func (m *DynamicBayesianModel) Fit(matches []MatchRecord) error {
	if m == nil {
		return fmt.Errorf("dynamic Bayesian model is nil")
	}
	m.Config = m.Config.normalize()
	m.States = make(map[provider.TeamUID]DynamicTeamState)
	m.LeagueIntercept = m.Config.InitialIntercept
	m.HomeAdvantage = m.Config.InitialHomeAdvantage
	m.CurrentSeason = ""
	m.LastMatchAt = time.Time{}
	m.TotalMatches = 0
	m.SeasonMatches = 0
	m.fitted = false
	ordered := append([]MatchRecord(nil), matches...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].ScheduledAt.Equal(ordered[j].ScheduledAt) {
			return ordered[i].ScheduledAt.Before(ordered[j].ScheduledAt)
		}
		return ordered[i].MatchID < ordered[j].MatchID
	})
	for _, match := range ordered {
		if !match.Final() || match.HomeTeamUID == 0 || match.AwayTeamUID == 0 || match.HomeTeamUID == match.AwayTeamUID {
			continue
		}
		if m.CurrentSeason != "" && match.SeasonID != m.CurrentSeason {
			m.resetForSeason(match.SeasonID)
		} else if m.CurrentSeason == "" {
			m.CurrentSeason = match.SeasonID
		}
		m.update(match)
		m.LastMatchAt = match.ScheduledAt.UTC()
	}
	m.fitted = true
	return nil
}

// PrepareSeason applies the configured season boundary even when no completed
// match from the new season is available yet. This is required for round-one
// predictions: fitting on prior seasons alone must not leave the model's
// state labeled as the prior season.
func (m *DynamicBayesianModel) PrepareSeason(seasonID provider.SeasonID) {
	if m == nil || seasonID == "" {
		return
	}
	if m.CurrentSeason == "" {
		m.CurrentSeason = seasonID
		m.SeasonMatches = 0
		return
	}
	if m.CurrentSeason != seasonID {
		m.resetForSeason(seasonID)
	}
}

func (m *DynamicBayesianModel) resetForSeason(next provider.SeasonID) {
	carry := m.Config.SeasonCarryover
	for uid, state := range m.States {
		state.Rating *= carry
		state.AttackResidual *= carry
		state.DefenseResidual *= carry
		state.RatingVariance = clamp(state.RatingVariance*carry*carry+m.Config.InitialVariance*(1-carry), 0.05, 9)
		state.AttackVariance = clamp(state.AttackVariance*carry*carry+m.Config.InitialVariance*(1-carry), 0.05, 9)
		state.DefenseVariance = clamp(state.DefenseVariance*carry*carry+m.Config.InitialVariance*(1-carry), 0.05, 9)
		state.SeasonMatches = 0
		m.States[uid] = state
	}
	// The league scoring level and home effect also evolve, but more slowly
	// across seasons so historical totals cannot overwhelm current evidence.
	m.LeagueIntercept = carry*m.LeagueIntercept + (1-carry)*m.Config.InitialIntercept
	m.HomeAdvantage = carry*m.HomeAdvantage + (1-carry)*m.Config.InitialHomeAdvantage
	m.CurrentSeason = next
	m.SeasonMatches = 0
}

func (m *DynamicBayesianModel) state(uid provider.TeamUID) DynamicTeamState {
	value, ok := m.States[uid]
	if !ok {
		value = DynamicTeamState{
			TeamUID:         uid,
			RatingVariance:  m.Config.InitialVariance,
			AttackVariance:  m.Config.InitialVariance,
			DefenseVariance: m.Config.InitialVariance,
		}
	}
	return value
}

func (m *DynamicBayesianModel) update(match MatchRecord) {
	home := m.state(match.HomeTeamUID)
	away := m.state(match.AwayTeamUID)
	lambdaHome, lambdaAway := m.expectedGoalsForStates(home, away)
	observedHome, observedAway := float64(*match.HomeScore), float64(*match.AwayScore)
	errorHome := clamp(observedHome-lambdaHome, -3, 3)
	errorAway := clamp(observedAway-lambdaAway, -3, 3)

	// Penalized Poisson MAP update. The rating-derived term is fixed at the
	// pre-match state for this observation; only attack/defense residuals are
	// updated from the scoring likelihood, avoiding a probability-level blend.
	home.AttackResidual += m.Config.AttackLearningRate * (errorHome - m.Config.AttackRegularization*home.AttackResidual)
	away.DefenseResidual += m.Config.DefenseLearningRate * (-errorHome - m.Config.DefenseRegularization*away.DefenseResidual)
	away.AttackResidual += m.Config.AttackLearningRate * (errorAway - m.Config.AttackRegularization*away.AttackResidual)
	home.DefenseResidual += m.Config.DefenseLearningRate * (-errorAway - m.Config.DefenseRegularization*home.DefenseResidual)
	m.LeagueIntercept += m.Config.InterceptLearningRate * (errorHome + errorAway - m.Config.InterceptRegularization*(m.LeagueIntercept-m.Config.InitialIntercept))
	m.HomeAdvantage += m.Config.HomeAdvantageLearningRate * (errorHome - m.Config.HomeAdvantageRegularization*(m.HomeAdvantage-m.Config.InitialHomeAdvantage))

	// Diagonal precision updates are an inexpensive Laplace-style uncertainty
	// approximation. Process variance prevents certainty from accumulating
	// permanently through a season reset or a short streak.
	home.AttackVariance = inversePrecision(home.AttackVariance, lambdaHome+m.Config.AttackRegularization, m.Config.ProcessVariance)
	away.DefenseVariance = inversePrecision(away.DefenseVariance, lambdaHome+m.Config.DefenseRegularization, m.Config.ProcessVariance)
	away.AttackVariance = inversePrecision(away.AttackVariance, lambdaAway+m.Config.AttackRegularization, m.Config.ProcessVariance)
	home.DefenseVariance = inversePrecision(home.DefenseVariance, lambdaAway+m.Config.DefenseRegularization, m.Config.ProcessVariance)

	// Opponent-adjusted Elo-style state update with capped, recency-aware goal
	// margin. This state is the prior/covariate for the same unified goal model.
	difference := home.Rating - away.Rating + m.Config.RatingHomeAdvantage
	expectedHomeResult := sigmoid(difference / m.Config.RatingTemperature)
	actualHomeResult := 0.5
	if observedHome > observedAway {
		actualHomeResult = 1
	} else if observedHome < observedAway {
		actualHomeResult = 0
	}
	margin := math.Log1p(math.Abs(observedHome - observedAway))
	margin = math.Min(margin, m.Config.RatingGoalDifferenceCap)
	marginMultiplier := 1 + m.Config.RatingGoalDifferenceWeight*margin
	ratingDelta := m.Config.RatingK * (actualHomeResult - expectedHomeResult) * marginMultiplier
	ratingDelta = clamp(ratingDelta, -m.Config.RatingK*2, m.Config.RatingK*2)
	home.Rating += ratingDelta
	away.Rating -= ratingDelta
	home.RatingVariance = inversePrecision(home.RatingVariance, expectedHomeResult*(1-expectedHomeResult)+0.05, m.Config.ProcessVariance)
	away.RatingVariance = inversePrecision(away.RatingVariance, expectedHomeResult*(1-expectedHomeResult)+0.05, m.Config.ProcessVariance)

	home.Matches++
	home.SeasonMatches++
	away.Matches++
	away.SeasonMatches++
	m.States[home.TeamUID] = home
	m.States[away.TeamUID] = away
	m.TotalMatches++
	m.SeasonMatches++
	m.recenterResiduals()
}

func inversePrecision(variance, observationPrecision, processVariance float64) float64 {
	variance = clamp(variance, 0.01, 16)
	if observationPrecision < 0.0001 {
		observationPrecision = 0.0001
	}
	posterior := 1 / (1/variance + observationPrecision)
	return clamp(posterior+processVariance, 0.01, 16)
}

func (m *DynamicBayesianModel) recenterResiduals() {
	if len(m.States) == 0 {
		return
	}
	attackMean, defenseMean := 0.0, 0.0
	uids := make([]provider.TeamUID, 0, len(m.States))
	for uid := range m.States {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	for _, uid := range uids {
		state := m.States[uid]
		attackMean += state.AttackResidual
		defenseMean += state.DefenseResidual
	}
	count := float64(len(uids))
	attackMean /= count
	defenseMean /= count
	for uid, state := range m.States {
		state.AttackResidual -= attackMean
		state.DefenseResidual -= defenseMean
		m.States[uid] = state
	}
	m.LeagueIntercept += attackMean - defenseMean
}

func (m *DynamicBayesianModel) expectedGoalsForStates(home, away DynamicTeamState) (float64, float64) {
	ratingDifference := home.Rating - away.Rating
	etaHome := m.LeagueIntercept + m.HomeAdvantage +
		m.Config.RatingToGoalScale*ratingDifference +
		home.AttackResidual - away.DefenseResidual
	etaAway := m.LeagueIntercept -
		m.Config.RatingToGoalScale*ratingDifference +
		away.AttackResidual - home.DefenseResidual
	return clamp(math.Exp(clamp(etaHome, -4, math.Log(8))), 0.05, 8),
		clamp(math.Exp(clamp(etaAway, -4, math.Log(8))), 0.05, 8)
}

func (m *DynamicBayesianModel) ExpectedGoals(match MatchRecord) (float64, float64) {
	if m == nil {
		return 1.1, 1.1
	}
	return m.expectedGoalsForStates(m.state(match.HomeTeamUID), m.state(match.AwayTeamUID))
}

func (m *DynamicBayesianModel) Predict(match MatchRecord) Probability {
	return m.PredictGoals(match, m.Config.MaxScore).Probability
}

func (m *DynamicBayesianModel) PredictGoals(match MatchRecord, maxScore int) GoalPrediction {
	homeLambda, awayLambda := m.ExpectedGoals(match)
	distribution, err := NewScorelineDistribution(homeLambda, awayLambda, maxScore)
	if err != nil {
		return GoalPrediction{Probability: NormalizeProbability(1, 1, 1), LambdaHome: homeLambda, LambdaAway: awayLambda}
	}
	return GoalPredictionFromScoreline(distribution)
}

func (m *DynamicBayesianModel) Diagnostics(match MatchRecord) DynamicBayesianDiagnostics {
	home := m.state(match.HomeTeamUID)
	away := m.state(match.AwayTeamUID)
	homeLambda, awayLambda := m.expectedGoalsForStates(home, away)
	result := DynamicBayesianDiagnostics{
		HomeRating:             home.Rating,
		AwayRating:             away.Rating,
		HomeRatingUncertainty:  math.Sqrt(clamp(home.RatingVariance, 0, 16)),
		AwayRatingUncertainty:  math.Sqrt(clamp(away.RatingVariance, 0, 16)),
		HomeAttackStrength:     m.Config.RatingToGoalScale*home.Rating + home.AttackResidual,
		AwayAttackStrength:     m.Config.RatingToGoalScale*away.Rating + away.AttackResidual,
		HomeDefenseStrength:    m.Config.RatingToGoalScale*home.Rating + home.DefenseResidual,
		AwayDefenseStrength:    m.Config.RatingToGoalScale*away.Rating + away.DefenseResidual,
		HomeAttackUncertainty:  math.Sqrt(clamp(home.AttackVariance, 0, 16)),
		AwayAttackUncertainty:  math.Sqrt(clamp(away.AttackVariance, 0, 16)),
		HomeDefenseUncertainty: math.Sqrt(clamp(home.DefenseVariance, 0, 16)),
		AwayDefenseUncertainty: math.Sqrt(clamp(away.DefenseVariance, 0, 16)),
		ExpectedHomeGoals:      homeLambda,
		ExpectedAwayGoals:      awayLambda,
		LeagueScoringIntercept: m.LeagueIntercept,
		HomeAdvantage:          m.HomeAdvantage,
		SeasonMatchesUsed:      m.SeasonMatches,
		TotalMatchesUsed:       m.TotalMatches,
	}
	if !m.LastMatchAt.IsZero() {
		result.StateAsOf = m.LastMatchAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func (m *DynamicBayesianModel) StateSnapshot() []DynamicTeamState {
	if m == nil {
		return nil
	}
	result := make([]DynamicTeamState, 0, len(m.States))
	for _, state := range m.States {
		result = append(result, state)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TeamUID < result[j].TeamUID })
	return result
}

func (m *DynamicBayesianModel) Parameters() map[string]any {
	if m == nil {
		return map[string]any{"model": DynamicBayesianModelName, "version": DynamicBayesianModelVersion}
	}
	return map[string]any{
		"model":               DynamicBayesianModelName,
		"version":             DynamicBayesianModelVersion,
		"configuration":       m.Config,
		"season_id":           m.CurrentSeason,
		"total_matches_used":  m.TotalMatches,
		"season_matches_used": m.SeasonMatches,
	}
}

func sigmoid(value float64) float64 {
	if value >= 0 {
		z := math.Exp(-value)
		return 1 / (1 + z)
	}
	z := math.Exp(value)
	return z / (1 + z)
}
