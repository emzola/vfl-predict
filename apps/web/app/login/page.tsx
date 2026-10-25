"use client";

import { FormEvent, useState } from "react";

export default function LoginPage() {
  const [email, setEmail] = useState("");
  const [status, setStatus] = useState<"idle" | "sending" | "sent" | "error">("idle");
  const [message, setMessage] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setStatus("sending");
    setMessage("");
    try {
      const response = await fetch("/api/v1/auth/request-link", {
        method: "POST",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify({ email, redirect: "/dashboard" }),
      });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(typeof body.error === "string" ? body.error : "Unable to request a sign-in link");
      setStatus("sent");
      setMessage(typeof body.message === "string" ? body.message : "Check your inbox for a sign-in link.");
    } catch (error) {
      setStatus("error");
      setMessage(error instanceof Error ? error.message : "Unable to request a sign-in link");
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-ink px-4 py-12">
      <section className="w-full max-w-md">
        <div className="mb-8 flex items-center gap-3">
          <span className="flex h-11 w-11 items-center justify-center rounded-2xl bg-accent text-lg font-black text-ink">V</span>
          <div><p className="font-bold tracking-wide text-white">BET9JA VFL</p><p className="text-xs text-slate-400">private analytics</p></div>
        </div>
        <div className="panel p-7 sm:p-9">
          <p className="eyebrow">Private access</p>
          <h1 className="mt-3 text-3xl font-semibold text-white">Sign in without a password</h1>
          <p className="mt-3 text-sm leading-6 text-slate-400">Enter the authorized email address. If it is configured for this private workspace, a single-use link will be sent.</p>
          <form onSubmit={submit} className="mt-8 space-y-4">
            <label className="block"><span className="mb-2 block text-sm font-medium text-slate-200">Email address</span><input required type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} className="w-full rounded-xl border border-slate-700 bg-slate-950/50 px-4 py-3 text-white outline-none ring-accent/50 placeholder:text-slate-600 focus:ring-2" placeholder="you@example.com" /></label>
            <button disabled={status === "sending"} className="w-full rounded-xl bg-accent px-4 py-3 font-semibold text-ink transition hover:bg-emerald-300 disabled:cursor-wait disabled:opacity-60">{status === "sending" ? "Sending…" : "Send sign-in link"}</button>
          </form>
          {message && <p role="status" className={"mt-5 rounded-xl border p-3 text-sm " + (status === "error" ? "border-danger/30 bg-danger/10 text-rose-200" : "border-accent/30 bg-accent/10 text-emerald-100")}>{message}</p>}
          <p className="mt-6 text-xs leading-5 text-slate-500">No public registration, passwords, social login, or guest mode. The dashboard does not place bets.</p>
        </div>
      </section>
    </main>
  );
}
