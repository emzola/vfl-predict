"use client";

import { useEffect, useState } from "react";
import { apiFetch, csrfToken, errorMessage, formatDate } from "../../../src/lib/api";
import { ErrorState, LoadingState, PageHeading, StatusPill } from "../../../src/components/Ui";

type Settings = { email: string; preferences: { timezone: string; theme: "dark" | "light" | "system"; refresh_interval_seconds: number; date_format: "iso" | "locale" } };
type Session = { session_id: string; created_at: string; last_seen_at: string; expires_at: string; current: boolean; revoked_at?: string };

export default function SettingsPage() {
  const [data, setData] = useState<Settings>();
  const [sessions, setSessions] = useState<Session[]>([]);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  useEffect(() => { Promise.all([apiFetch<Settings>("/api/v1/settings"), apiFetch<{ sessions: Session[] }>("/api/v1/auth/sessions")]).then(([settings, sessionData]) => { setData(settings); setSessions(sessionData.sessions); }).catch((value) => setError(errorMessage(value))); }, []);
  async function save() {
    if (!data) return;
    setSaved("");
    try {
      await apiFetch("/api/v1/settings", { method: "PATCH", headers: { "X-CSRF-Token": csrfToken() }, body: JSON.stringify(data.preferences) });
      setSaved("Preferences saved.");
    } catch (value) {
      setError(errorMessage(value));
    }
  }
  async function revoke(id: string) {
    try {
      await apiFetch("/api/v1/auth/sessions/" + encodeURIComponent(id) + "/revoke", { method: "POST", headers: { "X-CSRF-Token": csrfToken() } });
      setSessions((items) => items.map((item) => item.session_id === id ? { ...item, revoked_at: new Date().toISOString() } : item));
    } catch (value) {
      setError(errorMessage(value));
    }
  }
  if (error && !data) return <><PageHeading eyebrow="Settings" title="Private account" /><ErrorState message={error} /></>;
  if (!data) return <><PageHeading eyebrow="Settings" title="Private account" /><LoadingState /></>;
  return <><PageHeading eyebrow="Settings" title="Private account" description="Security-critical environment configuration is intentionally not exposed in the browser." />{error && <div className="mb-5"><ErrorState message={error} /></div>}<div className="grid gap-6 xl:grid-cols-2"><section className="panel p-6"><p className="eyebrow">Authorized account</p><p className="mt-3 text-lg font-semibold text-white">{data.email}</p><p className="mt-2 text-sm leading-6 text-slate-400">This identity is read-only. There is no public registration or password reset flow.</p><div className="mt-8 border-t border-slate-800 pt-6"><p className="eyebrow">Display preferences</p><div className="mt-4 space-y-4"><label className="block text-sm text-slate-300">Timezone<input value={data.preferences.timezone} onChange={(event) => setData({ ...data, preferences: { ...data.preferences, timezone: event.target.value } })} className="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950/40 px-3 py-2 text-white" /></label><label className="block text-sm text-slate-300">Theme<select value={data.preferences.theme} onChange={(event) => setData({ ...data, preferences: { ...data.preferences, theme: event.target.value as Settings["preferences"]["theme"] } })} className="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950/40 px-3 py-2 text-white"><option value="dark">Dark</option><option value="light">Light</option><option value="system">System</option></select></label><label className="block text-sm text-slate-300">Refresh interval (seconds)<input type="number" min={30} max={900} value={data.preferences.refresh_interval_seconds} onChange={(event) => setData({ ...data, preferences: { ...data.preferences, refresh_interval_seconds: Number(event.target.value) } })} className="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950/40 px-3 py-2 text-white" /></label><label className="block text-sm text-slate-300">Date format<select value={data.preferences.date_format} onChange={(event) => setData({ ...data, preferences: { ...data.preferences, date_format: event.target.value as Settings["preferences"]["date_format"] } })} className="mt-2 w-full rounded-lg border border-slate-700 bg-slate-950/40 px-3 py-2 text-white"><option value="iso">ISO-like</option><option value="locale">Locale</option></select></label><button onClick={save} className="rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-ink">Save preferences</button>{saved && <span className="ml-3 text-sm text-accent">{saved}</span>}</div></div></section><section className="panel p-6"><p className="eyebrow">Active sessions</p><p className="mt-3 text-sm leading-6 text-slate-400">Revoke sessions you no longer recognize. Session identifiers are public opaque IDs; secrets remain hashed server-side.</p><div className="mt-5 space-y-3">{sessions.map((item) => <div key={item.session_id} className="rounded-xl border border-slate-800 p-4"><div className="flex items-start justify-between gap-3"><div><p className="font-medium text-white">{item.current ? "Current session" : "Session"}</p><p className="mt-1 text-xs text-slate-500">Created {formatDate(item.created_at)} · last seen {formatDate(item.last_seen_at)}</p></div>{item.revoked_at ? <StatusPill status="revoked" /> : item.current ? <StatusPill status="active" /> : <button onClick={() => revoke(item.session_id)} className="text-xs text-warning underline-offset-4 hover:underline">Revoke</button>}</div></div>)}</div></section></div></>;
}
