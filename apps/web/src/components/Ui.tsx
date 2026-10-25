import Link from "next/link";
import { formatDate, percentage } from "../lib/api";
import type { SecondaryGoalPrediction } from "../lib/types";

export function PageHeading({ eyebrow, title, description, action }: { eyebrow: string; title: string; description?: string; action?: React.ReactNode }) {
  return (
    <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <p className="eyebrow">{eyebrow}</p>
        <h1 className="mt-2 text-3xl font-semibold tracking-tight text-white">{title}</h1>
        {description && <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-400">{description}</p>}
      </div>
      {action}
    </div>
  );
}

export function LoadingState() {
  return <div className="panel flex min-h-40 items-center justify-center text-sm text-slate-400">Loading stored data…</div>;
}

export function ErrorState({ message }: { message: string }) {
  return <div className="rounded-2xl border border-danger/30 bg-danger/10 p-5 text-sm text-rose-200">{message}</div>;
}

export function EmptyState({ title, description }: { title: string; description: string }) {
  return <div className="panel p-8 text-center"><p className="text-lg font-semibold text-white">{title}</p><p className="mx-auto mt-2 max-w-lg text-sm text-slate-400">{description}</p></div>;
}

export function MetricCard({ label, value, detail, tone = "default" }: { label: string; value: string; detail?: string; tone?: "default" | "good" | "warn" }) {
  const color = tone === "good" ? "text-accent" : tone === "warn" ? "text-warning" : "text-white";
  return <div className="panel p-5"><p className="eyebrow">{label}</p><p className={"mt-3 text-2xl font-semibold " + color}>{value}</p>{detail && <p className="mt-2 text-xs text-slate-500">{detail}</p>}</div>;
}

export function StatusPill({ status }: { status: string }) {
  const normalized = status.toLowerCase();
  const style = normalized === "frozen" || normalized === "settled" ? "border-accent/30 bg-accent/10 text-accent" : normalized === "stale" || normalized === "missed_cutoff" ? "border-warning/30 bg-warning/10 text-warning" : normalized === "unavailable" ? "border-danger/30 bg-danger/10 text-rose-200" : "border-slate-600 bg-slate-800 text-slate-300";
  return <span className={"status-pill " + style}>{status.replaceAll("_", " ")}</span>;
}

export function ProbabilityBars({ probability }: { probability: { home: number; draw: number; away: number } }) {
  return <div className="space-y-2">
    {([["Home", probability.home], ["Draw", probability.draw], ["Away", probability.away]] as const).map(([label, value]) => (
      <div key={label} className="grid grid-cols-[3.5rem_1fr_3.5rem] items-center gap-2 text-xs">
        <span className="text-slate-400">{label}</span>
        <div className="h-2 overflow-hidden rounded-full bg-slate-800"><div className="h-full rounded-full bg-accent" style={{ width: percentage(value) }} /></div>
        <span className="text-right font-semibold text-slate-200">{percentage(value)}</span>
      </div>
    ))}
  </div>;
}

function validProbability(value: number | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 1;
}

function SecondaryProbability({ label, value }: { label: string; value?: number }) {
  if (!validProbability(value)) return null;
  return <div className="rounded-lg bg-slate-950/30 p-3"><div className="flex items-center justify-between gap-2 text-xs"><span className="text-slate-400">{label}</span><span className="font-semibold text-slate-200">{percentage(value)}</span></div><div className="mt-2 h-1.5 overflow-hidden rounded-full bg-slate-800"><div className="h-full rounded-full bg-accent/80" style={{ width: percentage(value) }} /></div></div>;
}

export function SecondaryGoalMarkets({ secondary, model }: { secondary?: SecondaryGoalPrediction; model?: string }) {
  if (!secondary) return null;
  const probabilities = [
    ["Over 2.5 goals", secondary.over_2_5],
    ["Under 2.5 goals", secondary.under_2_5],
    ["Both teams to score", secondary.both_teams_to_score],
    ["Neither team to score", secondary.neither_team_scores],
  ] as const;
  const availableProbabilities = probabilities.filter(([, value]) => validProbability(value));
  const correctScores = secondary.correct_scores && typeof secondary.correct_scores === "object"
    ? Object.entries(secondary.correct_scores).filter(([, value]) => validProbability(value)).sort((left, right) => right[1] - left[1]).slice(0, 3)
    : [];
  const hasExpectedGoals = Number.isFinite(secondary.lambda_home) && Number.isFinite(secondary.lambda_away);
  if (availableProbabilities.length === 0 && correctScores.length === 0 && !hasExpectedGoals) return null;
  const modelLabel = model === "poisson" ? "Poisson goal model" : model || "Goal model";
  return <div className="mt-5 rounded-xl border border-slate-800 bg-slate-950/30 p-3 text-xs"><div className="flex flex-wrap items-center justify-between gap-2"><p className="eyebrow">Secondary goal markets</p><span className="text-slate-500">{modelLabel}</span></div>{availableProbabilities.length > 0 && <div className="mt-3 grid gap-2 sm:grid-cols-2">{availableProbabilities.map(([label, value]) => <SecondaryProbability key={label} label={label} value={value} />)}</div>}{hasExpectedGoals && <p className="mt-3 text-slate-500">Expected goals: {secondary.lambda_home?.toFixed(2)} home · {secondary.lambda_away?.toFixed(2)} away</p>}{correctScores.length > 0 && <div className="mt-3 border-t border-slate-800 pt-3"><p className="text-slate-500">Top correct scores</p><div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-slate-300">{correctScores.map(([score, value]) => <span key={score}>{score} {percentage(value)}</span>)}</div></div>}<p className="mt-3 text-[11px] leading-5 text-slate-600">Model-derived probabilities; these are not bookmaker Over/Under prices.</p></div>;
}

export function FixtureLabel({ matchId, home, away, kickoff }: { matchId: number; home: string; away: string; kickoff: string }) {
  return <div><Link href={"/dashboard/matches/" + matchId} className="font-semibold text-white hover:text-accent">{home} <span className="text-slate-500">vs</span> {away}</Link><p className="mt-1 text-xs text-slate-500">{formatDate(kickoff)}</p></div>;
}

export function FreshnessNote({ timestamp, label = "Observed" }: { timestamp?: string; label?: string }) {
  return <p className="text-xs text-slate-500">{label}: {formatDate(timestamp)}</p>;
}
