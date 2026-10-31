import { describe, expect, it } from "vitest";
import { outcomeLabel, rankPredictions } from "./predictions";
import type { Bookmaker, BookmakerGoalMarket, Prediction, Probability } from "./types";

function bookmaker(modelDifference: Probability): Bookmaker {
  return {
    home_odds: 2,
    draw_odds: 3,
    away_odds: 4,
    implied: { home: 0.45, draw: 0.3, away: 0.25 },
    model_difference: modelDifference,
    overround: 1.08,
  };
}

function goalMarket(modelDifference: number, pointInTimeVerified = true): BookmakerGoalMarket {
  return {
    market_id: 18,
    specifiers: "total=2.5",
    over_odds: 1.7,
    under_odds: 2.3,
    implied: { over_2_5: 0.55, under_2_5: 0.45 },
    model_difference: modelDifference,
    overround: 1.05,
    point_in_time_verified: pointInTimeVerified,
  };
}

function prediction(matchId: number, probability: Probability, market?: Probability, goalDifference?: number, over25 = 0.5): Prediction {
  return {
    prediction_run_id: `run-${matchId}`,
    prediction_revision_id: `revision-${matchId}`,
    match_id: matchId,
    season_id: "season",
    round: 1,
    fixture: {
      match_id: matchId,
      season_id: "season",
      round: 1,
      scheduled_at: `2026-10-25T${String(10 + matchId).padStart(2, "0")}:00:00Z`,
      home_team_uid: matchId,
      home_team: `Home ${matchId}`,
      away_team_uid: matchId + 100,
      away_team: `Away ${matchId}`,
      status: "scheduled",
    },
    probability,
    most_likely_outcome: "home",
    model_version: "phase5.v1",
    status: "provisional",
    prediction_as_of: "2026-10-25T08:00:00Z",
    cutoff_at: "2026-10-25T09:00:00Z",
    earliest_kickoff_at: "2026-10-25T10:00:00Z",
    point_in_time_verified: Boolean(market),
    eligible: Boolean(market),
    created_at: "2026-10-25T08:00:00Z",
    bookmaker: market ? bookmaker(market) : undefined,
    bookmaker_goal_market: goalDifference === undefined ? undefined : goalMarket(goalDifference),
    secondary: { over_2_5: over25, under_2_5: 1 - over25, both_teams_to_score: 0.5, neither_team_scores: 0.5 },
  };
}

describe("prediction decision ranking", () => {
  it("prioritizes a positive model-versus-market difference", () => {
    const confident = prediction(1, { home: 0.8, draw: 0.1, away: 0.1 }, undefined, 0.06, 0.65);
    const marketSignal = prediction(2, { home: 0.2, draw: 0.2, away: 0.6 }, { home: -0.02, draw: 0.01, away: 0.12 }, 0.12, 0.72);

    const ranked = rankPredictions([confident, marketSignal]);

    expect(ranked.map((item) => item.match_id)).toEqual([2, 1]);
    expect(ranked[0].decision_basis).toBe("goal_market_difference");
    expect(ranked[0].decision_outcome).toBe("over_2_5");
    expect(ranked[0].decision_market_difference).toBe(0.12);
  });

  it("falls back to the Over 2.5 model probability when no positive edge exists", () => {
    const lower = prediction(1, { home: 0.42, draw: 0.3, away: 0.28 }, { home: -0.02, draw: 0, away: -0.01 }, -0.02, 0.42);
    const higher = prediction(2, { home: 0.64, draw: 0.2, away: 0.16 }, undefined, undefined, 0.64);

    const ranked = rankPredictions([lower, higher]);

    expect(ranked.map((item) => item.match_id)).toEqual([2, 1]);
    expect(ranked[0].decision_basis).toBe("model_probability");
    expect(outcomeLabel(ranked[0].decision_outcome)).toBe("Over 2.5 goals");
  });

  it("does not rank an unverified market difference as a decision signal", () => {
    const item = prediction(1, { home: 0.4, draw: 0.3, away: 0.3 }, { home: -0.02, draw: 0.01, away: 0.12 }, 0.12, 0.65);
    item.bookmaker_goal_market!.point_in_time_verified = false;

    const ranked = rankPredictions([item]);

    expect(ranked[0].decision_basis).toBe("model_probability");
    expect(ranked[0].decision_outcome).toBe("over_2_5");
  });

  it("does not mutate the API fixture order", () => {
    const input = [prediction(1, { home: 0.4, draw: 0.3, away: 0.3 }), prediction(2, { home: 0.7, draw: 0.2, away: 0.1 })];
    const originalIds = input.map((item) => item.match_id);

    rankPredictions(input);

    expect(input.map((item) => item.match_id)).toEqual(originalIds);
  });
});
