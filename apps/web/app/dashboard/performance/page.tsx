"use client";

import { useEffect, useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { apiFetch, errorMessage } from "../../../src/lib/api";
import type { Metric } from "../../../src/lib/types";
import { EmptyState, ErrorState, LoadingState, PageHeading, StatusPill } from "../../../src/components/Ui";

export default function PerformancePage() {
  const [metrics, setMetrics] = useState<Metric[]>();
  const [error, setError] = useState("");
  useEffect(() => { apiFetch<{ metrics: Metric[] }>("/api/v1/performance").then((value) => setMetrics(value.metrics)).catch((value) => setError(errorMessage(value))); }, []);
  const chartData = useMemo(() => {
    const grouped = new Map<string, { model: string; log_loss?: number; brier_score?: number }>();
    (metrics || []).forEach((metric) => {
      const key = metric.model_key || metric.model_version || metric.run_id;
      const current = grouped.get(key) || { model: key };
      if (metric.metric === "log_loss") current.log_loss = metric.value;
      if (metric.metric === "brier_score") current.brier_score = metric.value;
      grouped.set(key, current);
    });
    return Array.from(grouped.values()).slice(0, 12);
  }, [metrics]);
  if (error && !metrics) return <><PageHeading eyebrow="Performance" title="Model comparison" /><ErrorState message={error} /></>;
  if (!metrics) return <><PageHeading eyebrow="Performance" title="Model comparison" description="Retrospective evaluation, forward observation, and offline verification remain separate datasets." /><LoadingState /></>;
  return <><PageHeading eyebrow="Performance" title="Model comparison" description="Bookmaker-implied probabilities remain the primary benchmark. Candidate research artifacts are never presented as production winners." /><div className="mb-6 rounded-2xl border border-warning/30 bg-warning/10 p-4 text-sm leading-6 text-amber-100">Historical odds retrieved after settlement are retrospective evidence unless an application observation before the cutoff is recorded. This page does not claim profitability or a LightGBM advantage.</div>{metrics.length === 0 ? <EmptyState title="No evaluation metrics" description="Run the research evaluation tools to populate model comparison metrics." /> : <><div className="panel p-5"><p className="eyebrow">Metric comparison</p><div className="mt-5 h-72 w-full"><ResponsiveContainer width="100%" height="100%"><BarChart data={chartData} margin={{ top: 8, right: 20, bottom: 8, left: 0 }}><CartesianGrid strokeDasharray="3 3" stroke="#25344b" /><XAxis dataKey="model" tick={{ fill: "#94a3b8", fontSize: 11 }} /><YAxis tick={{ fill: "#94a3b8", fontSize: 11 }} /><Tooltip contentStyle={{ background: "#101b2d", border: "1px solid #334155", borderRadius: 12 }} /><Bar dataKey="log_loss" fill="#65e6bb" name="Log loss" /><Bar dataKey="brier_score" fill="#f5c76a" name="Brier score" /></BarChart></ResponsiveContainer></div></div><div className="panel mt-6 overflow-hidden"><div className="overflow-x-auto"><table className="min-w-full text-left text-sm"><thead className="border-b border-slate-800 text-xs uppercase tracking-wider text-slate-500"><tr><th className="px-5 py-4">Model</th><th className="px-5 py-4">Metric</th><th className="px-5 py-4">Value</th><th className="px-5 py-4">Scope</th><th className="px-5 py-4">Status</th></tr></thead><tbody className="divide-y divide-slate-800">{metrics.slice(0, 200).map((metric, index) => <tr key={metric.run_id + metric.metric + metric.season_id + index}><td className="px-5 py-4 text-white">{metric.model_key || metric.model_version || "Unknown"}</td><td className="px-5 py-4 text-slate-300">{metric.metric}</td><td className="px-5 py-4 font-mono text-slate-200">{metric.value.toFixed(6)}</td><td className="px-5 py-4 text-slate-400">{metric.scope || "aggregate"} {metric.season_id ? "· " + metric.season_id : ""}</td><td className="px-5 py-4">{metric.promotion_status ? <StatusPill status={metric.promotion_status} /> : <span className="text-xs text-slate-500">research</span>}</td></tr>)}</tbody></table></div></div></>}</>;
}
