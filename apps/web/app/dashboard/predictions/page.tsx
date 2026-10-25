"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch, errorMessage, formatDate, percentage } from "../../../src/lib/api";
import { outcomeLabel, rankPredictions } from "../../../src/lib/predictions";
import type { Overview, PredictionPage } from "../../../src/lib/types";
import { ErrorState, FixtureLabel, LoadingState, PageHeading, ProbabilityBars, SecondaryGoalMarkets, StatusPill } from "../../../src/components/Ui";

export default function PredictionsPage() {
  const [overview, setOverview] = useState<Overview>();
  const [page, setPage] = useState<PredictionPage>();
  const [error, setError] = useState("");
  const refreshInFlightRef = useRef(false);

  const refreshCurrentRound = useCallback(async () => {
    if (refreshInFlightRef.current) return;
    refreshInFlightRef.current = true;
    try {
      const current = await apiFetch<Overview>("/api/v1/dashboard/overview");
      setOverview(current);
      const season = current.current_season?.season_id || "";
      if (!season || !current.current_round) {
        setPage({
          season_id: season || undefined,
          status: "unavailable",
          expected_fixture_count: 8,
          generated_fixture_count: 0,
          fixtures: [],
          complete: false,
          provenance: "No upcoming round is currently available.",
        });
        setError("");
        return;
      }
      const round = String(current.current_round.round);
      const query = new URLSearchParams();
      query.set("season_id", season);
      query.set("round", round);
      const value = await apiFetch<PredictionPage>("/api/v1/predictions?" + query.toString());
      setPage(value);
      setError("");
    } catch (value) {
      setError(errorMessage(value));
    } finally {
      refreshInFlightRef.current = false;
    }
  }, []);

  useEffect(() => {
    void refreshCurrentRound();
    const timer = window.setInterval(() => void refreshCurrentRound(), 30000);

    return () => {
      window.clearInterval(timer);
    };
  }, [refreshCurrentRound]);

  if (error && !page) return <><PageHeading eyebrow="Predictions" title="Round predictions" /><ErrorState message={error} /></>;
  if (!page) return <><PageHeading eyebrow="Predictions" title="Round predictions" description="Provisional and frozen probabilities are read from persisted prediction revisions." /><LoadingState /></>;
  const rankedPredictions = rankPredictions(page.fixtures);
  return (
    <>
      <PageHeading eyebrow="Predictions" title="Round predictions" description="The page automatically follows the next upcoming round after the current playing round. An official round is only complete when all eight fixtures are present and frozen before the earliest kickoff." />
      {error && <div className="mb-5"><ErrorState message={error} /></div>}
      <div className="mb-6 flex flex-wrap items-center gap-3"><StatusPill status={page.status || "unavailable"} /><span className="text-sm text-slate-400">{page.season_id ? "Season " + page.season_id : "No season"} · {page.round ? "Round " + page.round : "No round"} · {page.fixtures.length}/8 displayed</span>{page.model_version && <span className="text-xs text-slate-500">Policy {page.model_version}</span>}</div>
      {page.fixtures.length > 0 && <div className="mb-6 rounded-2xl border border-slate-800 bg-slate-950/20 p-4"><p className="eyebrow">Decision ranking</p><p className="mt-2 text-sm leading-6 text-slate-400">Fixtures are ordered by the strongest positive model-versus-market difference when available, then by the model probability for the selected outcome. This is a transparent review aid, not a profitability claim.</p></div>}
      {!page.complete && <div className="mb-6 rounded-2xl border border-warning/30 bg-warning/10 p-4 text-sm leading-6 text-amber-100">This is not a complete official round. The interface keeps it visibly provisional or unavailable and does not promote it to a frozen eight-fixture set.</div>}
      {rankedPredictions.length === 0 ? <div className="panel p-8 text-center text-sm text-slate-400">No stored predictions match this season and round.</div> : <div className="grid gap-4 md:grid-cols-2">{rankedPredictions.map((item) => <article key={item.prediction_revision_id} className="panel p-5"><div className="flex items-start justify-between gap-3"><div><FixtureLabel matchId={item.match_id} home={item.fixture.home_team} away={item.fixture.away_team} kickoff={item.fixture.scheduled_at} /><div className="mt-3 flex flex-wrap items-center gap-2"><span className="rounded-full border border-accent/30 bg-accent/10 px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wider text-accent">Priority #{item.decision_rank}</span><span className="text-xs text-slate-400">{outcomeLabel(item.decision_outcome)}</span>{item.decision_basis === "market_difference" && item.decision_market_difference !== undefined ? <span className="text-xs text-accent">+{percentage(item.decision_market_difference)} pp vs market</span> : <span className="text-xs text-slate-500">{percentage(item.decision_probability)} model probability</span>}</div></div><StatusPill status={item.status} /></div><div className="mt-5"><ProbabilityBars probability={item.probability} /></div><SecondaryGoalMarkets secondary={item.secondary} model={item.secondary_model} />{item.bookmaker && <div className="mt-5 rounded-xl border border-slate-800 bg-slate-950/30 p-3 text-xs"><div className="flex items-center justify-between gap-3"><p className="eyebrow">Observed 1X2 market</p><span className="text-slate-500">Overround {((item.bookmaker.overround - 1) * 100).toFixed(2)}%</span></div><div className="mt-3 grid grid-cols-3 gap-2 text-slate-300"><span>H {item.bookmaker.home_odds.toFixed(2)}</span><span>D {item.bookmaker.draw_odds.toFixed(2)}</span><span>A {item.bookmaker.away_odds.toFixed(2)}</span></div><div className="mt-2 text-slate-500">Market implied: {Math.round(item.bookmaker.implied.home * 100)}% / {Math.round(item.bookmaker.implied.draw * 100)}% / {Math.round(item.bookmaker.implied.away * 100)}%</div><div className="mt-1 text-slate-500">Model difference: {Math.round(item.bookmaker.model_difference.home * 100)} / {Math.round(item.bookmaker.model_difference.draw * 100)} / {Math.round(item.bookmaker.model_difference.away * 100)} percentage points</div></div>}{!item.bookmaker && <p className="mt-4 text-xs text-slate-500">No stored eligible 1X2 market was attached to this prediction.</p>}<div className="mt-5 grid grid-cols-2 gap-3 border-t border-slate-800 pt-4 text-xs"><div><p className="text-slate-500">Most likely</p><p className="mt-1 font-semibold capitalize text-white">{item.most_likely_outcome}</p></div><div><p className="text-slate-500">Generated</p><p className="mt-1 text-slate-300">{formatDate(item.prediction_as_of)}</p></div><div><p className="text-slate-500">Cutoff</p><p className="mt-1 text-slate-300">{formatDate(item.cutoff_at)}</p></div><div><p className="text-slate-500">Odds observation</p><p className="mt-1 text-slate-300">{formatDate(item.odds_observed_at)}</p></div></div>{!item.point_in_time_verified && <p className="mt-4 text-xs text-warning">Odds provenance is not verified as point-in-time.</p>}</article>)}</div>}
      <p className="mt-6 text-xs text-slate-500">Provenance: {page.provenance}</p>
      <p className="mt-2 text-xs text-slate-600">Auto-refreshes every 30 seconds.</p>
      {overview?.current_round && <p className="mt-2 text-xs text-slate-600">Next round kickoff: {formatDate(overview.current_round.earliest_kickoff_at)}</p>}
    </>
  );
}
