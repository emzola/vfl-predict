"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { apiFetch, csrfToken } from "../lib/api";

const navigation = [
  { href: "/dashboard", label: "Overview" },
  { href: "/dashboard/predictions", label: "Predictions" },
  { href: "/dashboard/future-rounds", label: "Future rounds" },
  { href: "/dashboard/history", label: "History" },
  { href: "/dashboard/performance", label: "Performance" },
  { href: "/dashboard/paper-testing", label: "Paper testing" },
  { href: "/dashboard/operations", label: "Operations" },
  { href: "/dashboard/auto-betting", label: "Auto betting" },
  { href: "/dashboard/settings", label: "Settings" },
];

export default function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const [email, setEmail] = useState("Private account");
  const [menuOpen, setMenuOpen] = useState(false);

  useEffect(() => {
    apiFetch<{ email: string }>("/api/v1/auth/me").then((value) => setEmail(value.email)).catch(() => undefined);
  }, []);

  async function logout() {
    await apiFetch("/api/v1/auth/logout", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken() },
    });
    window.location.href = "/login";
  }

  return (
    <div className="min-h-screen bg-ink">
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-64 border-r border-slate-800 bg-[#0b1728] px-5 py-6 lg:block">
        <Brand />
        <nav className="mt-10 space-y-1" aria-label="Main navigation">
          {navigation.map((item) => (
            <NavItem key={item.href} {...item} active={pathname === item.href} />
          ))}
        </nav>
        <div className="absolute bottom-6 left-5 right-5 border-t border-slate-800 pt-4">
          <p className="truncate text-xs text-slate-400" title={email}>{email}</p>
          <button onClick={logout} className="mt-3 text-sm text-slate-300 underline-offset-4 hover:text-accent hover:underline">
            Sign out
          </button>
        </div>
      </aside>

      <div className="lg:pl-64">
        <header className="sticky top-0 z-20 border-b border-slate-800/80 bg-ink/90 px-4 py-4 backdrop-blur lg:px-8">
          <div className="flex items-center justify-between">
            <div className="lg:hidden"><Brand compact /></div>
            <p className="hidden text-sm text-slate-400 lg:block">Private, auditable prediction workspace</p>
            <button className="rounded-lg border border-slate-700 px-3 py-2 text-sm text-slate-300 lg:hidden" onClick={() => setMenuOpen(!menuOpen)} aria-expanded={menuOpen}>
              Menu
            </button>
            <span className="hidden text-xs text-slate-500 sm:block">{email}</span>
          </div>
          {menuOpen && (
            <nav className="mt-4 grid gap-1 border-t border-slate-800 pt-4 lg:hidden" aria-label="Mobile navigation">
              {navigation.map((item) => (
                <NavItem key={item.href} {...item} active={pathname === item.href} onClick={() => setMenuOpen(false)} />
              ))}
              <button onClick={logout} className="px-3 py-2 text-left text-sm text-slate-300">Sign out</button>
            </nav>
          )}
        </header>
        <main className="mx-auto max-w-[1500px] px-4 py-6 lg:px-8 lg:py-8">{children}</main>
      </div>
    </div>
  );
}

function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <Link href="/dashboard" className="flex items-center gap-3">
      <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-accent font-black text-ink">V</span>
      {!compact && <span><span className="block text-sm font-bold tracking-wide text-white">BET9JA VFL</span><span className="block text-xs text-slate-400">private analytics</span></span>}
    </Link>
  );
}

function NavItem({ href, label, active, onClick }: { href: string; label: string; active: boolean; onClick?: () => void }) {
  return <Link href={href} onClick={onClick} className={"block rounded-xl px-3 py-2.5 text-sm transition " + (active ? "bg-accent/10 font-semibold text-accent" : "text-slate-300 hover:bg-slate-800 hover:text-white")}>{label}</Link>;
}
