import type { Prediction, Probability } from "./types";

export type PredictionOutcome = keyof Probability;

export type RankedPrediction = Prediction & {
  decision_rank: number;
  decision_outcome: PredictionOutcome;
  decision_probability: number;
  decision_market_difference?: number;
  decision_basis: "market_difference" | "model_probability";
};

const outcomes: PredictionOutcome[] = ["home", "draw", "away"];

function finite(value: number | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function mostLikelyOutcome(probability: Probability): PredictionOutcome {
  return outcomes.reduce((best, outcome) => probability[outcome] > probability[best] ? outcome : best, "home" as PredictionOutcome);
}

function positiveMarketSignal(prediction: Prediction): { outcome: PredictionOutcome; difference: number } | undefined {
  if (!prediction.bookmaker || !prediction.point_in_time_verified) return undefined;
  const candidate = outcomes
    .map((outcome) => ({ outcome, difference: prediction.bookmaker?.model_difference[outcome] }))
    .filter((value): value is { outcome: PredictionOutcome; difference: number } => finite(value.difference) && value.difference > 0)
    .sort((left, right) => right.difference - left.difference)[0];
  return candidate;
}

/**
 * Orders fixtures for review without changing any stored prediction.
 * Positive model-vs-market differences receive priority. If no positive
 * difference is available, the strongest model probability is used instead.
 */
export function rankPredictions(predictions: Prediction[]): RankedPrediction[] {
  return predictions
    .map((prediction, originalIndex) => {
      const marketSignal = positiveMarketSignal(prediction);
      const likelyOutcome = mostLikelyOutcome(prediction.probability);
      const outcome = marketSignal?.outcome ?? likelyOutcome;
      const marketDifference = prediction.bookmaker?.model_difference[outcome];
      const decisionBasis: RankedPrediction["decision_basis"] = marketSignal ? "market_difference" : "model_probability";
      return {
        ...prediction,
        decision_rank: 0,
        decision_outcome: outcome,
        decision_probability: prediction.probability[outcome],
        decision_market_difference: finite(marketDifference) ? marketDifference : undefined,
        decision_basis: decisionBasis,
        _originalIndex: originalIndex,
      };
    })
    .sort((left, right) => {
      const leftMarket = left.decision_basis === "market_difference" ? left.decision_market_difference ?? 0 : -1;
      const rightMarket = right.decision_basis === "market_difference" ? right.decision_market_difference ?? 0 : -1;
      if (rightMarket !== leftMarket) return rightMarket - leftMarket;
      if (right.decision_probability !== left.decision_probability) return right.decision_probability - left.decision_probability;
      const kickoffOrder = new Date(left.fixture.scheduled_at).getTime() - new Date(right.fixture.scheduled_at).getTime();
      if (Number.isFinite(kickoffOrder) && kickoffOrder !== 0) return kickoffOrder;
      return left._originalIndex - right._originalIndex;
    })
    .map(({ _originalIndex: _ignored, ...prediction }, index) => ({ ...prediction, decision_rank: index + 1 }));
}

export function outcomeLabel(outcome: PredictionOutcome): string {
  return outcome === "home" ? "Home win" : outcome === "draw" ? "Draw" : "Away win";
}
