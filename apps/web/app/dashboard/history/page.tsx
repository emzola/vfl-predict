"use client";

import { useEffect, useState } from "react";
import { apiFetch, errorMessage, formatDate } from "../../../src/lib/api";
import type { Prediction } from "../../../src/lib/types";
import { EmptyState, ErrorState, FixtureLabel, LoadingState, PageHeading, StatusPill } from "../../../src/components/Ui";

export default function HistoryPage() {
  const [items, setItems] = useState<Prediction[]>();
  const [error, setError] = useState("");
  const [season, setSeason] = useState("");
  const [round, setRound] = useState("");
  const [status, setStatus] = useState("");
  const [model, setModel] = useState("");
  function load() {
    const query = new URLSearchParams();
    if (season) query.set("season_id", season);
    if (round) query.set("round", round);
    if (status) query.set("status", status);
    if (model) query.set("model", model);
    setError("");
    apiFetch<{ predictions: Prediction[] }>("/api/v1/dashboard/history?" + query.toString()).then((value) => setItems(value.predictions)).catch((value) => setError(errorMessage(value)));
  }
  useEffect(() => { load(); }, []);
  if (error && !items) return <><PageHeading eyebrow="History" title="Prediction archive" /><ErrorState message={error} /></>;
  if (!items) return <><PageHeading eyebrow="History" title="Prediction archive" description="Frozen revisions stay immutable; settlements and corrections are shown as stored." /><LoadingState /></>;
  return <><PageHeading eyebrow="History" title="Prediction archive" description="This archive separates the original prediction timestamp and cutoff from later settlement information." action={<div className="flex flex-wrap gap-2"><input aria-label="Season filter" value={season} onChange={(event) => setSeason(event.target.value)} placeholder="Season" className="w-28 rounded-lg border border-slate-700 bg-slate-950/50 px-3 py-2 text-sm text-white" /><input aria-label="Round filter" value={round} onChange={(event) => setRound(event.target.value)} placeholder="Round" className="w-20 rounded-lg border border-slate-700 bg-slate-950/50 px-3 py-2 text-sm text-white" /><select aria-label="Status filter" value={status} onChange={(event) => setStatus(event.target.value)} className="rounded-lg border border-slate-700 bg-slate-950/50 px-3 py-2 text-sm text-white"><option value="">Any status</option><option value="frozen">Frozen</option><option value="settled">Settled</option><option value="provisional">Provisional</option><option value="missed_cutoff">Missed cutoff</option></select><input aria-label="Model filter" value={model} onChange={(event) => setModel(event.target.value)} placeholder="Model" className="w-28 rounded-lg border border-slate-700 bg-slate-950/50 px-3 py-2 text-sm text-white" /><button onClick={load} className="rounded-lg bg-accent px-3 py-2 text-sm font-semibold text-ink">Filter</button></div>} />{error && <div className="mb-5"><ErrorState message={error} /></div>}{items.length === 0 ? <EmptyState title="No prediction history" description="Stored prediction revisions will appear here after an engine run." /> : <div className="panel overflow-hidden"><div className="overflow-x-auto"><table className="min-w-full text-left text-sm"><thead className="border-b border-slate-800 bg-slate-950/30 text-xs uppercase tracking-wider text-slate-500"><tr><th className="px-5 py-4">Fixture</th><th className="px-5 py-4">Status / model</th><th className="px-5 py-4">Probability</th><th className="px-5 py-4">Result</th><th className="px-5 py-4">Cutoff / odds</th></tr></thead><tbody className="divide-y divide-slate-800">{items.map((item) => <tr key={item.prediction_revision_id} className="align-top"><td className="px-5 py-4"><FixtureLabel matchId={item.match_id} home={item.fixture.home_team} away={item.fixture.away_team} kickoff={item.fixture.scheduled_at} /></td><td className="px-5 py-4"><StatusPill status={item.status} /><p className="mt-2 text-xs text-slate-500">{item.model_version}<br />Generated {formatDate(item.prediction_as_of)}</p></td><td className="px-5 py-4 text-xs leading-6 text-slate-300">H {Math.round(item.probability.home * 100)}% · D {Math.round(item.probability.draw * 100)}% · A {Math.round(item.probability.away * 100)}%</td><td className="px-5 py-4 text-xs text-slate-400">{item.fixture.home_score === undefined ? "Unsettled" : item.fixture.home_score + "–" + item.fixture.away_score}{item.actual_outcome ? <><br /><span className={item.correct === false ? "text-warning" : "text-accent"}>{item.correct === undefined ? "Settled" : item.correct ? "Correct" : "Incorrect"}</span></> : null}</td><td className="px-5 py-4 text-xs text-slate-400">{formatDate(item.cutoff_at)}<br />{item.point_in_time_verified ? "Verified observation" : "Not point-in-time verified"}<br />{formatDate(item.odds_observed_at)}</td></tr>)}</tbody></table></div></div>}</>;
}
