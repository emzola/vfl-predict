package research

import (
	"fmt"
	"math"
)

// ScorelineDistribution is the single joint distribution used by the Phase 6
// model for every supported goal market. Probabilities are normalized after
// truncation; TailMass records the unnormalized mass outside the stored grid so
// consumers can audit the numerical approximation.
type ScorelineDistribution struct {
	LambdaHome float64
	LambdaAway float64
	MaxGoals   int
	Matrix     [][]float64
	TailMass   float64
}

// NewScorelineDistribution builds an independent-Poisson joint scoreline
// distribution. The initial grid is expanded when the caller asks for an
// adaptive grid (maxGoals <= 0), and fixed grids retain their residual-tail
// diagnostic while normalizing the stored probabilities.
func NewScorelineDistribution(lambdaHome, lambdaAway float64, maxGoals int) (ScorelineDistribution, error) {
	if math.IsNaN(lambdaHome) || math.IsInf(lambdaHome, 0) || lambdaHome <= 0 {
		return ScorelineDistribution{}, fmt.Errorf("invalid home goal rate %v", lambdaHome)
	}
	if math.IsNaN(lambdaAway) || math.IsInf(lambdaAway, 0) || lambdaAway <= 0 {
		return ScorelineDistribution{}, fmt.Errorf("invalid away goal rate %v", lambdaAway)
	}
	adaptive := maxGoals <= 0
	if adaptive {
		maxGoals = 12
	}
	if adaptive {
		for maxGoals < 25 {
			homeTail := 1 - poissonMass(lambdaHome, maxGoals)
			awayTail := 1 - poissonMass(lambdaAway, maxGoals)
			if homeTail <= 1e-9 && awayTail <= 1e-9 {
				break
			}
			maxGoals++
		}
	}
	if maxGoals < 1 {
		maxGoals = 1
	}
	homePMF := poissonPMF(lambdaHome, maxGoals)
	awayPMF := poissonPMF(lambdaAway, maxGoals)
	matrix := make([][]float64, maxGoals+1)
	sum := 0.0
	for home := 0; home <= maxGoals; home++ {
		matrix[home] = make([]float64, maxGoals+1)
		for away := 0; away <= maxGoals; away++ {
			value := homePMF[home] * awayPMF[away]
			matrix[home][away] = value
			sum += value
		}
	}
	if sum <= 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
		return ScorelineDistribution{}, fmt.Errorf("scoreline mass is invalid")
	}
	for home := range matrix {
		for away := range matrix[home] {
			matrix[home][away] /= sum
		}
	}
	return ScorelineDistribution{
		LambdaHome: lambdaHome,
		LambdaAway: lambdaAway,
		MaxGoals:   maxGoals,
		Matrix:     matrix,
		TailMass:   clamp(1-sum, 0, 1),
	}, nil
}

func (d ScorelineDistribution) Validate(tolerance float64) error {
	if tolerance <= 0 {
		tolerance = 1e-9
	}
	if d.MaxGoals < 1 || len(d.Matrix) != d.MaxGoals+1 {
		return fmt.Errorf("scoreline grid has invalid dimensions")
	}
	sum := 0.0
	for home, row := range d.Matrix {
		if len(row) != d.MaxGoals+1 {
			return fmt.Errorf("scoreline row %d has invalid length", home)
		}
		for away, value := range row {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < -tolerance {
				return fmt.Errorf("scoreline[%d][%d] is invalid: %v", home, away, value)
			}
			sum += value
		}
	}
	if math.Abs(sum-1) > tolerance {
		return fmt.Errorf("scoreline probabilities sum to %.12f", sum)
	}
	return nil
}

// GoalPredictionFromScoreline derives all supported markets from one matrix.
// In particular, Over/Under and BTTS are not recomputed from independent
// approximations, so their complements and the 1X2 probabilities remain
// mathematically consistent.
func GoalPredictionFromScoreline(d ScorelineDistribution) GoalPrediction {
	correct := make(map[string]float64, len(d.Matrix)*len(d.Matrix))
	home, draw, away := 0.0, 0.0, 0.0
	over25, btts, expectedHome, expectedAway := 0.0, 0.0, 0.0, 0.0
	for homeGoals, row := range d.Matrix {
		for awayGoals, mass := range row {
			correct[fmt.Sprintf("%d-%d", homeGoals, awayGoals)] = mass
			expectedHome += float64(homeGoals) * mass
			expectedAway += float64(awayGoals) * mass
			switch {
			case homeGoals > awayGoals:
				home += mass
			case homeGoals == awayGoals:
				draw += mass
			default:
				away += mass
			}
			if homeGoals+awayGoals >= 3 {
				over25 += mass
			}
			if homeGoals >= 1 && awayGoals >= 1 {
				btts += mass
			}
		}
	}
	return GoalPrediction{
		Probability:       NormalizeProbability(home, draw, away),
		LambdaHome:        d.LambdaHome,
		LambdaAway:        d.LambdaAway,
		Over25:            clamp(over25, 0, 1),
		Under25:           clamp(1-over25, 0, 1),
		BothTeamsToScore:  clamp(btts, 0, 1),
		NeitherTeamScores: clamp(1-btts, 0, 1),
		CorrectScores:     correct,
		ResidualTail:      d.TailMass,
		ExpectedHomeGoals: expectedHome,
		ExpectedAwayGoals: expectedAway,
	}
}

func poissonMass(lambda float64, maxScore int) float64 {
	sum := 0.0
	for _, value := range poissonPMF(lambda, maxScore) {
		sum += value
	}
	return clamp(sum, 0, 1)
}

func scorelineGoalPrediction(lambdaHome, lambdaAway float64, maxScore int) GoalPrediction {
	distribution, err := NewScorelineDistribution(lambdaHome, lambdaAway, maxScore)
	if err != nil {
		return GoalPrediction{
			Probability: NormalizeProbability(1, 1, 1),
			LambdaHome:  lambdaHome,
			LambdaAway:  lambdaAway,
		}
	}
	return GoalPredictionFromScoreline(distribution)
}
