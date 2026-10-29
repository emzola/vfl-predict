import os from "node:os";
import path from "node:path";
import { chromium } from "playwright-extra";
import stealthPlugin from "puppeteer-extra-plugin-stealth";

chromium.use(stealthPlugin());

const workerId = process.env.AUTOBET_WORKER_ID || `${os.hostname()}-${process.pid}`;
const intervalMs = Math.max(10, Number(process.env.AUTOBET_INTERVAL_SECONDS || 10)) * 1000;
const internalBaseUrl = (process.env.INTERNAL_API_BASE_URL || "http://api:8090").replace(/\/+$/, "");
const internalToken = process.env.INTERNAL_API_TOKEN || "";
const virtualUrl = process.env.BET9JA_VIRTUAL_URL || "https://virtual.bet9ja.com/virtual/";
const tournamentName = process.env.BET9JA_VFL_TOURNAMENT_NAME || "Euro League";
const fallbackUsername = process.env.BET9JA_USERNAME || "";
const fallbackPassword = process.env.BET9JA_PASSWORD || "";
// Bet9ja automation intentionally runs headed under Xvfb so the site sees the
// same browser mode as the operator and the worker can be inspected if needed.
const headless = false;
const dryRun = /^true$/i.test(process.env.AUTOBET_DRY_RUN || "false");
const userDataRoot = process.env.AUTOBET_USER_DATA_DIR || path.join("/tmp", "autobet-browser");
// Credentials are stored by the API; use a unique profile per worker process
// so a stale Chromium lock from another container cannot block launch.
const userDataDir = path.join(userDataRoot, `worker-${os.hostname()}-${process.pid}-${Date.now()}`);
const navigationTimeoutMs = numberEnv("AUTOBET_NAVIGATION_TIMEOUT_MS", 30000, 5000);
const actionTimeoutMs = numberEnv("AUTOBET_ACTION_TIMEOUT_MS", 10000, 1000);
const pageReadyTimeoutMs = numberEnv("AUTOBET_PAGE_READY_TIMEOUT_MS", 60000, 5000);
const confirmationTimeoutMs = numberEnv("AUTOBET_CONFIRMATION_TIMEOUT_MS", 10000, 1000);
const shortDelayMs = numberEnv("AUTOBET_SHORT_WAIT_MS", 1500, 100);
const selectorPollMs = numberEnv("AUTOBET_SELECTOR_POLL_MS", 400, 100);
const typingDelayMs = numberEnv("AUTOBET_TYPING_DELAY_MS", 75, 0);
const browserRecycleMs = numberEnv("AUTOBET_BROWSER_RECYCLE_MINUTES", 180, 10) * 60 * 1000;
const maxPrepareAttempts = numberEnv("AUTOBET_MAX_PREPARE_ATTEMPTS", 2, 1);
const sessionCheckMs = numberEnv("AUTOBET_SESSION_CHECK_SECONDS", 60, 15) * 1000;
const maxConsecutiveFailures = numberEnv("AUTOBET_MAX_FAILURES_BEFORE_RESTART", 3, 1);
const heartbeatMs = numberEnv("AUTOBET_HEARTBEAT_SECONDS", 15, 5) * 1000;
const browserLocale = process.env.AUTOBET_LOCALE || "en-US";
// Optional, e.g. Africa/Lagos. Leave unset unless the worker's IP is in that zone,
// otherwise the timezone itself becomes a mismatch.
const browserTimezone = process.env.AUTOBET_TIMEZONE || "";

const config = {
  loginTriggerSelectors: selectorList("BET9JA_LOGIN_TRIGGER_SELECTORS", [
    'button:has-text("Log In")',
    'button:has-text("Login")',
    'a:has-text("Login")',
    'text=/\\blog\\s*in\\b/i',
  ]),
  loginPanelLabelSelectors: selectorList("BET9JA_LOGIN_PANEL_LABEL_SELECTORS", [
    'text=/^\\s*username\\s*$/i',
    'text=/^\\s*password\\s*$/i',
  ]),
  usernameSelectors: selectorList("BET9JA_USERNAME_SELECTORS", [
    'input[autocomplete="username"]',
    'input[name="username" i]',
    'input[name="userName" i]',
    'input[name*="phone" i]',
    'input[name*="mobile" i]',
    'input[id*="username" i]',
    'input[id*="phone" i]',
    'input[aria-label*="username" i]',
    'input[aria-label*="phone" i]',
    'input[aria-label*="mobile" i]',
    'input[placeholder*="username" i]',
    'input[placeholder*="phone" i]',
    'input[placeholder*="mobile" i]',
    'input[type="tel"]',
    'input[inputmode="numeric"]',
    'input[type="number"]',
    'input[type="text"]',
    'input:not([type])',
  ]),
  passwordSelectors: selectorList("BET9JA_PASSWORD_SELECTORS", [
    'input[autocomplete="current-password"]',
    'input[name="password"]',
    'input[id*="password" i]',
    'input[aria-label*="password" i]',
    'input[type="password"]',
    'input[placeholder*="password" i]',
  ]),
  loginSubmitSelectors: selectorList("BET9JA_LOGIN_SUBMIT_SELECTORS", [
    'button:has-text("Login")',
    'button:has-text("Log in")',
    'button:has-text("Sign in")',
    'button[type="submit"]',
    'input[type="submit"]',
    'text=/\\blog\\s*in\\b/i',
  ]),
  loginFailureSelectors: selectorList("BET9JA_LOGIN_FAILURE_SELECTORS", [
    'text=/invalid\\s+(?:username|password|credentials)/i',
    'text=/incorrect\\s+(?:username|password|credentials)/i',
    'text=/wrong\\s+(?:username|password)/i',
    'text=/captcha/i',
    'text=/verification\\s+(?:code|required)/i',
    'text=/too\\s+many\\s+login\\s+attempts/i',
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
    'input:not([type="hidden"])',
  ]),
  submitSelectors: selectorList("BET9JA_PLACE_BET_SELECTORS", [
    'button:has-text("Place Bet")',
    'button:has-text("Book Bet")',
    'button:has-text("Place bet")',
    'text=/place\\s*bet/i',
  ]),
  betslipHeaderSelectors: selectorList("BET9JA_BETSLIP_HEADER_SELECTORS", [
    'text=/^betslip\\s*\\(\\s*\\d+\\s*\\)$/i',
  ]),
  confirmationSelectors: selectorList("BET9JA_CONFIRM_SELECTORS", []),
  successSelectors: selectorList("BET9JA_SUCCESS_SELECTORS", [
    'text=/^\\s*your\\s+bet\\s+has\\s+been\\s+placed\\.?\\s*$/i',
    'text=/^\\s*bet\\s+has\\s+been\\s+placed\\.?\\s*$/i',
    'text=/^\\s*place\\s+another\\s+bet\\s*$/i',
  ]),
  betReferenceSelectors: selectorList("BET9JA_BET_REFERENCE_SELECTORS", [
    'text=/reference/i',
    'text=/booking\\s*code/i',
  ]),
  betslipRemoveSelectors: selectorList("BET9JA_BETSLIP_REMOVE_SELECTORS", [
    'button[aria-label*="remove" i]',
    'button[title*="remove" i]',
    'button[aria-label*="delete" i]',
    'button[title*="delete" i]',
    'button[aria-label*="close" i]',
    'button[title*="close" i]',
    'button:has-text("×")',
    'text=/^x$/i',
  ]),
  dismissOverlaySelectors: selectorList("BET9JA_DISMISS_OVERLAY_SELECTORS", [
    'button[aria-label*="close" i]',
    'button[title*="close" i]',
    '[role="dialog"] button[aria-label*="close" i]',
    '[role="dialog"] button[title*="close" i]',
  ]),
};

const browserState = {
  context: null,
  page: null,
  launchedAt: 0,
};

let unconfirmedBet = null;
let lastRuntime = {};
const session = { prepared: false, lastCheckedAt: 0, failures: 0 };
// Last placement/login failure. It stays on the dashboard until a round is
// actually placed or skipped, so a failure is not wiped by the next idle poll.
let lastFailure = "";

function describeFailure(error, task) {
  const round = task?.candidate
    ? ` [season ${task.candidate.season_id} round ${task.candidate.round}: ${task.candidate.home_team} - ${task.candidate.away_team}]`
    : "";
  return `${new Date().toISOString()}${round} ${normalizeError(error)}`;
}

class UnconfirmedBetError extends Error {
  constructor(message, candidate, currentUrl) {
    super(message);
    this.name = "UnconfirmedBetError";
    this.currentSeasonID = candidate?.season_id || "";
    this.currentRound = candidate?.round || 0;
    this.currentMatchID = candidate?.match_id || 0;
    this.currentURL = currentUrl || "";
  }
}

async function main() {
  await reportRuntime({ state: "idle", worker_id: workerId, last_error: "" });
  // Background heartbeat so long browser steps (launch, login, page load) never
  // leave the dashboard showing a stale state.
  setInterval(() => void reportRuntime({ ...lastRuntime }), heartbeatMs).unref?.();
  while (true) {
    let task = null;
    try {
      task = await apiRequest("/internal/v1/auto-betting/task");
      if (unconfirmedBet) {
        await reportRuntime({
          state: "error",
          worker_id: workerId,
          current_season_id: task?.current_season_id || unconfirmedBet.currentSeasonID,
          current_round: task?.candidate?.round || unconfirmedBet.currentRound,
          current_match_id: task?.candidate?.match_id || unconfirmedBet.currentMatchID,
          current_url: unconfirmedBet.currentURL,
          last_error: unconfirmedBet.message,
        });
      } else {
        await handleTask(task);
      }
    } catch (error) {
      if (error instanceof UnconfirmedBetError) {
        unconfirmedBet = {
          currentSeasonID: error.currentSeasonID,
          currentRound: error.currentRound,
          currentMatchID: error.currentMatchID,
          currentURL: error.currentURL,
          message: normalizeError(error),
        };
      }
      lastFailure = describeFailure(error, task);
      console.error(`[autobet] ${lastFailure}`);
      // Keep the browser open across failures; the next loop re-verifies the page and
      // re-navigates. Only restart Chromium when it is dead or keeps failing.
      session.prepared = false;
      session.failures += 1;
      if (!browserAlive() || session.failures >= maxConsecutiveFailures) {
        await closeBrowser().catch(() => undefined);
        session.failures = 0;
      }
      await reportRuntime({
        state: "error",
        worker_id: workerId,
        ...(unconfirmedBet
          ? {
              current_season_id: unconfirmedBet.currentSeasonID,
              current_round: unconfirmedBet.currentRound,
              current_match_id: unconfirmedBet.currentMatchID,
              current_url: unconfirmedBet.currentURL,
            }
          : {}),
        last_error: unconfirmedBet?.message || lastFailure,
      });
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
    last_error: lastFailure,
  };
  if (!task || task.status === "disabled") {
    // Auto betting switched off: release the browser.
    if (browserState.context) {
      await closeBrowser().catch(() => undefined);
    }
    session.prepared = false;
    await reportRuntime({ ...runtime, state: task?.status || "idle" });
    return;
  }

  const credentials = await resolveCredentials();
  if (!credentials.username || !credentials.password) {
    await reportRuntime({ ...runtime, state: "login_required", last_error: "Bet9ja credentials must be saved from the dashboard or provided through the env fallback." });
    return;
  }

  const isReady = task.status === "ready" && Boolean(task.candidate);
  // Auto betting is on: open the browser, log in and select Euro League once, then
  // keep reusing that page for every bet.
  const page = await keepSessionWarm(credentials, isReady ? task.candidate : null);
  if (!isReady) {
    await reportRuntime({ ...runtime, state: task.status || "idle", current_url: safeUrl(page) });
    return;
  }

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
    lastFailure = "";
    session.failures = 0;
    await reportRuntime({
      ...runtime,
      last_error: "",
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
  lastFailure = "";
  session.failures = 0;
  await reportRuntime({
    ...runtime,
    last_error: "",
    state: "awaiting_settlement",
    current_url: safeUrl(page),
    last_heartbeat_at: new Date().toISOString(),
  });
}

function browserAlive() {
  return Boolean(browserState.context && browserState.page && !browserState.page.isClosed());
}

// Cheap check that the already-open page can still be used: still on the virtual
// site, still logged in, and the betting frame is loaded.
async function isSessionStillUsable(page) {
  let host = "";
  try {
    host = new URL(safeUrl(page)).host;
  } catch {
    return false;
  }
  if (host !== new URL(virtualUrl).host) return false;
  if (!(await hasActiveSession(page))) return false;
  return Boolean(await bettingFrame(page));
}

async function keepSessionWarm(credentials, candidate) {
  // Recycle a long-lived browser only between bets, never right before one.
  if (!candidate && browserAlive() && shouldRecycleBrowser()) {
    await closeBrowser();
    session.prepared = false;
  }
  if (browserAlive() && session.prepared) {
    const page = browserState.page;
    if (!candidate && Date.now() - session.lastCheckedAt < sessionCheckMs) {
      return page;
    }
    if (await isSessionStillUsable(page)) {
      session.lastCheckedAt = Date.now();
      if (candidate) await ensureTournament(page, candidate);
      return page;
    }
  }
  const page = await ensureReadyPage(candidate, credentials);
  session.prepared = true;
  session.lastCheckedAt = Date.now();
  return page;
}

async function ensureReadyPage(candidate, credentials) {
  let lastError;
  for (let attempt = 1; attempt <= maxPrepareAttempts; attempt += 1) {
    const page = await ensureBrowser();
    try {
      await openVirtualHome(page);
      if (!(await hasActiveSession(page))) {
        await login(page, credentials);
        await openVirtualHome(page);
      }
      await ensureTournament(page, candidate);
      return page;
    } catch (error) {
      lastError = error;
      if (attempt >= maxPrepareAttempts) {
        break;
      }
      await closeBrowser();
      await sleep(1000);
    }
  }
  throw lastError;
}

async function ensureBrowser() {
  if (browserAlive()) {
    return browserState.page;
  }
  await closeBrowser();

  try {
    browserState.context = await chromium.launchPersistentContext(userDataDir, {
      headless,
      viewport: { width: 1440, height: 1000 },
      args: [
        "--disable-dev-shm-usage",
        "--no-sandbox",
        "--disable-setuid-sandbox",
        "--disable-blink-features=AutomationControlled",
        "--disable-gpu"
      ],
      deviceScaleFactor: 1,
      // No userAgent override: a hard-coded "Windows / Chrome 126" string contradicted
      // the real Linux Chromium (navigator.platform, client hints, JS features), which is
      // exactly what bot checks look for. Let Chromium report its own identity.
      locale: browserLocale,
      ...(browserTimezone ? { timezoneId: browserTimezone } : {}),
    });
  } catch (error) {
    const display = process.env.DISPLAY || "unset";
    const message = normalizeError(error);
    throw new Error(`Chromium headed launch failed for user data directory ${userDataDir} with DISPLAY=${display}. ${message}`);
  }

  browserState.context.setDefaultTimeout(actionTimeoutMs);
  browserState.context.setDefaultNavigationTimeout(navigationTimeoutMs);
  browserState.page = browserState.context.pages()[0] || await browserState.context.newPage();
  browserState.launchedAt = Date.now();
  bindContextLifecycle(browserState.context);
  bindPageLifecycle(browserState.page);
  return browserState.page;
}

async function hasActiveSession(page) {
  const snapshot = await inspectSession(page);
  return isActiveSessionSnapshot(snapshot);
}

async function login(page, credentials) {
  await maybeDismissOverlays(page);
  let fields = await findLoginFields(page, 8000);
  if (!fields.userField || !fields.passwordField) {
    const trigger = await firstExisting(page, config.loginTriggerSelectors);
    if (trigger) {
      await clickLocator(trigger);
      await shortWait();
    }
    fields = await findLoginFields(page, 10000);
  }
  if ((!fields.userField || !fields.passwordField) && !page.isClosed()) {
    await page.reload({ waitUntil: "domcontentloaded", timeout: navigationTimeoutMs }).catch(() => undefined);
    await waitForPageReady(page);
    const trigger = await firstExisting(page, config.loginTriggerSelectors);
    if (trigger) {
      await clickLocator(trigger).catch(() => undefined);
      await shortWait();
    }
    fields = await findLoginFields(page, 10000);
  }
  const { userField, passwordField } = fields;
  if (!userField || !passwordField) {
    const snapshot = await inspectSession(page);
    throw new Error(`Bet9ja login form was not found. ${await formatLoginDiagnostics(page, credentials, snapshot)}`);
  }
  await fillInput(userField, credentials.username);
  await fillInput(passwordField, credentials.password);
  const loginUrl = safeUrl(page);
  const loginScope = await findLoginScope(userField, passwordField);
  const scopedSubmit = await firstExisting(loginScope, config.loginSubmitSelectors);
  if (!scopedSubmit) {
    const snapshot = await inspectSession(page);
    throw new Error(`Bet9ja login submit button was not found inside the login form. ${await formatLoginDiagnostics(page, credentials, snapshot, loginUrl)}`);
  }
  await clickLocator(scopedSubmit);
  await waitForPageReady(page);
  const session = await waitForLoginResult(page);
  if (session.loginFailureIndicator) {
    throw new Error(`Bet9ja login was rejected. ${await formatLoginDiagnostics(page, credentials, session)}`);
  }
  if (!isActiveSessionSnapshot(session, { allowImplicit: true })) {
    throw new Error(`Bet9ja login did not produce an active session. ${await formatLoginDiagnostics(page, credentials, session, loginUrl)}`);
  }
  await reportRuntime({
    state: "ready",
    worker_id: workerId,
    current_url: safeUrl(page),
    last_error: lastFailure,
    last_login_at: new Date().toISOString(),
    last_heartbeat_at: new Date().toISOString(),
  });
}

async function findLoginFields(page, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let latest = { scope: page, userField: null, passwordField: null };
  while (Date.now() < deadline) {
    const scope = await findLoginPanel(page);
    const userField = await firstExisting(scope, config.usernameSelectors);
    const passwordField = await firstExisting(scope, config.passwordSelectors);
    latest = { scope, userField, passwordField };
    if (userField && passwordField) {
      return latest;
    }
    await sleep(selectorPollMs);
  }
  return latest;
}

async function findLoginPanel(page) {
  for (const selector of config.loginPanelLabelSelectors) {
    for (const root of searchRoots(page)) {
      const label = root.locator(selector).first();
      if (!(await isVisible(label))) continue;
      const candidates = [
        label.locator("xpath=ancestor::form[1]").first(),
        label.locator("xpath=ancestor::*[.//input and (.//button or .//input[@type='submit'] or .//*[@role='button'])][1]").first(),
      ];
      for (const candidate of candidates) {
        if (!(await isVisible(candidate))) continue;
        const userField = await firstExisting(candidate, config.usernameSelectors);
        const passwordField = await firstExisting(candidate, config.passwordSelectors);
        if (userField && passwordField) {
          return candidate;
        }
      }
    }
  }
  return page;
}

async function findLoginScope(userField, passwordField) {
  const candidates = [
    userField.locator("xpath=ancestor::form[1]").first(),
    userField.locator("xpath=ancestor::*[.//input[@type='password'] and (.//button or .//input[@type='submit'] or .//*[@role='button'])][1]").first(),
    passwordField.locator("xpath=ancestor::*[.//input and (.//button or .//input[@type='submit'] or .//*[@role='button'])][1]").first(),
  ];
  for (const candidate of candidates) {
    if (!(await isVisible(candidate))) continue;
    if (await firstExisting(candidate, config.loginSubmitSelectors)) {
      return candidate;
    }
  }
  return userField.locator("xpath=ancestor::*[.//button or .//input[@type='submit'] or .//*[@role='button']][1]").first();
}

async function inspectSession(page) {
  const logoutIndicator = await firstVisibleMatch(page, config.logoutIndicators);
  const usernameField = await firstVisibleMatch(page, config.usernameSelectors);
  const passwordField = await firstVisibleMatch(page, config.passwordSelectors);
  const loginTrigger = await firstVisibleMatch(page, config.loginTriggerSelectors);
  const loginFailureIndicator = await firstVisibleMatch(page, config.loginFailureSelectors);
  return {
    logoutIndicator,
    loginFailureIndicator,
    loginFormVisible: Boolean(usernameField || passwordField),
    loginTriggerVisible: Boolean(loginTrigger),
  };
}

function isActiveSessionSnapshot(snapshot, { allowImplicit = false } = {}) {
  if (snapshot.logoutIndicator) return true;
  if (snapshot.loginFailureIndicator || snapshot.loginFormVisible || snapshot.loginTriggerVisible) return false;
  return allowImplicit;
}

async function waitForLoginResult(page) {
  const deadline = Date.now() + numberEnv("AUTOBET_LOGIN_VERIFICATION_TIMEOUT_MS", 10000, 1000);
  let snapshot = await inspectSession(page);
  while (Date.now() < deadline) {
    if (snapshot.loginFailureIndicator || isActiveSessionSnapshot(snapshot, { allowImplicit: true })) {
      return snapshot;
    }
    await sleep(selectorPollMs);
    snapshot = await inspectSession(page);
  }
  return snapshot;
}

async function formatLoginDiagnostics(page, credentials, snapshot, loginUrl = "") {
  const url = safeUrl(page);
  const [title, bodyText] = await Promise.all([
    getPageTitle(page),
    getSanitizedBodyText(page, credentials?.username),
  ]);
  const urlChanged = Boolean(loginUrl && url && loginUrl !== url);
  return [
    `url=${url || "unknown"}`,
    `url_changed=${urlChanged}`,
    `title=${JSON.stringify(title)}`,
    `frames=${JSON.stringify(frameUrls(page))}`,
    `authenticated_indicator=${snapshot.logoutIndicator?.selector || "none"}`,
    `login_form_visible=${snapshot.loginFormVisible}`,
    `login_trigger_visible=${snapshot.loginTriggerVisible}`,
    `login_failure_indicator=${snapshot.loginFailureIndicator?.selector || "none"}`,
    `body=${JSON.stringify(bodyText)}`,
  ].join(" ");
}

function getPageTitle(page) {
  return page.title().then((value) => String(value || "").trim().slice(0, 160)).catch(() => "");
}

function getSanitizedBodyText(page, username) {
  return Promise.all(searchRoots(page).map((root) => (
    root.locator("body").innerText({ timeout: 1000 }).then((value) => {
      let text = String(value || "").replace(/\s+/g, " ").trim();
      if (username) {
        text = text.split(String(username)).join("[redacted]");
      }
      return text;
    }).catch(() => "")
  ))).then((values) => values.filter(Boolean).join(" | ").slice(0, 600)).catch(() => "");
}

async function resolveCredentials() {
  const stored = await fetchStoredCredentials();
  if (stored?.username && stored?.password) {
    return stored;
  }
  return { username: fallbackUsername, password: fallbackPassword };
}

async function fetchStoredCredentials() {
  const response = await fetch(`${internalBaseUrl}/internal/v1/auto-betting/credentials`, {
    headers: {
      Accept: "application/json",
      ...(internalToken ? { "X-Internal-Token": internalToken } : {}),
    },
  });
  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new Error(`Internal API /internal/v1/auto-betting/credentials failed with ${response.status}: ${body || response.statusText}`);
  }
  return response.json();
}

async function ensureTournament(page, candidate) {
  // On a reused page Euro League is usually already showing; don't click the tab
  // again (that reloads the game frame) if the candidate's match is visible.
  if (candidate?.home_team && await waitForTeamText(page, candidate.home_team, 1500)) {
    return;
  }
  for (let attempt = 0; attempt < 2; attempt += 1) {
    await maybeDismissOverlays(page);
    const selector = await firstExisting(page, config.tournamentSelectors);
    if (selector) {
      await clickLocator(selector).catch(() => undefined);
      await shortWait();
    }
    if (!candidate?.home_team || await waitForTeamText(page, candidate.home_team, 5000)) {
      return;
    }
    await page.reload({ waitUntil: "domcontentloaded", timeout: navigationTimeoutMs }).catch(() => undefined);
    await waitForPageReady(page);
  }
}









async function fillStake(betslip, stakeAmount) {
  const stakeField = await waitForExisting(betslip, config.stakeSelectors, 10000);
  if (!stakeField) {
    throw new Error("Stake input was not found after selecting the market.");
  }
  const expectedValue = normalizeNumeric(String(Number(stakeAmount)));
  for (let attempt = 0; attempt < 2; attempt += 1) {
    await fillInput(stakeField, String(stakeAmount));
    const typedValue = normalizeNumeric(await stakeField.inputValue().catch(() => ""));
    if (typedValue === expectedValue || typedValue.startsWith(expectedValue)) {
      return;
    }
  }
  throw new Error(`Stake input value did not match ${expectedValue || String(stakeAmount)}.`);
}


async function formatBetConfirmationDiagnostics(page) {
  const bodyText = await getSanitizedBodyText(page, "");
  const submitVisible = Boolean(await firstVisibleMatch(page, config.submitSelectors));
  return [
    "url=" + (safeUrl(page) || "unknown"),
    "submit_visible=" + submitVisible,
    "body=" + JSON.stringify(bodyText),
  ].join(" ");
}

async function readBetReference(page, timeoutMs = 0) {
  const deadline = Date.now() + Math.max(0, timeoutMs);
  while (true) {
    const match = await firstVisibleMatch(page, config.betReferenceSelectors);
    if (match) {
      return (await match.locator.innerText().catch(() => "")).trim();
    }
    if (!timeoutMs || Date.now() >= deadline) {
      return "";
    }
    await sleep(selectorPollMs);
  }
}


async function reportRuntime(payload) {
  const { last_heartbeat_at: _ignored, ...rest } = payload || {};
  lastRuntime = rest;
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
    for (const searchRoot of searchRoots(root)) {
      const locator = searchRoot.locator(selector).first();
      if (await isVisible(locator) && await isEnabled(locator)) {
        return locator;
      }
    }
  }
  return null;
}

async function firstVisibleMatch(root, selectors) {
  for (const selector of selectors) {
    for (const searchRoot of searchRoots(root)) {
      const locator = searchRoot.locator(selector).first();
      if (await isVisible(locator)) {
        return { locator, selector };
      }
    }
  }
  return null;
}

function searchRoots(root) {
  if (!root || typeof root.frames !== "function") {
    return [root];
  }
  try {
    return root.frames();
  } catch {
    return [root];
  }
}

async function waitForExisting(root, selectors, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const locator = await firstExisting(root, selectors);
    if (locator) return locator;
    await sleep(selectorPollMs);
  }
  return null;
}

async function waitForVisibleExisting(root, selectors, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const match = await firstVisibleMatch(root, selectors);
    if (match) return match.locator;
    await sleep(selectorPollMs);
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

async function isVisible(locator) {
  try {
    return await locator.isVisible();
  } catch {
    return false;
  }
}

async function isEnabled(locator) {
  try {
    return await locator.isEnabled();
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

function numberEnv(name, fallback, minimum = 0) {
  const value = Number(process.env[name] || fallback);
  if (!Number.isFinite(value)) return fallback;
  return Math.max(minimum, value);
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

function frameUrls(page) {
  return searchRoots(page).map((root) => {
    try {
      return typeof root.url === "function" ? root.url() : "";
    } catch {
      return "";
    }
  }).filter(Boolean);
}

async function shortWait() {
  await sleep(shortDelayMs);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function shouldRecycleBrowser() {
  return browserState.launchedAt > 0 && Date.now() - browserState.launchedAt >= browserRecycleMs;
}

function resetBrowserState() {
  browserState.context = null;
  browserState.page = null;
  browserState.launchedAt = 0;
}

async function closeBrowser() {
  const context = browserState.context;
  resetBrowserState();
  if (context) {
    await context.close().catch(() => undefined);
  }
}

function bindContextLifecycle(context) {
  context.on("close", () => {
    if (browserState.context === context) {
      resetBrowserState();
    }
  });
  context.on("page", (page) => {
    if (!browserState.page || browserState.page.isClosed()) {
      browserState.page = page;
      bindPageLifecycle(page);
    }
  });
}

// Recent network/console problems, attached to page-render diagnostics so a blank
// page says *why* it is blank (blocked script, 403 from a WAF, geo block, JS error).
const pageProblems = [];
function notePageProblem(text) {
  pageProblems.push(`${new Date().toISOString().slice(11, 19)} ${String(text).slice(0, 220)}`);
  if (pageProblems.length > 25) pageProblems.shift();
}

function bindPageLifecycle(page) {
  page.on("requestfailed", (request) => {
    notePageProblem(`requestfailed ${request.failure()?.errorText || ""} ${request.url()}`);
  });
  page.on("response", (response) => {
    if (response.status() >= 400) notePageProblem(`http ${response.status()} ${response.url()}`);
  });
  page.on("console", (message) => {
    if (message.type() === "error") notePageProblem(`console ${message.text()}`);
  });
  page.on("pageerror", (error) => notePageProblem(`pageerror ${normalizeError(error)}`));
  page.on("close", () => {
    if (browserState.page === page) {
      browserState.page = null;
    }
  });
  page.on("crash", () => {
    if (browserState.page === page) {
      resetBrowserState();
    }
  });
  page.on("dialog", (dialog) => {
    void dialog.dismiss().catch(() => undefined);
  });
}

async function openVirtualHome(page) {
  await page.goto(virtualUrl, { waitUntil: "domcontentloaded", timeout: navigationTimeoutMs });
  await waitForPageReady(page);
}

async function waitForPageReady(page) {
  await page.waitForLoadState("domcontentloaded", { timeout: navigationTimeoutMs }).catch(() => undefined);
  await page.locator("body").first().waitFor({ state: "visible", timeout: pageReadyTimeoutMs }).catch(() => undefined);
  await page.waitForLoadState("networkidle", { timeout: 2000 }).catch(() => undefined);
  if (!(await waitForRenderedContent(page, pageReadyTimeoutMs))) {
    throw new Error(`Bet9ja page did not render visible content. ${await formatPageRenderDiagnostics(page)}`);
  }
  await maybeDismissOverlays(page);
  await shortWait();
}

async function waitForRenderedContent(page, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  const readySelectors = [
    ...config.loginTriggerSelectors,
    ...config.logoutIndicators,
    ...config.tournamentSelectors,
    'text=/place\\s+your\\s+bets/i',
    'text=/register/i',
  ];
  while (Date.now() < deadline) {
    if (await firstVisibleMatch(page, readySelectors)) {
      return true;
    }
    for (const root of searchRoots(page)) {
      const body = root.locator("body").first();
      if (!(await isVisible(body))) continue;
      const text = (await body.innerText().catch(() => "")).replace(/\s+/g, " ").trim();
      if (text.length >= 40) {
        return true;
      }
    }
    await sleep(selectorPollMs);
  }
  return false;
}

async function formatPageRenderDiagnostics(page) {
  const [title, bodyText, readyState, htmlLengths] = await Promise.all([
    getPageTitle(page),
    getSanitizedBodyText(page),
    page.evaluate(() => document.readyState).catch(() => "unknown"),
    Promise.all(searchRoots(page).map((root) => root.locator("body").first().evaluate((body) => body.innerHTML.length).catch(() => 0))),
  ]);
  return [
    `url=${safeUrl(page) || "unknown"}`,
    `title=${JSON.stringify(title)}`,
    `ready_state=${readyState}`,
    `render_timeout_ms=${pageReadyTimeoutMs}`,
    `frames=${JSON.stringify(frameUrls(page))}`,
    `body_html_lengths=${JSON.stringify(htmlLengths)}`,
    `body=${JSON.stringify(bodyText)}`,
    `user_agent=${JSON.stringify(await page.evaluate(() => navigator.userAgent).catch(() => ""))}`,
    `page_problems=${JSON.stringify(pageProblems.slice(-15))}`,
  ].join(" ");
}

async function refreshForCandidate(page, candidate) {
  await page.reload({ waitUntil: "domcontentloaded", timeout: navigationTimeoutMs }).catch(() => undefined);
  await waitForPageReady(page);
  await ensureTournament(page, candidate);
}

async function maybeDismissOverlays(page) {
  for (let pass = 0; pass < 2; pass += 1) {
    const button = await firstExisting(page, config.dismissOverlaySelectors);
    if (!button) {
      return;
    }
    await clickLocator(button).catch(() => undefined);
    await sleep(250);
  }
}

async function waitForTeamText(page, teamName, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    for (const root of searchRoots(page)) {
      const locator = root.getByText(new RegExp(escapeRegExp(teamName), "i")).first();
      if (await isVisible(locator)) {
        return true;
      }
    }
    await sleep(selectorPollMs);
  }
  return false;
}

async function clickLocator(locator, options = {}) {
  let lastError;
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      await locator.scrollIntoViewIfNeeded().catch(() => undefined);
      await locator.click({ timeout: actionTimeoutMs, ...options });
      return;
    } catch (error) {
      lastError = error;
      await sleep(200);
    }
  }
  throw lastError instanceof Error ? lastError : new Error("Element click failed.");
}

async function fillInput(locator, value) {
  await locator.scrollIntoViewIfNeeded().catch(() => undefined);
  await locator.click({ clickCount: 3, timeout: actionTimeoutMs }).catch(() => undefined);
  await selectAll(locator);
  await locator.fill("").catch(() => undefined);
  if (typingDelayMs > 0) {
    await locator.type(String(value), { delay: typingDelayMs });
  } else {
    await locator.fill(String(value));
  }
}

async function selectAll(locator) {
  await locator.press("Control+A").catch(() => undefined);
  await locator.press("Meta+A").catch(() => undefined);
}

async function clickableAncestor(locator) {
  const candidate = locator.locator("xpath=ancestor-or-self::button | ancestor-or-self::*[@role='button']").first();
  return await isVisible(candidate) && await isEnabled(candidate) ? candidate : locator;
}

function normalizeNumeric(value) {
  return String(value || "").replace(/[^\d.]/g, "");
}

// ---------- Euro League (vflm) market selection ----------
//
// DOM layout (checked against the live page, Euro League / product=vflm):
//   - Match table + betslip live in the frame vgstaticassetslive-vs001.akamaized.net/.../initializer/...
//   - Video, results, league table and the betting countdown live in vflive-vs001.akamaized.net/vflm/...
//   - Each match is div.sr-matchRow; names are span.sr-home / span.sr-away (the badge <img> shares the class).
//   - The "+21" expander is .sr-additional-markets a; expanded markets render inside the same .sr-matchRow.
//   - Odds buttons are a.sr-oddsButton with a Sportradar market id in data-name, price in span.sr-odd:
//       /18/12?total=1.5  Total Goals over 1.5        <- the market we want
//       /19/12?total=1.5  Home Total Goals over 1.5
//       /20/12?total=1.5  Away Total Goals over 1.5
//       /68/12?total=1.5  1st half total over 1.5
//     When a match's main Total Goals line is 1.5, that button is in the row itself; otherwise it is in the
//     expanded panel. The same selector covers both cases.
//   - Betslip internals (selection card, stake input, Place bet button) were NOT inspected.

const OVER_15 = 'a.sr-oddsButton[data-name$="/18/12?total=1.5"]';
// The countdown is span.countdown_amount. A generic mm:ss text selector also matches the video player's
// span.fp-elapsed / span.fp-remaining and can return the wrong time.
const TIMER = ".countdown_amount";
const verifyRound = /^true$/i.test(process.env.BET9JA_VERIFY_ROUND || "false");

// ---------- odds parsing ----------

function extractQuotedOdds(text) {
  const matches = [...String(text || "").matchAll(/\d+\.\d{2}/g)].map((m) => Number(m[0])).filter(Number.isFinite);
  return matches.length > 0 ? matches[matches.length - 1] : undefined;
}

// The price is in span.sr-odd. The button's full text is "over 1.51.15" (label + price), so read the span when
// present; otherwise (main-row buttons hold only the price) fall back to the button text.
async function readOptionOdds(option) {
  const odd = option.locator(".sr-odd").first();
  const raw = (await safeCount(odd))
    ? await odd.textContent().catch(() => "")
    : String((await option.textContent().catch(() => "")) || "").replace(/^\s*over\s*1[.,]5/i, " ");
  return extractQuotedOdds(String(raw || ""));
}

async function readBetslipOdds(betslip) {
  const text = String((await betslip.innerText().catch(() => "")) || "").replace(/\s+/g, " ");
  const m = text.match(/over\s*1[.,]5\s*(\d+\.\d{2})/i);
  return m ? Number(m[1]) : undefined;
}

async function readTicketStake(betslip) {
  const text = String((await betslip.innerText().catch(() => "")) || "").replace(/\s+/g, " ");
  const m = text.match(/ticket\s*stake\s*(?:NGN)?\s*([\d,]+(?:\.\d+)?)/i);
  return m ? Number(m[1].replace(/,/g, "")) : undefined;
}

async function readSecondsLeft(page) {
  const match = await firstVisibleMatch(page, [TIMER]);
  if (!match) return null;
  const text = String((await match.locator.textContent().catch(() => "")) || "").trim();
  const [minutes, seconds] = text.split(":").map(Number);
  return Number.isFinite(minutes) && Number.isFinite(seconds) ? minutes * 60 + seconds : null;
}

async function waitUntil(predicate, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await predicate()) return true;
    await sleep(selectorPollMs);
  }
  return false;
}

// ---------- match row + Over 1.5 ----------

// The match table lives in its own iframe; it is the only frame that contains div.sr-matchRow.
async function bettingFrame(page) {
  for (const frame of page.frames()) {
    if (await safeCount(frame.locator(".sr-matchRow"))) return frame;
  }
  return null;
}

function exactText(value) {
  return new RegExp(`^\\s*${escapeRegExp(value)}\\s*$`, "i");
}

// span.sr-home / span.sr-away: the badge <img> carries the same class, so the tag matters.
async function findMatchRow(page, candidate) {
  const frame = await bettingFrame(page);
  if (!frame) return null;
  const rows = frame
    .locator(".sr-matchRow")
    .filter({ has: frame.locator("span.sr-home", { hasText: exactText(candidate.home_team) }) })
    .filter({ has: frame.locator("span.sr-away", { hasText: exactText(candidate.away_team) }) });
  if ((await safeCount(rows)) !== 1) return null;
  await rows.scrollIntoViewIfNeeded().catch(() => undefined);
  return rows;
}

async function expandMatchRow(row) {
  const expander = row.locator(".sr-additional-markets a").first();
  if (!(await isVisible(expander))) return false;
  await clickLocator(expander).catch(() => undefined);
  await shortWait();
  return true;
}

// Market 18 / outcome 12 / total=1.5 = Total Goals over 1.5. Never matches Home (19) or Away (20) totals.
async function findOverOnePointFiveOption(_page, row) {
  const button = row.locator(OVER_15).first();
  return (await isVisible(button)) ? button : null;
}

async function locateOverMarket(page, candidate) {
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const row = await findMatchRow(page, candidate);
    if (!row) {
      if (attempt === 0) {
        await refreshForCandidate(page, candidate);
        continue;
      }
      return { skipReason: "Match row was not found on the virtual page." };
    }
    let market = await findOverOnePointFiveOption(page, row);
    if (!market && (await expandMatchRow(row))) {
      for (let poll = 0; poll < 6 && !market; poll += 1) {
        await sleep(selectorPollMs);
        market = await findOverOnePointFiveOption(page, row);
      }
    }
    if (market) return { row, market };
    if (attempt === 0) await refreshForCandidate(page, candidate);
  }
  return { skipReason: "Over 1.5 market was not available for the selected match." };
}

// ---------- betslip ----------

// The betslip panel (header "Betslip (n)", selections, stake, Place bet) is form.sr-bs-container.
async function findBetslipScope(page) {
  const frame = (await bettingFrame(page)) || page;
  for (const root of searchRoots(frame)) {
    const panel = root.locator("form.sr-bs-container").first();
    if (await isVisible(panel)) return panel;
  }
  // Fallback (live check 2026-10-29): a filled slip always shows "Ticket stake" and
  // "Place bet NGN ..." in one panel. The last match in document order is the
  // innermost such element, i.e. the slip itself rather than a page wrapper.
  for (const root of page.frames()) {
    const panel = root
      .locator("form, div, section")
      .filter({ hasText: /ticket\s*stake/i })
      .filter({ hasText: /place\s*bet/i })
      .last();
    if (await isVisible(panel)) return panel;
  }
  return null;
}

async function betslipCount(page) {
  const header = await firstVisibleMatch(page, config.betslipHeaderSelectors);
  if (!header) return 0;
  const text = String((await header.locator.innerText().catch(() => "")) || "");
  return Number((text.match(/\(\s*(\d+)\s*\)/) || [])[1] || 0);
}

async function clearBetslip(page) {
  for (let pass = 0; pass < 5; pass += 1) {
    if ((await betslipCount(page)) === 0) return;
    const betslip = await findBetslipScope(page);
    if (!betslip) break;

    // 1) selectors from config / env
    const configured = await firstExisting(betslip, config.betslipRemoveSelectors);
    if (configured) {
      await clickLocator(configured).catch(() => undefined);
    } else {
      // 2) the selection card = innermost element with the "Season N - Matchday N" line and a price
      const card = betslip
        .locator("div")
        .filter({ hasText: /season\s*\d+\s*-\s*match\s*day\s*\d+/i })
        .filter({ hasText: /\d+\.\d{2}/ })
        .last();
      await card.scrollIntoViewIfNeeded().catch(() => undefined);
      const guess = card.locator('[class*="close" i], [class*="remove" i], [class*="delete" i], [class*="clear" i]').first();
      if (await isVisible(guess)) {
        await clickLocator(guess).catch(() => undefined);
      } else {
        // 3) last resort: the round X icon sits in the card's top-right corner
        const box = await card.boundingBox().catch(() => null);
        if (box) await page.mouse.click(box.x + box.width * 0.935, box.y + Math.min(box.height * 0.17, 26));
      }
    }
    await sleep(500);
  }
  if ((await betslipCount(page)) > 0) {
    throw new Error("Existing Bet9ja betslip could not be cleared safely.");
  }
}

async function waitForBetslipSelection(page, candidate) {
  const teams = new RegExp(`${escapeRegExp(candidate.home_team)}\\s*-\\s*${escapeRegExp(candidate.away_team)}`, "i");
  const roundPattern =
    candidate.season_id && candidate.round
      ? new RegExp(`${escapeRegExp(String(candidate.season_id))}\\s*-\\s*match\\s*day\\s*${Number(candidate.round)}\\b`, "i")
      : null;
  const deadline = Date.now() + 10000;
  let lastText = "";
  while (Date.now() < deadline) {
    const betslip = await findBetslipScope(page);
    if (betslip) {
      lastText = String((await betslip.innerText().catch(() => "")) || "").replace(/\s+/g, " ");
      // The "Betslip (n)" tab header may sit outside the slip panel, so fall back to the page-wide header.
      const count = (lastText.match(/betslip\s*\(\s*(\d+)\s*\)/i) || [])[1] || String(await betslipCount(page));
      const isPlainTotalOver15 = /total goals\s*over\s*1[.,]5/i.test(lastText) && !/(home|away)\s+total goals/i.test(lastText);
      if (count === "1" && teams.test(lastText) && !isPlainTotalOver15 && /over\s*1[.,]5/i.test(lastText)) {
        // Wrong market in the slip (e.g. Home Total Goals over 1.5). Never stake it.
        await clearBetslip(page).catch(() => undefined);
        throw new Error(`Betslip holds the wrong market (expected Total Goals over 1.5): ${JSON.stringify(lastText.slice(0, 300))}`);
      }
      if (count === "1" && teams.test(lastText) && isPlainTotalOver15) {
        if (verifyRound && roundPattern && !roundPattern.test(lastText)) {
          await clearBetslip(page).catch(() => undefined);
          throw new Error(`Betslip is for a different season/matchday than ${candidate.season_id} round ${candidate.round}: ${JSON.stringify(lastText.slice(0, 300))}`);
        }
        return betslip;
      }
    }
    await sleep(selectorPollMs);
  }
  throw new Error(
    `Bet9ja betslip did not show Over 1.5 for ${candidate.home_team} - ${candidate.away_team}. Betslip text: ${JSON.stringify(lastText.slice(0, 300))}`,
  );
}

// ---------- main placement flow ----------

async function placeCandidate(page, candidate) {
  await clearBetslip(page);

  // No seconds-left cutoff: the worker always attempts a valid candidate while the
  // round is still open for betting.
  const located = await locateOverMarket(page, candidate);
  if (located.skipReason) {
    return { action: "skip", reason: located.skipReason };
  }

  const minimumOdds = Number(candidate.minimum_odds || 0);
  const tableOdds = await readOptionOdds(located.market);
  if (typeof tableOdds === "number" && tableOdds < minimumOdds) {
    return {
      action: "skip",
      reason: `Over 1.5 odds ${tableOdds.toFixed(2)} were below the configured minimum ${minimumOdds.toFixed(2)}.`,
      observedOdds: tableOdds,
    };
  }

  await clickLocator(located.market);
  await shortWait();
  await maybeDismissOverlays(page);

  const betslip = await waitForBetslipSelection(page, candidate);

  // The betslip shows the price that will actually be staked; trust it over the table.
  const slipOdds = await readBetslipOdds(betslip);
  const effectiveOdds = typeof slipOdds === "number" ? slipOdds : tableOdds;
  if (typeof effectiveOdds === "number" && effectiveOdds < minimumOdds) {
    await clearBetslip(page).catch(() => undefined);
    return {
      action: "skip",
      reason: `Over 1.5 odds ${effectiveOdds.toFixed(2)} in the betslip were below the configured minimum ${minimumOdds.toFixed(2)}.`,
      observedOdds: effectiveOdds,
    };
  }

  await fillStake(betslip, candidate.stake_amount);
  // "Place bet NGN 0.00" stays grey until the ticket stake registers, so confirm it did.
  const stakeRegistered = await waitUntil(async () => (await readTicketStake(betslip)) === Number(candidate.stake_amount), 4000);
  if (!stakeRegistered) {
    throw new Error(`Betslip ticket stake did not update to ${candidate.stake_amount}.`);
  }

  if (dryRun) {
    return {
      action: "placed",
      quotedOdds: effectiveOdds || minimumOdds,
      betReference: "dry-run",
    };
  }

  const submit = await waitForExisting(betslip, config.submitSelectors, 10000);
  if (!submit) {
    throw new Error("Place bet button was not found.");
  }
  await clickLocator(submit);
  await shortWait();

  const confirmation = await firstExisting(page, config.confirmationSelectors);
  if (confirmation) {
    await clickLocator(confirmation).catch(() => undefined);
    await shortWait();
  }

  const success = await waitForVisibleExisting(page, config.successSelectors, confirmationTimeoutMs);
  if (!success) {
    const diagnostics = await formatBetConfirmationDiagnostics(page);
    throw new UnconfirmedBetError(
      "Bet9ja bet outcome is unknown: the success confirmation was not detected after submitting. " + diagnostics,
      candidate,
      safeUrl(page),
    );
  }

  const betReference = await readBetReference(page, confirmationTimeoutMs);
  // The browser is reused for the next bet, so dismiss the success panel if it offers to.
  const another = await firstExisting(page, ['text=/^\\s*place\\s+another\\s+bet\\s*$/i']);
  if (another) {
    await clickLocator(another).catch(() => undefined);
  }
  return {
    action: "placed",
    quotedOdds: effectiveOdds || minimumOdds,
    betReference,
  };
}

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => {
    void closeBrowser().finally(() => process.exit(0));
  });
}

main().catch(async (error) => {
  await reportRuntime({ state: "error", worker_id: workerId, last_error: normalizeError(error) });
  await closeBrowser().catch(() => undefined);
  process.exitCode = 1;
});
