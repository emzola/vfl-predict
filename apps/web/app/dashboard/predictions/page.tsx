"use client";

import { useEffect, useState } from "react";
import { apiFetch, errorMessage, formatDate } from "../../../src/lib/api";
import type { Overview, PredictionPage } from "../../../src/lib/types";
import { ErrorState, FixtureLabel, LoadingState, PageHeading, ProbabilityBars, StatusPill } from "../../../src/components/Ui";

export default function PredictionsPage() {
  const [overview, setOverview] = useState<Overview>();
  const [page, setPage] = useState<PredictionPage>();
  const [season, setSeason] = useState("");
  const [round, setRound] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    apiFetch<Overview>("/api/v1/dashboard/overview").then((value) => {
      setOverview(value);
      const nextSeason = value.current_season?.season_id || "";
      const nextRound = value.current_round?.round ? String(value.current_round.round) : "";
      setSeason(nextSeason);
      setRound(nextRound);
      return apiFetch<PredictionPage>("/api/v1/predictions?season_id=" + encodeURIComponent(nextSeason) + "&round=" + encodeURIComponent(nextRound));
    }).then(setPage).catch((value) => setError(errorMessage(value)));
  }, []);

  function loadSelected() {
    setError("");
    const query = new URLSearchParams();
    if (season) query.set("season_id", season);
    if (round) query.set("round", round);
    apiFetch<PredictionPage>("/api/v1/predictions?" + query.toString()).then(setPage).catch((value) => setError(errorMessage(value)));
  }

  if (error && !page) return <><PageHeading eyebrow="Predictions" title="Round predictions" /><ErrorState message={error} /></>;
  if (!page) return <><PageHeading eyebrow="Predictions" title="Round predictions" description="Provisional and frozen probabilities are read from persisted prediction revisions." /><LoadingState /></>;
  return (
    <>
      <PageHeading eyebrow="Predictions" title="Round predictions" description="An official round is only complete when all eight fixtures are present and frozen before the earliest kickoff." action={<div className="flex gap-2"><input aria-label="Season ID" value={season} onChange={(event) => setSeason(event.target.value)} placeholder="Season ID" className="w-32 rounded-lg border border-slate-700 bg-slate-950/50 px-3 py-2 text-sm text-white" /><input aria-label="Round" value={round} onChange={(event) => setRound(event.target.value)} placeholder="Round" className="w-20 rounded-lg border border-slate-700 bg-slate-950/50 px-3 py-2 text-sm text-white" /><button onClick={loadSelected} className="rounded-lg bg-accent px-3 py-2 text-sm font-semibold text-ink">Load</button></div>} />
      {error && <div className="mb-5"><ErrorState message={error} /></div>}
      <div className="mb-6 flex flex-wrap items-center gap-3"><StatusPill status={page.status || "unavailable"} /><span className="text-sm text-slate-400">{page.season_id ? "Season " + page.season_id : "No season"} · {page.round ? "Round " + page.round : "No round"} · {page.fixtures.length}/8 displayed</span>{page.model_version && <span className="text-xs text-slate-500">Model {page.model_version}</span>}</div>
      {!page.complete && <div className="mb-6 rounded-2xl border border-warning/30 bg-warning/10 p-4 text-sm leading-6 text-amber-100">This is not a complete official round. The interface keeps it visibly provisional or unavailable and does not promote it to a frozen eight-fixture set.</div>}
      {page.fixtures.length === 0 ? <div className="panel p-8 text-center text-sm text-slate-400">No stored predictions match this season and round.</div> : <div className="grid gap-4 md:grid-cols-2">{page.fixtures.map((item) => <article key={item.prediction_revision_id} className="panel p-5"><div className="flex items-start justify-between gap-3"><FixtureLabel matchId={item.match_id} home={item.fixture.home_team} away={item.fixture.away_team} kickoff={item.fixture.scheduled_at} /><StatusPill status={item.status} /></div><div className="mt-5"><ProbabilityBars probability={item.probability} /></div>{item.bookmaker && <div className="mt-5 rounded-xl border border-slate-800 bg-slate-950/30 p-3 text-xs"><div className="flex items-center justify-between gap-3"><p className="eyebrow">Observed 1X2 market</p><span className="text-slate-500">Overround {((item.bookmaker.overround - 1) * 100).toFixed(2)}%</span></div><div className="mt-3 grid grid-cols-3 gap-2 text-slate-300"><span>H {item.bookmaker.home_odds.toFixed(2)}</span><span>D {item.bookmaker.draw_odds.toFixed(2)}</span><span>A {item.bookmaker.away_odds.toFixed(2)}</span></div><div className="mt-2 text-slate-500">Market implied: {Math.round(item.bookmaker.implied.home * 100)}% / {Math.round(item.bookmaker.implied.draw * 100)}% / {Math.round(item.bookmaker.implied.away * 100)}%</div><div className="mt-1 text-slate-500">Model difference: {Math.round(item.bookmaker.model_difference.home * 100)} / {Math.round(item.bookmaker.model_difference.draw * 100)} / {Math.round(item.bookmaker.model_difference.away * 100)} percentage points</div></div>}{!item.bookmaker && <p className="mt-4 text-xs text-slate-500">No stored eligible 1X2 market was attached to this prediction.</p>}<div className="mt-5 grid grid-cols-2 gap-3 border-t border-slate-800 pt-4 text-xs"><div><p className="text-slate-500">Most likely</p><p className="mt-1 font-semibold capitalize text-white">{item.most_likely_outcome}</p></div><div><p className="text-slate-500">Generated</p><p className="mt-1 text-slate-300">{formatDate(item.prediction_as_of)}</p></div><div><p className="text-slate-500">Cutoff</p><p className="mt-1 text-slate-300">{formatDate(item.cutoff_at)}</p></div><div><p className="text-slate-500">Odds observation</p><p className="mt-1 text-slate-300">{formatDate(item.odds_observed_at)}</p></div></div>{!item.point_in_time_verified && <p className="mt-4 text-xs text-warning">Odds provenance is not verified as point-in-time.</p>}</article>)}</div>}
      <p className="mt-6 text-xs text-slate-500">Provenance: {page.provenance}</p>
      {overview?.current_round && <p className="mt-2 text-xs text-slate-600">Current round kickoff: {formatDate(overview.current_round.earliest_kickoff_at)}</p>}
    </>
  );
}
