"use client";

import { useEffect, useState } from "react";
import { apiFetch, errorMessage } from "../../../src/lib/api";
import type { Metric } from "../../../src/lib/types";
import { EmptyState, ErrorState, LoadingState, MetricCard, PageHeading } from "../../../src/components/Ui";

type PaperData = { summary: { eligible_predictions: number; settled_fixtures: number; pending_fixtures: number; log_loss?: number; brier_score?: number; accuracy?: number; roi?: number; has_genuine_data: boolean; provenance: string }; metrics: Metric[] };

export default function PaperTestingPage() {
  const [data, setData] = useState<PaperData>();
  const [error, setError] = useState("");
  useEffect(() => { apiFetch<PaperData>("/api/v1/paper-testing").then(setData).catch((value) => setError(errorMessage(value))); }, []);
  if (error && !data) return <><PageHeading eyebrow="Paper testing" title="Forward evidence" /><ErrorState message={error} /></>;
  if (!data) return <><PageHeading eyebrow="Paper testing" title="Forward evidence" description="Only genuine pre-cutoff observations and forward settlements are shown here." /><LoadingState /></>;
  if (!data.summary.has_genuine_data) return <><PageHeading eyebrow="Paper testing" title="Forward evidence" description="Only genuine pre-cutoff observations and forward settlements are shown here." /><EmptyState title="No genuine settled forward data" description="The engine has not yet produced enough forward-observed settlements to calculate a meaningful performance result. Retrospective research stays on the Performance page." /></>;
  return <><PageHeading eyebrow="Paper testing" title="Forward evidence" description="Research-only paper testing. No automated betting or execution is connected." /><div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4"><MetricCard label="Settled fixtures" value={String(data.summary.settled_fixtures)} detail="Forward-observed settlement track" /><MetricCard label="Log loss" value={data.summary.log_loss?.toFixed(4) || "—"} detail="Lower is better" /><MetricCard label="Brier score" value={data.summary.brier_score?.toFixed(4) || "—"} detail="Lower is better" /><MetricCard label="Accuracy" value={data.summary.accuracy === undefined ? "—" : String(Math.round(data.summary.accuracy * 1000) / 10) + "%"} detail="Not sufficient alone for selection" /></div><div className="panel mt-6 p-6"><p className="eyebrow">Provenance and limits</p><p className="mt-3 text-sm leading-6 text-slate-300">{data.summary.provenance}. Any hypothetical returns require valid pre-cutoff observed odds and must not be interpreted as a guaranteed edge.</p></div></>;
}
