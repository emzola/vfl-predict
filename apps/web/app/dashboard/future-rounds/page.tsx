"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch, errorMessage, formatDate } from "../../../src/lib/api";
import type { PredictionGroup } from "../../../src/lib/types";
import { EmptyState, ErrorState, FixtureLabel, LoadingState, PageHeading, ProbabilityBars, StatusPill } from "../../../src/components/Ui";

export default function FutureRoundsPage() {
  const [rounds, setRounds] = useState<PredictionGroup[]>();
  const [error, setError] = useState("");
  const refreshInFlight = useRef(false);
  const refresh = useCallback(async () => {
    if (refreshInFlight.current) return;
    refreshInFlight.current = true;
    try {
      const value = await apiFetch<{ rounds: PredictionGroup[] }>("/api/v1/dashboard/future-rounds");
      setRounds(value.rounds);
      setError("");
    } catch (value) {
      setError(errorMessage(value));
    } finally {
      refreshInFlight.current = false;
    }
  }, []);
  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), 30_000);
    return () => window.clearInterval(timer);
  }, [refresh]);
  if (error && !rounds) return <><PageHeading eyebrow="Future rounds" title="Provisional projections" /><ErrorState message={error} /></>;
  if (!rounds) return <><PageHeading eyebrow="Future rounds" title="Provisional projections" description="Future projections remain distinct from frozen official rounds." /><LoadingState /></>;
  return <><PageHeading eyebrow="Future rounds" title="Provisional projections" description="Only stored inputs are shown. A future projection does not imply that later results have been incorporated." action={<StatusPill status="provisional" />} />{error && <p className="mb-4 rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-200">Latest refresh failed: {error}</p>}{rounds.length === 0 ? <EmptyState title="No future projections" description="The engine has not stored a provisional prediction with sufficient inputs for a future round." /> : <div className="space-y-8">{rounds.map((page) => <section key={page.season_id + ":" + page.round}><div className="panel p-5"><div className="flex flex-wrap justify-between gap-3"><div><p className="eyebrow">Season {page.season_id} · Round {page.round}</p><p className="mt-2 text-sm text-slate-400">Generated {formatDate(page.prediction_as_of)} · {page.fixtures.length}/8 fixtures · Model {page.model_version || "—"}</p></div><StatusPill status={page.status || "provisional"} /></div></div><div className="mt-4 grid gap-4 md:grid-cols-2">{page.fixtures.map((item) => <article key={item.prediction_revision_id} className="panel p-5"><FixtureLabel matchId={item.match_id} home={item.fixture.home_team} away={item.fixture.away_team} kickoff={item.fixture.scheduled_at} /><div className="mt-5"><ProbabilityBars probability={item.probability} /></div><p className="mt-4 text-xs text-slate-500">Refresh is driven by the worker; browser reads do not poll Bet9ja.</p></article>)}</div></section>)}</div>}
  </>;
}
