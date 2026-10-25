"use client";

import { useCallback, useEffect, useState } from "react";
import { apiFetch, errorMessage, formatDate } from "../../src/lib/api";
import type { Overview } from "../../src/lib/types";
import { ErrorState, FixtureLabel, LoadingState, MetricCard, PageHeading, StatusPill } from "../../src/components/Ui";

export default function OverviewPage() {
  const [data, setData] = useState<Overview>();
  const [error, setError] = useState("");

  const load = useCallback(() => {
    apiFetch<Overview>("/api/v1/dashboard/overview").then(setData).catch((value) => setError(errorMessage(value)));
  }, []);

  useEffect(() => {
    load();
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") load();
    }, 60000);
    return () => window.clearInterval(timer);
  }, [load]);

  if (error && !data) return <><PageHeading eyebrow="Overview" title="Prediction workspace" /><ErrorState message={error} /></>;
  if (!data) return <><PageHeading eyebrow="Overview" title="Prediction workspace" description="Stored feed, prediction, and worker state." /><LoadingState /></>;

  const currentRound = data.current_round;
  const feedAt = typeof data.feed_freshness.latest_feed_observation === "string" ? data.feed_freshness.latest_feed_observation : undefined;
  return (
    <>
      <PageHeading eyebrow="Overview" title="Prediction workspace" description="A private view of the autonomous VFL engine. Dashboard reads never trigger provider polling." action={<span className="status-pill border-accent/30 bg-accent/10 text-accent">Read-only analytics</span>} />
      {error && <div className="mb-5"><ErrorState message={error} /></div>}
      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="Current season" value={data.current_season?.season_id || "Unavailable"} detail={data.current_season?.provider_year || "Provider season identity"} />
        <MetricCard label="Next matchday" value={currentRound ? "Round " + currentRound.round : "Unavailable"} detail={currentRound ? currentRound.fixture_count + " stored fixtures" : "No current round detected"} tone={currentRound?.fixture_count === 8 ? "good" : "warn"} />
        <MetricCard label="Frozen rounds" value={String(data.complete_frozen_rounds)} detail="Complete eight-fixture rounds" />
        <MetricCard label="Feed freshness" value={feedAt ? formatDate(feedAt) : "No observation"} detail="Stored observation timestamp" tone={feedAt ? "good" : "warn"} />
      </section>

      <section className="mt-6 grid gap-6 xl:grid-cols-[1.4fr_0.6fr]">
        <div className="panel p-6">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div><p className="eyebrow">Upcoming fixtures</p><h2 className="mt-2 text-xl font-semibold text-white">{currentRound ? "Round " + currentRound.round : "No verified upcoming round"}</h2></div>
            {currentRound && <StatusPill status={currentRound.state} />}
          </div>
          {data.upcoming_fixtures.length === 0 ? <p className="mt-8 text-sm text-slate-400">No upcoming fixtures are currently stored.</p> : <div className="mt-6 divide-y divide-slate-800">{data.upcoming_fixtures.slice(0, 8).map((fixture) => <div key={fixture.match_id} className="flex items-center justify-between gap-4 py-4"><FixtureLabel matchId={fixture.match_id} home={fixture.home_team} away={fixture.away_team} kickoff={fixture.scheduled_at} /><StatusPill status={fixture.status} /></div>)}</div>}
        </div>
        <div className="space-y-6">
          <div className="panel p-6"><p className="eyebrow">Prediction readiness</p><p className="mt-3 text-2xl font-semibold text-white">{currentRound?.fixture_count === 8 ? "Round-shaped data" : "Awaiting complete round"}</p><p className="mt-3 text-sm leading-6 text-slate-400">Official frozen predictions are only complete when all eight fixtures belong to the same round and the cutoff was respected.</p></div>
          <div className="panel p-6"><p className="eyebrow">Forward testing</p>{data.forward_testing.has_genuine_data ? <><p className="mt-3 text-2xl font-semibold text-accent">{data.forward_testing.settled_fixtures} settled</p><p className="mt-2 text-sm text-slate-400">Log loss {data.forward_testing.log_loss?.toFixed(4) || "—"} · Brier {data.forward_testing.brier_score?.toFixed(4) || "—"}</p></> : <><p className="mt-3 text-lg font-semibold text-warning">Pending genuine data</p><p className="mt-2 text-sm leading-6 text-slate-400">No genuine forward-observed settlements are available for a performance claim.</p></>}</div>
        </div>
      </section>

      <section className="mt-6 grid gap-6 lg:grid-cols-2">
        <div className="panel p-6"><p className="eyebrow">Active model</p>{data.active_model ? <div className="mt-3"><p className="text-lg font-semibold text-white">{String(data.active_model.model_key || "Model")}</p><p className="mt-1 text-sm text-slate-400">Version {String(data.active_model.version || "—")} · {String(data.active_model.promotion_status || "—")}</p></div> : <p className="mt-3 text-sm text-slate-400">No active artifact is registered. The bookmaker baseline remains the fallback.</p>}</div>
        <div className="panel p-6"><p className="eyebrow">Operational status</p><p className="mt-3 text-lg font-semibold text-white">{Number(data.worker_status[0]?.failed_jobs || 0) === 0 ? "No recorded worker failures" : String(data.worker_status[0]?.failed_jobs) + " failed jobs"}</p><p className="mt-1 text-sm text-slate-400">Checked {formatDate(data.checked_at)}</p></div>
      </section>
    </>
  );
}
