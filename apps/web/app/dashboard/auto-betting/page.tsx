"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch, csrfToken, errorMessage, formatDate, percentage } from "../../../src/lib/api";
import type { AutoBettingState } from "../../../src/lib/types";
import { ErrorState, FixtureLabel, LoadingState, MetricCard, PageHeading, StatusPill } from "../../../src/components/Ui";

function naira(value?: number) {
  if (typeof value !== "number" || Number.isNaN(value)) return "—";
  return new Intl.NumberFormat(undefined, { style: "currency", currency: "NGN", maximumFractionDigits: 2 }).format(value);
}

export default function AutoBettingPage() {
  const [data, setData] = useState<AutoBettingState>();
  const [enabled, setEnabled] = useState(false);
  const [baseStake, setBaseStake] = useState(0);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const refreshInFlight = useRef(false);

  const load = useCallback(async () => {
    if (refreshInFlight.current) return;
    refreshInFlight.current = true;
    try {
      const value = await apiFetch<AutoBettingState>("/api/v1/auto-betting");
      setData(value);
      setEnabled(value.settings.enabled);
      setBaseStake(value.settings.base_stake);
      setError("");
    } catch (value) {
      setError(errorMessage(value));
    } finally {
      refreshInFlight.current = false;
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), 30000);
    return () => window.clearInterval(timer);
  }, [load]);

  async function save() {
    setSaved("");
    try {
      await apiFetch("/api/v1/auto-betting", {
        method: "PATCH",
        headers: { "X-CSRF-Token": csrfToken() },
        body: JSON.stringify({ enabled, base_stake: Number(baseStake) }),
      });
      setSaved("Auto betting settings saved.");
      await load();
    } catch (value) {
      setError(errorMessage(value));
    }
  }

  if (error && !data) return <><PageHeading eyebrow="Auto betting" title="Automated staking" /><ErrorState message={error} /></>;
  if (!data) return <><PageHeading eyebrow="Auto betting" title="Automated staking" description="This page controls the separate betting worker and the internal staking ledger." /><LoadingState /></>;

  const summary = data.season_summary;
  const candidate = data.next_candidate;

  return (
    <>
      <PageHeading
        eyebrow="Auto betting"
        title="Automated staking"
        description="Auto betting uses the frozen rank #1 fixture only, always targets Over 1.5 Goals, skips missing or low-price markets, and tracks profit from the internal ledger instead of wallet balances."
        action={<StatusPill status={data.runtime.state || (data.settings.enabled ? "idle" : "disabled")} />}
      />
      {error && <div className="mb-5"><ErrorState message={error} /></div>}

      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="Auto betting" value={data.settings.enabled ? "Enabled" : "Disabled"} detail={"Updated " + formatDate(data.settings.updated_at)} tone={data.settings.enabled ? "good" : "warn"} />
        <MetricCard label="Base stake" value={naira(data.settings.base_stake)} detail="Martingale resets to this amount after a win" />
        <MetricCard label="Season profit" value={naira(summary?.total_profit || 0)} detail={summary?.season_id ? "Season " + summary.season_id : "No active season"} tone={summary?.total_profit && summary.total_profit > 0 ? "good" : "default"} />
        <MetricCard label="Next stake" value={naira(summary?.next_stake || data.settings.base_stake)} detail={summary?.pending_settlements ? "Current stake is awaiting settlement" : "Next eligible round uses this stake"} tone={summary?.paused ? "warn" : "default"} />
      </section>

      <section className="mt-6 grid gap-6 xl:grid-cols-[0.9fr_1.1fr]">
        <div className="panel p-6">
          <p className="eyebrow">Controls</p>
          <div className="mt-5 space-y-4">
            <label className="flex items-start gap-3 rounded-xl border border-slate-800 bg-slate-950/20 p-4">
              <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-950 text-accent" />
              <span>
                <span className="block font-medium text-white">Enable auto betting</span>
                <span className="mt-1 block text-sm leading-6 text-slate-400">The worker will only place a bet when a frozen official round exists, the season profit target has not been reached, and no earlier placed bet is still awaiting settlement.</span>
              </span>
            </label>

            <label className="block text-sm text-slate-300">
              Starting stake amount
              <input
                type="number"
                min={1}
                step="100"
                value={baseStake}
                onChange={(event) => setBaseStake(Number(event.target.value))}
                className="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950/40 px-3 py-2 text-white"
              />
            </label>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="rounded-xl border border-slate-800 bg-slate-950/20 p-4 text-sm">
                <p className="text-slate-500">Fixed market</p>
                <p className="mt-1 font-semibold text-white">Over 1.5 Goals</p>
              </div>
              <div className="rounded-xl border border-slate-800 bg-slate-950/20 p-4 text-sm">
                <p className="text-slate-500">Minimum odds</p>
                <p className="mt-1 font-semibold text-white">{data.settings.minimum_odds.toFixed(2)}</p>
              </div>
              <div className="rounded-xl border border-slate-800 bg-slate-950/20 p-4 text-sm">
                <p className="text-slate-500">Profit target</p>
                <p className="mt-1 font-semibold text-white">{naira(data.settings.profit_target)}</p>
              </div>
              <div className="rounded-xl border border-slate-800 bg-slate-950/20 p-4 text-sm">
                <p className="text-slate-500">Martingale carry</p>
                <p className="mt-1 font-semibold text-white">Skipped rounds keep the pending next stake</p>
              </div>
            </div>

            <div>
              <button onClick={save} className="rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-ink">Save auto betting</button>
              {saved && <span className="ml-3 text-sm text-accent">{saved}</span>}
            </div>
          </div>
        </div>

        <div className="space-y-6">
          <div className="panel p-6">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <p className="eyebrow">Worker state</p>
                <p className="mt-2 text-xl font-semibold text-white">{data.runtime.state.replaceAll("_", " ")}</p>
              </div>
              <StatusPill status={data.runtime.state} />
            </div>
            <div className="mt-4 grid gap-3 text-sm text-slate-300 sm:grid-cols-2">
              <div><p className="text-slate-500">Last heartbeat</p><p className="mt-1">{formatDate(data.runtime.last_heartbeat_at)}</p></div>
              <div><p className="text-slate-500">Last login</p><p className="mt-1">{formatDate(data.runtime.last_login_at)}</p></div>
              <div><p className="text-slate-500">Current season / round</p><p className="mt-1">{data.runtime.current_season_id || data.current_season_id || "—"}{data.runtime.current_round ? " · Round " + data.runtime.current_round : ""}</p></div>
              <div><p className="text-slate-500">Worker id</p><p className="mt-1 break-all">{data.runtime.worker_id || "—"}</p></div>
            </div>
            {data.runtime.last_error && <p className="mt-4 rounded-xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-rose-200">{data.runtime.last_error}</p>}
            {summary?.paused && <p className="mt-4 rounded-xl border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-amber-100">{summary.pause_reason}</p>}
          </div>

          <div className="panel p-6">
            <p className="eyebrow">Season ledger</p>
            <div className="mt-4 grid gap-3 text-sm text-slate-300 sm:grid-cols-2">
              <div><p className="text-slate-500">Total staked</p><p className="mt-1 font-semibold text-white">{naira(summary?.total_staked || 0)}</p></div>
              <div><p className="text-slate-500">Total returned</p><p className="mt-1 font-semibold text-white">{naira(summary?.total_returned || 0)}</p></div>
              <div><p className="text-slate-500">Wins / losses</p><p className="mt-1">{summary?.bets_won || 0} / {summary?.bets_lost || 0}</p></div>
              <div><p className="text-slate-500">Skipped / pending</p><p className="mt-1">{summary?.bets_skipped || 0} / {summary?.pending_settlements || 0}</p></div>
            </div>
          </div>
        </div>
      </section>

      <section className="panel mt-6 p-6">
        <p className="eyebrow">Next candidate</p>
        {!candidate ? <p className="mt-4 text-sm text-slate-400">No bet is currently ready. The worker is waiting for an eligible frozen round, a settled previous bet, or a new season after a profit-target pause.</p> : (
          <div className="mt-4">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <FixtureLabel matchId={candidate.match_id} home={candidate.home_team} away={candidate.away_team} kickoff={candidate.scheduled_at} />
                <div className="mt-3 flex flex-wrap items-center gap-2">
                  <span className="rounded-full border border-accent/30 bg-accent/10 px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wider text-accent">Priority #{candidate.decision_rank}</span>
                  <span className="text-xs text-slate-400">{candidate.decision_basis === "market_difference" ? "Model-vs-market rank" : "Model probability rank"}</span>
                  {typeof candidate.decision_market_difference === "number" && <span className="text-xs text-accent">+{percentage(candidate.decision_market_difference)} pp</span>}
                </div>
              </div>
              <StatusPill status={candidate.status} />
            </div>
            <div className="mt-5 grid gap-3 text-sm text-slate-300 sm:grid-cols-2 lg:grid-cols-4">
              <div><p className="text-slate-500">Round</p><p className="mt-1">Season {candidate.season_id} · Round {candidate.round}</p></div>
              <div><p className="text-slate-500">Market</p><p className="mt-1 font-semibold text-white">Over 1.5 Goals</p></div>
              <div><p className="text-slate-500">Stake</p><p className="mt-1 font-semibold text-white">{naira(candidate.stake_amount)}</p></div>
              <div><p className="text-slate-500">Odds floor</p><p className="mt-1 font-semibold text-white">{candidate.minimum_odds.toFixed(2)}</p></div>
            </div>
          </div>
        )}
      </section>

      <section className="panel mt-6 p-6">
        <p className="eyebrow">Recent round decisions</p>
        {data.recent_rounds.length === 0 ? <p className="mt-4 text-sm text-slate-400">No auto betting decisions have been recorded yet.</p> : (
          <div className="mt-4 space-y-3">
            {data.recent_rounds.map((item) => (
              <div key={item.season_id + ":" + item.round + ":" + item.created_at} className="rounded-xl border border-slate-800 bg-slate-950/20 p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <p className="font-semibold text-white">{item.home_team} <span className="text-slate-500">vs</span> {item.away_team}</p>
                    <p className="mt-1 text-xs text-slate-500">Season {item.season_id} · Round {item.round} · recorded {formatDate(item.created_at)}</p>
                  </div>
                  <StatusPill status={item.status} />
                </div>
                <div className="mt-4 grid gap-3 text-sm text-slate-300 sm:grid-cols-2 lg:grid-cols-4">
                  <div><p className="text-slate-500">Stake</p><p className="mt-1">{naira(item.stake_amount)}</p></div>
                  <div><p className="text-slate-500">Quoted odds</p><p className="mt-1">{typeof item.quoted_odds === "number" ? item.quoted_odds.toFixed(2) : "—"}</p></div>
                  <div><p className="text-slate-500">Return</p><p className="mt-1">{naira(item.realized_return ?? item.potential_return)}</p></div>
                  <div><p className="text-slate-500">Profit</p><p className={"mt-1 " + ((item.realized_profit || 0) > 0 ? "text-accent" : (item.realized_profit || 0) < 0 ? "text-rose-200" : "text-slate-300")}>{naira(item.realized_profit)}</p></div>
                </div>
                {item.skip_reason && <p className="mt-3 text-sm text-amber-100">{item.skip_reason}</p>}
                {item.bet_reference && <p className="mt-3 text-xs text-slate-500">Reference: {item.bet_reference}</p>}
                {(item.placed_at || item.settled_at) && <p className="mt-2 text-xs text-slate-500">Placed {formatDate(item.placed_at)} · Settled {formatDate(item.settled_at)}</p>}
              </div>
            ))}
          </div>
        )}
      </section>

      <p className="mt-6 text-xs text-slate-600">Auto-refreshes every 30 seconds.</p>
    </>
  );
}
