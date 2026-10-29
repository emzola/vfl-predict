import os from "node:os";
import path from "node:path";
import { chromium } from "playwright";

const workerId = process.env.AUTOBET_WORKER_ID || `${os.hostname()}-${process.pid}`;
const intervalMs = Math.max(10, Number(process.env.AUTOBET_INTERVAL_SECONDS || 30)) * 1000;
const internalBaseUrl = (process.env.INTERNAL_API_BASE_URL || "http://api:8090").replace(/\/+$/, "");
const internalToken = process.env.INTERNAL_API_TOKEN || "";
const virtualUrl = process.env.BET9JA_VIRTUAL_URL || "https://virtual.bet9ja.com/virtual/";
const tournamentName = process.env.BET9JA_VFL_TOURNAMENT_NAME || "Euro League";
const username = process.env.BET9JA_USERNAME || "";
const password = process.env.BET9JA_PASSWORD || "";
const headless = !/^false$/i.test(process.env.AUTOBET_HEADLESS || "true");
const dryRun = /^true$/i.test(process.env.AUTOBET_DRY_RUN || "false");
const userDataDir = process.env.AUTOBET_USER_DATA_DIR || path.join("/tmp", "autobet-browser");

const config = {
  loginTriggerSelectors: selectorList("BET9JA_LOGIN_TRIGGER_SELECTORS", [
    'button:has-text("Log In")',
    'button:has-text("Login")',
    'a:has-text("Login")',
    'text=/\\blog\\s*in\\b/i',
  ]),
  usernameSelectors: selectorList("BET9JA_USERNAME_SELECTORS", [
    'input[name="username"]',
    'input[name="userName"]',
    'input[placeholder*="username" i]',
    'input[type="text"]',
  ]),
  passwordSelectors: selectorList("BET9JA_PASSWORD_SELECTORS", [
    'input[name="password"]',
    'input[type="password"]',
    'input[placeholder*="password" i]',
  ]),
  loginSubmitSelectors: selectorList("BET9JA_LOGIN_SUBMIT_SELECTORS", [
    'button:has-text("Login")',
    'button:has-text("Log in")',
    'button:has-text("Sign in")',
    'text=/\\blog\\s*in\\b/i',
  ]),
  logoutIndicators: selectorList("BET9JA_LOGOUT_INDICATORS", [
    'text=/my\\s*account/i',
    'text=/make\\s*a\\s*deposit/i',
    'text=/logout/i',
    'text=/sign\\s*out/i',
    'text=/balance/i',
  ]),
  tournamentSelectors: selectorList("BET9JA_TOURNAMENT_SELECTORS", [
    'text="Euro League"',
    `text=${tournamentName}`,
    `text=/${escapeRegExp(tournamentName)}/i`,
  ]),
  matchRowSelectors: selectorList("BET9JA_MATCH_ROW_SELECTORS", [
    "tr",
    '[role="row"]',
    ".matches tr",
    ".table tr",
    "tbody tr",
  ]),
  matchExpandSelectors: selectorList("BET9JA_MATCH_EXPAND_SELECTORS", [
    'text=/^\\+\\d+$/',
    'text=/\\+\\d+/',
    '[aria-label*="more" i]',
    '[title*="more" i]',
  ]),
  overMarketSelectors: selectorList("BET9JA_OVER15_SELECTORS", [
    'text=/over\\s*1[\\.,]5/i',
    'button:has-text("Over 1.5")',
    'button:has-text("Over 1,5")',
  ]),
  stakeSelectors: selectorList("BET9JA_STAKE_SELECTORS", [
    'input[aria-label*="stake" i]',
    'input[name*="stake" i]',
    'input[placeholder*="stake" i]',
    'input[placeholder*="amount" i]',
    'input[type="number"]',
  ]),
  submitSelectors: selectorList("BET9JA_PLACE_BET_SELECTORS", [
    'button:has-text("Place Bet")',
    'button:has-text("Book Bet")',
    'button:has-text("Place bet")',
    'text=/place\\s*bet/i',
  ]),
  confirmationSelectors: selectorList("BET9JA_CONFIRM_SELECTORS", []),
  betReferenceSelectors: selectorList("BET9JA_BET_REFERENCE_SELECTORS", [
    'text=/reference/i',
    'text=/booking\\s*code/i',
  ]),
  betslipRemoveSelectors: selectorList("BET9JA_BETSLIP_REMOVE_SELECTORS", [
    'button[aria-label*="remove" i]',
    'button[title*="remove" i]',
    'text=/^x$/i',
  ]),
};

const browserState = {
  context: null,
  page: null,
};

async function main() {
  await reportRuntime({ state: "idle", worker_id: workerId, last_error: "" });
  while (true) {
    try {
      const task = await apiRequest("/internal/v1/auto-betting/task");
      await handleTask(task);
    } catch (error) {
      await reportRuntime({ state: "error", worker_id: workerId, last_error: normalizeError(error) });
    }
    await sleep(intervalMs);
  }
}

async function handleTask(task) {
  const runtime = {
    worker_id: workerId,
    current_season_id: task?.current_season_id || "",
    current_round: task?.candidate?.round || 0,
    current_match_id: task?.candidate?.match_id || 0,
    last_error: "",
  };
  if (!task || task.status !== "ready" || !task.candidate) {
    await reportRuntime({ ...runtime, state: task?.status || "idle" });
    return;
  }

  if (!username || !password) {
    await reportRuntime({ ...runtime, state: "login_required", last_error: "BET9JA_USERNAME and BET9JA_PASSWORD must be configured." });
    return;
  }

  const page = await ensureReadyPage(task.candidate);
  await reportRuntime({
    ...runtime,
    state: "placing",
    current_url: safeUrl(page),
    last_heartbeat_at: new Date().toISOString(),
  });

  const result = await placeCandidate(page, task.candidate);
  if (result.action === "skip") {
    await apiRequest(`/internal/v1/auto-betting/rounds/${encodeURIComponent(task.candidate.season_id)}/${task.candidate.round}/skip`, {
      method: "POST",
      body: JSON.stringify({
        reason: result.reason,
        observed_odds: typeof result.observedOdds === "number" ? result.observedOdds : undefined,
      }),
    });
    await reportRuntime({
      ...runtime,
      state: "idle",
      current_url: safeUrl(page),
      last_heartbeat_at: new Date().toISOString(),
    });
    return;
  }

  await apiRequest(`/internal/v1/auto-betting/rounds/${encodeURIComponent(task.candidate.season_id)}/${task.candidate.round}/placed`, {
    method: "POST",
    body: JSON.stringify({
      quoted_odds: result.quotedOdds,
      bet_reference: result.betReference || "",
    }),
  });
  await reportRuntime({
    ...runtime,
    state: "awaiting_settlement",
    current_url: safeUrl(page),
    last_heartbeat_at: new Date().toISOString(),
  });
}

async function ensureReadyPage(candidate) {
  const page = await ensureBrowser();
  await page.goto(virtualUrl, { waitUntil: "domcontentloaded" });
  await shortWait();
  if (!(await hasActiveSession(page))) {
    await login(page);
  }
  await ensureTournament(page, candidate);
  return page;
}

async function ensureBrowser() {
  if (browserState.context && browserState.page && !browserState.page.isClosed()) {
    return browserState.page;
  }
  browserState.context = await chromium.launchPersistentContext(userDataDir, {
    headless,
    viewport: { width: 1440, height: 1000 },
    args: ["--disable-dev-shm-usage", "--no-sandbox"],
  });
  browserState.page = browserState.context.pages()[0] || await browserState.context.newPage();
  return browserState.page;
}

async function hasActiveSession(page) {
  const logoutIndicator = await firstExisting(page, config.logoutIndicators);
  if (logoutIndicator) return true;
  const passwordField = await firstExisting(page, config.passwordSelectors);
  if (passwordField) return false;
  return false;
}

async function login(page) {
  const trigger = await firstExisting(page, config.loginTriggerSelectors);
  if (trigger) {
    await trigger.click();
    await shortWait();
  }
  const userField = await waitForExisting(page, config.usernameSelectors, 10000);
  const passwordField = await waitForExisting(page, config.passwordSelectors, 10000);
  if (!userField || !passwordField) {
    throw new Error("Bet9ja login form was not found.");
  }
  await userField.fill(username);
  await passwordField.fill(password);
  const submit = await firstExisting(page, config.loginSubmitSelectors);
  if (!submit) {
    throw new Error("Bet9ja login submit button was not found.");
  }
  await submit.click();
  await page.waitForLoadState("domcontentloaded").catch(() => undefined);
  await shortWait();
  if (!(await hasActiveSession(page))) {
    throw new Error("Bet9ja login did not produce an active session.");
  }
  await reportRuntime({
    state: "ready",
    worker_id: workerId,
    current_url: safeUrl(page),
    last_login_at: new Date().toISOString(),
    last_heartbeat_at: new Date().toISOString(),
  });
}

async function ensureTournament(page, candidate) {
  const selector = await firstExisting(page, config.tournamentSelectors);
  if (selector) {
    await selector.scrollIntoViewIfNeeded().catch(() => undefined);
    await selector.click().catch(() => undefined);
    await shortWait();
  }
  if (candidate?.home_team) {
    await page.getByText(new RegExp(escapeRegExp(candidate.home_team), "i")).first().waitFor({ timeout: 5000 }).catch(() => undefined);
  }
}

async function placeCandidate(page, candidate) {
  await clearBetslip(page);

  const row = await findMatchRow(page, candidate);
  if (!row) {
    return { action: "skip", reason: "Match row was not found on the virtual page." };
  }

  let market = await findOverOnePointFiveOption(row);
  if (!market) {
    await expandMatchRow(row);
    market = await findOverOnePointFiveOption(row);
  }
  if (!market) {
    return { action: "skip", reason: "Over 1.5 market was not available for the selected match." };
  }

  const marketText = await market.innerText().catch(() => "");
  const quotedOdds = extractQuotedOdds(marketText);
  if (typeof quotedOdds === "number" && quotedOdds < Number(candidate.minimum_odds || 0)) {
    return {
      action: "skip",
      reason: `Over 1.5 odds ${quotedOdds.toFixed(2)} were below the configured minimum ${Number(candidate.minimum_odds).toFixed(2)}.`,
      observedOdds: quotedOdds,
    };
  }

  await market.click();
  await shortWait();

  await fillStake(page, candidate.stake_amount);

  if (dryRun) {
    return {
      action: "placed",
      quotedOdds: quotedOdds || Number(candidate.minimum_odds),
      betReference: "dry-run",
    };
  }

  const submit = await waitForExisting(page, config.submitSelectors, 10000);
  if (!submit) {
    throw new Error("Place bet button was not found.");
  }
  await submit.click();
  await shortWait();

  const confirmation = await firstExisting(page, config.confirmationSelectors);
  if (confirmation) {
    await confirmation.click().catch(() => undefined);
    await shortWait();
  }

  const betReference = await readBetReference(page);
  return {
    action: "placed",
    quotedOdds: quotedOdds || Number(candidate.minimum_odds),
    betReference,
  };
}

async function findMatchRow(page, candidate) {
  const homePattern = new RegExp(escapeRegExp(candidate.home_team), "i");
  const awayPattern = new RegExp(escapeRegExp(candidate.away_team), "i");
  const homeText = page.getByText(homePattern);
  const awayText = page.getByText(awayPattern);
  for (const selector of config.matchRowSelectors) {
    const locator = page.locator(selector).filter({ has: homeText }).filter({ has: awayText }).first();
    if (await exists(locator)) {
      await locator.scrollIntoViewIfNeeded().catch(() => undefined);
      return locator;
    }
  }
  return null;
}

async function expandMatchRow(row) {
  const expand = await firstExisting(row, config.matchExpandSelectors);
  if (!expand) return;
  await expand.scrollIntoViewIfNeeded().catch(() => undefined);
  await expand.click().catch(() => undefined);
  await shortWait();
}

async function findOverOnePointFiveOption(row) {
  const scopes = [row, row.locator("xpath=following-sibling::*[1]").first()];
  for (const scope of scopes) {
    const direct = await firstExisting(scope, config.overMarketSelectors);
    if (direct) {
      const pricedDirect = await pricedOptionFromContainer(direct);
      if (pricedDirect) return pricedDirect;
      return direct;
    }
    const lineRow = await findTotalGoalsOnePointFiveLine(scope);
    if (lineRow) {
      const option = await pricedOptionFromContainer(lineRow);
      if (option) return option;
    }
  }
  return null;
}

async function findTotalGoalsOnePointFiveLine(scope) {
  const lineSelectors = [
    'tr',
    '[role="row"]',
    'div',
  ];
  for (const selector of lineSelectors) {
    const locator = scope.locator(selector)
      .filter({ hasText: /1[\.,]5/ })
      .filter({ hasText: /over/i })
      .first();
    if (await exists(locator)) {
      return locator;
    }
  }
  return null;
}

async function pricedOptionFromContainer(container) {
  const oddsTexts = container.locator('text=/\\d+\\.\\d+/');
  const count = await safeCount(oddsTexts);
  for (let index = count - 1; index >= 0; index -= 1) {
    const locator = oddsTexts.nth(index);
    const text = (await locator.innerText().catch(() => "")).trim();
    const value = Number(text.replace(/[^\d.]/g, ""));
    if (Number.isFinite(value) && value >= 1) {
      return locator;
    }
  }
  return null;
}

async function fillStake(page, stakeAmount) {
  const stakeField = await waitForExisting(page, config.stakeSelectors, 10000);
  if (!stakeField) {
    throw new Error("Stake input was not found after selecting the market.");
  }
  await stakeField.scrollIntoViewIfNeeded().catch(() => undefined);
  await stakeField.click({ clickCount: 3 }).catch(() => undefined);
  await stakeField.fill("").catch(() => undefined);
  await stakeField.type(String(stakeAmount));
  const typedValue = await stakeField.inputValue().catch(() => "");
  if (!typedValue || !typedValue.replace(/[^\d.]/g, "").startsWith(String(Number(stakeAmount)))) {
    await stakeField.press("Control+A").catch(() => undefined);
    await stakeField.type(String(stakeAmount));
  }
}

async function clearBetslip(page) {
  for (let pass = 0; pass < 5; pass += 1) {
    const button = await firstExisting(page, config.betslipRemoveSelectors);
    if (!button) return;
    await button.click().catch(() => undefined);
    await sleep(300);
  }
}

async function readBetReference(page) {
  for (const selector of config.betReferenceSelectors) {
    const locator = page.locator(selector).first();
    if (await exists(locator)) {
      return (await locator.innerText().catch(() => "")).trim();
    }
  }
  return "";
}

function extractQuotedOdds(text) {
  const matches = [...String(text || "").matchAll(/(\d+\.\d+)/g)].map((item) => Number(item[1])).filter(Number.isFinite);
  return matches.length > 0 ? matches[matches.length - 1] : undefined;
}

async function reportRuntime(payload) {
  try {
    await apiRequest("/internal/v1/auto-betting/runtime", {
      method: "POST",
      body: JSON.stringify({
        worker_id: workerId,
        last_heartbeat_at: new Date().toISOString(),
        ...payload,
      }),
    });
  } catch {
    // Keep the worker loop alive even if the dashboard heartbeat write fails.
  }
}

async function apiRequest(pathname, init = {}) {
  const response = await fetch(`${internalBaseUrl}${pathname}`, {
    ...init,
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      ...(internalToken ? { "X-Internal-Token": internalToken } : {}),
      ...(init.headers || {}),
    },
  });
  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new Error(`Internal API ${pathname} failed with ${response.status}: ${body || response.statusText}`);
  }
  return response.status === 204 ? null : response.json();
}

async function firstExisting(root, selectors) {
  for (const selector of selectors) {
    const locator = root.locator(selector).first();
    if (await exists(locator)) {
      return locator;
    }
  }
  return null;
}

async function waitForExisting(root, selectors, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const locator = await firstExisting(root, selectors);
    if (locator) return locator;
    await sleep(500);
  }
  return null;
}

async function exists(locator) {
  try {
    return await locator.count() > 0;
  } catch {
    return false;
  }
}

async function safeCount(locator) {
  try {
    return await locator.count();
  } catch {
    return 0;
  }
}

function selectorList(name, defaults) {
  const raw = process.env[name];
  if (!raw) return defaults;
  return raw.split(";;").map((value) => value.trim()).filter(Boolean);
}

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function normalizeError(error) {
  return error instanceof Error ? error.message : String(error);
}

function safeUrl(page) {
  try {
    return page.url();
  } catch {
    return "";
  }
}

async function shortWait() {
  await sleep(1500);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

main().catch(async (error) => {
  await reportRuntime({ state: "error", worker_id: workerId, last_error: normalizeError(error) });
  process.exitCode = 1;
});
