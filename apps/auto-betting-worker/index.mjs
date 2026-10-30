import os from "node:os";

import {
  Bet9jaAmbiguousError,
  Bet9jaApiClient,
  Bet9jaAuthError,
  buildBetPayload,
} from "./bet9ja-api.mjs";

const workerId = process.env.AUTOBET_WORKER_ID || `${os.hostname()}-${process.pid}`;
const intervalMs = numberEnv("AUTOBET_INTERVAL_SECONDS", 10, 2) * 1000;
const internalBaseURL = (process.env.INTERNAL_API_BASE_URL || "http://api:8090").replace(/\/+$/, "");
const internalToken = process.env.INTERNAL_API_TOKEN || "";
const dryRun = /^true$/i.test(process.env.AUTOBET_DRY_RUN || "false");
const heartbeatMs = numberEnv("AUTOBET_HEARTBEAT_SECONDS", 15, 5) * 1000;
const maxConsecutiveFailures = numberEnv("AUTOBET_MAX_FAILURES_BEFORE_RESTART", 3, 1);
const maxOddsAgeMs = numberEnv("AUTOBET_MAX_ODDS_AGE_SECONDS", 180, 5) * 1000;

const bookmakerID = numberEnv("BET9JA_BOOKMAKER_ID", 9478, 1);
const apiKey = process.env.BET9JA_API_KEY || "";
const limitID = numberEnv("BET9JA_LIMIT_ID", 1424, 1);
const selectionID = process.env.BET9JA_SELECTION_ID || "uof:6/sr:sport:1/18/12?total=1.5";
const productType = process.env.BET9JA_PRODUCT_TYPE || "VFLM";
const marketName = process.env.BET9JA_TICKET_MARKET_NAME || "Total Goals";
const outcomeName = process.env.BET9JA_TICKET_OUTCOME_NAME || "over 1.5";
const stakeScale = numberEnv("BET9JA_STAKE_SCALE", 10000, 1);
const oddsScale = numberEnv("BET9JA_ODDS_SCALE", 10000, 1);

const provider = new Bet9jaApiClient({
  loginURL: process.env.BET9JA_LOGIN_URL || "https://virtual.bet9ja.com/commonmenu/CommonDesktopMenu/login",
  initURL: process.env.BET9JA_INIT_URL || "https://virtual.bet9ja.com/betradardesktopmenu/IntegrationBetradar/getGames",
  ticketsBaseURL: process.env.BET9JA_TICKETS_BASE_URL || "https://rgspluslive-vs001.akamaized.net/betting-rest/bookmakers",
  bookmakerID,
  apiKey,
  timeoutMs: numberEnv("BET9JA_REQUEST_TIMEOUT_MS", 30000, 1000),
});
let lastRuntime = {};
let lastFailure = "";
let consecutiveFailures = 0;
let unconfirmedBet = null;
let stopping = false;

class StaleOddsError extends Error {
  constructor(message) {
    super(message);
    this.name = "StaleOddsError";
  }
}

class WorkerUnconfirmedBetError extends Error {
  constructor(message, candidate, ticketID = "") {
    super(message);
    this.name = "WorkerUnconfirmedBetError";
    this.seasonID = candidate?.season_id || "";
    this.round = candidate?.round || 0;
    this.matchID = candidate?.match_id || 0;
    this.ticketID = ticketID;
  }
}

async function main() {
  await reportRuntime({ state: "idle", last_error: "" });
  setInterval(() => void reportRuntime({ ...lastRuntime }), heartbeatMs).unref?.();

  while (!stopping) {
    let task = null;
    try {
      task = await apiRequest("/internal/v1/auto-betting/task");
      restoreUnconfirmedBet(task);
      if (task?.status === "disabled" || task?.settings?.enabled === false) {
        await handleTask(task);
      } else if (unconfirmedBet) {
        await reportRuntime({
          state: "error",
          current_season_id: task?.current_season_id || unconfirmedBet.seasonID,
          current_round: task?.candidate?.round || unconfirmedBet.round,
          current_match_id: task?.candidate?.match_id || unconfirmedBet.matchID,
          last_error: unconfirmedBet.message,
        });
      } else {
        await handleTask(task);
      }
    } catch (error) {
      if (error instanceof WorkerUnconfirmedBetError || error instanceof Bet9jaAmbiguousError) {
        unconfirmedBet = {
          seasonID: error.seasonID || task?.candidate?.season_id || "",
          round: error.round || task?.candidate?.round || 0,
          matchID: error.matchID || task?.candidate?.match_id || 0,
          ticketID: error.ticketID || "",
          message: `UNCONFIRMED_BET: ${normalizeError(error)}`,
        };
      }
      lastFailure = describeFailure(error, task);
      consecutiveFailures += 1;
      console.error(`[autobet] ${lastFailure}`);
      await reportRuntime({
        state: "error",
        current_season_id: task?.current_season_id || unconfirmedBet?.seasonID || "",
        current_round: task?.candidate?.round || unconfirmedBet?.round || 0,
        current_match_id: task?.candidate?.match_id || unconfirmedBet?.matchID || 0,
        last_error: unconfirmedBet?.message || lastFailure,
      });
      if (consecutiveFailures >= maxConsecutiveFailures && !(error instanceof WorkerUnconfirmedBetError)) {
        // The process is intentionally kept alive by Compose. Resetting only the
        // provider session gives transient failures a clean login without risking
        // a duplicate bet.
        provider.clearSession();
        consecutiveFailures = 0;
      }
    }
    if (!stopping) await sleep(intervalMs);
  }
}

async function handleTask(task) {
  const candidate = task?.candidate;
  const runtime = {
    current_season_id: task?.current_season_id || "",
    current_round: candidate?.round || 0,
    current_match_id: candidate?.match_id || 0,
    last_login_at: provider.lastLoginAt || undefined,
    last_error: unconfirmedBet?.message || lastFailure,
  };

  if (!task || task.status === "disabled" || task.settings?.enabled === false) {
    let logoutError = "";
    if (provider.authenticated) {
      try {
        await provider.logout();
      } catch (error) {
        logoutError = `Bet9ja logout failed: ${normalizeError(error)}`;
        console.error(`[autobet] ${logoutError}`);
      }
    } else {
      provider.clearSession();
    }
    await reportRuntime({
      ...runtime,
      state: task?.status || "disabled",
      current_url: "",
      last_error: logoutError || unconfirmedBet?.message || lastFailure,
    });
    return;
  }

  const credentials = await resolveCredentials();
  if (!credentials?.username || !credentials?.password) {
    await reportRuntime({
      ...runtime,
      state: "login_required",
      last_error: "Bet9ja credentials must be saved from the dashboard before auto betting can start.",
    });
    return;
  }

  if (provider.authenticated && !provider.credentialsMatch(credentials)) {
    // A dashboard credential update invalidates the in-memory provider session.
    // Best-effort logout is followed by a local clear even if the old session is
    // already gone upstream.
    await provider.logout().catch((error) => {
      console.error(`[autobet] previous Bet9ja session cleanup failed: ${normalizeError(error)}`);
    });
  }
  await provider.ensureLoggedIn(credentials);

  if (task.status !== "ready" || !candidate) {
    consecutiveFailures = 0;
    await reportRuntime({
      ...runtime,
      state: task.status || "idle",
      last_login_at: provider.lastLoginAt || undefined,
      last_error: lastFailure,
    });
    return;
  }
  if (!task.selection) {
    throw new Error("No current live Over 1.5 selection was available for the candidate match.");
  }

  await reportRuntime({
    ...runtime,
    state: "placing",
    last_login_at: provider.lastLoginAt || undefined,
    last_heartbeat_at: new Date().toISOString(),
  });

  const result = await placeCandidate(candidate, task.selection, credentials);
  if (result.action === "skip") {
    await apiRequest(`/internal/v1/auto-betting/rounds/${encodeURIComponent(candidate.season_id)}/${candidate.round}/skip`, {
      method: "POST",
      body: JSON.stringify({
        reason: result.reason,
        observed_odds: typeof result.observedOdds === "number" ? result.observedOdds : undefined,
      }),
    });
    lastFailure = "";
    consecutiveFailures = 0;
    await reportRuntime({
      ...runtime,
      state: "idle",
      last_login_at: provider.lastLoginAt || undefined,
      last_error: "",
      last_heartbeat_at: new Date().toISOString(),
    });
    return;
  }

  try {
    await apiRequest(`/internal/v1/auto-betting/rounds/${encodeURIComponent(candidate.season_id)}/${candidate.round}/placed`, {
      method: "POST",
      body: JSON.stringify({
        quoted_odds: result.quotedOdds,
        bet_reference: result.betReference || "",
      }),
    });
  } catch (error) {
    throw new WorkerUnconfirmedBetError(
      `Bet9ja accepted ticket ${result.betReference || "without a reference"}, but the internal ledger could not record it. Do not retry automatically: ${normalizeError(error)}`,
      candidate,
      result.betReference || "",
    );
  }

  lastFailure = "";
  consecutiveFailures = 0;
  await reportRuntime({
    ...runtime,
    state: "awaiting_settlement",
    last_login_at: provider.lastLoginAt || undefined,
    last_error: "",
    last_heartbeat_at: new Date().toISOString(),
  });
}

async function placeCandidate(candidate, selection, credentials) {
  const decimalOdds = Number(selection.decimal_odds);
  const minimumOdds = Number(candidate.minimum_odds || 0);
  if (Number(selection.market_id) !== 18 || selection.specifiers !== "total=1.5" || String(selection.outcome_id) !== "12") {
    throw new Error("The live selection did not match the configured Over 1.5 market.");
  }
  if (!Number.isFinite(decimalOdds) || decimalOdds <= 0) {
    throw new Error("The live Over 1.5 selection did not contain valid odds.");
  }
  if (selection.market_status !== undefined && Number(selection.market_status) !== 1) {
    return { action: "skip", reason: "The Over 1.5 market is not currently active.", observedOdds: decimalOdds };
  }
  if (selection.outcome_active !== undefined && Number(selection.outcome_active) !== 1) {
    return { action: "skip", reason: "The Over 1.5 outcome is not currently active.", observedOdds: decimalOdds };
  }
  const observedAt = Date.parse(selection.observed_at || selection.source_timestamp_at || "");
  if (!Number.isFinite(observedAt) || Date.now() - observedAt > maxOddsAgeMs) {
    throw new StaleOddsError(`Live Over 1.5 odds are stale or unavailable (maximum age ${Math.round(maxOddsAgeMs / 1000)} seconds).`);
  }
  if (decimalOdds < minimumOdds) {
    return {
      action: "skip",
      reason: `Over 1.5 odds ${decimalOdds.toFixed(2)} were below the configured minimum ${minimumOdds.toFixed(2)}.`,
      observedOdds: decimalOdds,
    };
  }

  const buildPayload = () => buildBetPayload(candidate, selection, {
    bookmakerID,
    sessionID: provider.sessionID,
    limitID,
    selectionID,
    productType,
    marketName,
    outcomeName,
    stakeScale,
    oddsScale,
  });
  let payload = buildPayload();
  if (dryRun) {
    return {
      action: "placed",
      quotedOdds: decimalOdds,
      betReference: "dry-run",
    };
  }

  let placed;
  try {
    placed = await provider.placeBet(payload);
  } catch (error) {
    if (!(error instanceof Bet9jaAuthError)) throw error;
    // Only an explicit authentication failure is safe to retry. Network
    // failures and malformed provider responses remain unconfirmed so a second
    // ticket cannot be created accidentally.
    await provider.login(credentials);
    payload = buildPayload();
    placed = await provider.placeBet(payload);
  }
  return {
    action: "placed",
    quotedOdds: decimalOdds,
    betReference: placed.ticketId,
  };
}

async function resolveCredentials() {
  const response = await fetch(`${internalBaseURL}/internal/v1/auto-betting/credentials`, {
    headers: {
      Accept: "application/json",
      ...(internalToken ? { "X-Internal-Token": internalToken } : {}),
    },
    signal: AbortSignal.timeout(numberEnv("INTERNAL_API_TIMEOUT_MS", 10000, 1000)),
  });
  if (response.status === 404) return null;
  if (!response.ok) {
    throw new Error(`Internal credentials API failed with HTTP ${response.status}.`);
  }
  const value = await response.json();
  return value?.username && value?.password ? value : null;
}

async function reportRuntime(payload) {
  const { last_heartbeat_at: _ignored, ...rest } = payload || {};
  lastRuntime = {
    worker_id: workerId,
    last_login_at: provider.lastLoginAt || undefined,
    ...rest,
  };
  try {
    await apiRequest("/internal/v1/auto-betting/runtime", {
      method: "POST",
      body: JSON.stringify({
        worker_id: workerId,
        last_heartbeat_at: new Date().toISOString(),
        ...(provider.lastLoginAt ? { last_login_at: provider.lastLoginAt } : {}),
        ...payload,
      }),
    });
  } catch {
    // Runtime reporting must not stop a provider session or a safe worker loop.
  }
}

async function apiRequest(pathname, init = {}) {
  const response = await fetch(`${internalBaseURL}${pathname}`, {
    ...init,
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      ...(internalToken ? { "X-Internal-Token": internalToken } : {}),
      ...(init.headers || {}),
    },
    signal: init.signal || AbortSignal.timeout(numberEnv("INTERNAL_API_TIMEOUT_MS", 10000, 1000)),
  });
  if (!response.ok) {
    throw new Error(`Internal API ${pathname} failed with HTTP ${response.status}.`);
  }
  return response.status === 204 ? null : response.json();
}

function describeFailure(error, task) {
  const candidate = task?.candidate;
  const round = candidate
    ? ` [season ${candidate.season_id} round ${candidate.round}: ${candidate.home_team} - ${candidate.away_team}]`
    : "";
  return `${new Date().toISOString()}${round} ${normalizeError(error)}`;
}

function restoreUnconfirmedBet(task) {
  if (unconfirmedBet) return;
  const runtime = task?.runtime;
  const message = String(runtime?.last_error || "");
  if (runtime?.state === "placing" && task?.candidate) {
    unconfirmedBet = {
      seasonID: task.candidate.season_id || runtime?.current_season_id || "",
      round: task.candidate.round || runtime?.current_round || 0,
      matchID: task.candidate.match_id || runtime?.current_match_id || 0,
      ticketID: "",
      message: "UNCONFIRMED_BET: worker restarted while a Bet9ja ticket request may have been in progress; reconcile the ticket before retrying.",
    };
    return;
  }
  if (runtime?.state !== "error" || !message.startsWith("UNCONFIRMED_BET:")) return;
  unconfirmedBet = {
    seasonID: task?.candidate?.season_id || runtime?.current_season_id || "",
    round: task?.candidate?.round || runtime?.current_round || 0,
    matchID: task?.candidate?.match_id || runtime?.current_match_id || 0,
    ticketID: "",
    message,
  };
}

function normalizeError(error) {
  return error instanceof Error ? error.message : String(error);
}

function numberEnv(name, fallback, minimum = 0) {
  const value = Number(process.env[name] || fallback);
  if (!Number.isFinite(value)) return fallback;
  return Math.max(minimum, value);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function shutdown(signal) {
  if (stopping) return;
  stopping = true;
  try {
    if (provider.authenticated) await provider.logout();
  } catch (error) {
    console.error(`[autobet] ${signal} shutdown logout failed: ${normalizeError(error)}`);
  }
  process.exit(0);
}

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => void shutdown(signal));
}

main().catch(async (error) => {
  console.error(`[autobet] fatal: ${normalizeError(error)}`);
  try {
    await reportRuntime({ state: "error", last_error: normalizeError(error) });
    if (provider.authenticated) await provider.logout();
  } catch {
    // Process is exiting; nothing further can be recovered here.
  }
  process.exitCode = 1;
});

// The container intentionally runs this module as its single long-lived process.
