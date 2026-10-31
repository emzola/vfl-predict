import type { Prediction, Probability } from "./types";

export type PredictionOutcome = keyof Probability | "over_2_5";
type OneXTwoOutcome = keyof Probability;

export type RankedPrediction = Prediction & {
  decision_rank: number;
  decision_outcome: PredictionOutcome;
  decision_probability: number;
  decision_market_difference?: number;
  decision_basis: "goal_market_difference" | "model_probability";
};

function finite(value: number | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function mostLikelyOutcome(probability: Probability): OneXTwoOutcome {
  if (probability.draw >= probability.home && probability.draw >= probability.away) return "draw";
  if (probability.home >= probability.away) return "home";
  return "away";
}

function positiveGoalMarketSignal(prediction: Prediction): { outcome: "over_2_5"; difference: number } | undefined {
  const market = prediction.bookmaker_goal_market;
  if (!market || !market.point_in_time_verified || !finite(prediction.secondary?.over_2_5)) return undefined;
  if (!finite(market.model_difference) || market.model_difference <= 0) return undefined;
  return { outcome: "over_2_5", difference: market.model_difference };
}

function decisionProbability(prediction: Prediction, outcome: PredictionOutcome): number {
  if (outcome === "over_2_5") return prediction.secondary?.over_2_5 ?? 0;
  return prediction.probability[outcome];
}

/**
 * Orders fixtures for review without changing any stored prediction.
 * Positive Dynamic Bayesian Over 2.5-vs-market differences receive priority.
 * If no positive difference is available, the model probability is used for
 * the remaining frozen predictions as the ranking fallback.
 */
export function rankPredictions(predictions: Prediction[]): RankedPrediction[] {
  return predictions
    .map((prediction, originalIndex) => {
      const marketSignal = positiveGoalMarketSignal(prediction);
      const likelyOutcome: PredictionOutcome = finite(prediction.secondary?.over_2_5) ? "over_2_5" : mostLikelyOutcome(prediction.probability);
      const outcome = marketSignal?.outcome ?? likelyOutcome;
      const decisionBasis: RankedPrediction["decision_basis"] = marketSignal ? "goal_market_difference" : "model_probability";
      return {
        ...prediction,
        decision_rank: 0,
        decision_outcome: outcome,
        decision_probability: decisionProbability(prediction, outcome),
        decision_market_difference: marketSignal?.difference,
        decision_basis: decisionBasis,
        _originalIndex: originalIndex,
      };
    })
    .sort((left, right) => {
      const leftMarket = left.decision_basis === "goal_market_difference" ? left.decision_market_difference ?? 0 : -1;
      const rightMarket = right.decision_basis === "goal_market_difference" ? right.decision_market_difference ?? 0 : -1;
      if (rightMarket !== leftMarket) return rightMarket - leftMarket;
      if (right.decision_probability !== left.decision_probability) return right.decision_probability - left.decision_probability;
      const kickoffOrder = new Date(left.fixture.scheduled_at).getTime() - new Date(right.fixture.scheduled_at).getTime();
      if (Number.isFinite(kickoffOrder) && kickoffOrder !== 0) return kickoffOrder;
      return left._originalIndex - right._originalIndex;
    })
    .map(({ _originalIndex: _ignored, ...prediction }, index) => ({ ...prediction, decision_rank: index + 1 }));
}

export function outcomeLabel(outcome: PredictionOutcome): string {
  if (outcome === "over_2_5") return "Over 2.5 goals";
  return outcome === "home" ? "Home win" : outcome === "draw" ? "Draw" : "Away win";
}
