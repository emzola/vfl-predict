const DEFAULT_LOGIN_URL = "https://virtual.bet9ja.com/commonmenu/CommonDesktopMenu/login";
const DEFAULT_INIT_URL = "https://virtual.bet9ja.com/commonmenu/CommonDesktopMenu/getinit";
const DEFAULT_TICKETS_BASE_URL = "https://rgspluslive-vs001.akamaized.net/betting-rest/bookmakers";
const DEFAULT_AUTH_ORIGIN = "https://virtual.bet9ja.com";
const DEFAULT_AUTH_REFERER = `${DEFAULT_AUTH_ORIGIN}/virtual/?s=new`;
const DEFAULT_TICKET_ORIGIN = "https://launcherlive-vs001.akamaized.net";
const DEFAULT_TICKET_REFERER = `${DEFAULT_TICKET_ORIGIN}/`;

export class Bet9jaError extends Error {
  constructor(message, code = "bet9ja_error") {
    super(message);
    this.name = "Bet9jaError";
    this.code = code;
  }
}

export class Bet9jaConfigurationError extends Bet9jaError {
  constructor(message) {
    super(message, "configuration_error");
    this.name = "Bet9jaConfigurationError";
  }
}

export class Bet9jaAuthError extends Bet9jaError {
  constructor(message) {
    super(message, "authentication_error");
    this.name = "Bet9jaAuthError";
  }
}

export class Bet9jaRejectedError extends Bet9jaError {
  constructor(message) {
    super(message, "bet_rejected");
    this.name = "Bet9jaRejectedError";
  }
}

export class Bet9jaAmbiguousError extends Bet9jaError {
  constructor(message) {
    super(message, "bet_outcome_unknown");
    this.name = "Bet9jaAmbiguousError";
  }
}

export class Bet9jaTransportError extends Bet9jaError {
  constructor(message) {
    super(message, "provider_transport_error");
    this.name = "Bet9jaTransportError";
  }
}

export class CookieJar {
  constructor() {
    this.cookies = new Map();
  }

  storeFromResponse(url, response) {
    const headers = response?.headers;
    if (!headers) return;
    let values = [];
    if (typeof headers.getSetCookie === "function") {
      values = headers.getSetCookie();
    } else {
      const combined = headers.get("set-cookie");
      if (combined) values = splitSetCookieHeader(combined);
    }
    for (const value of values) this.store(url, value);
  }

  store(url, raw) {
    if (!raw) return;
    let parsedURL;
    try {
      parsedURL = new URL(url);
    } catch {
      return;
    }
    const pieces = String(raw).split(";");
    const pair = pieces.shift() || "";
    const equals = pair.indexOf("=");
    if (equals <= 0) return;

    const name = pair.slice(0, equals).trim();
    const value = pair.slice(equals + 1).trim();
    const cookie = {
      name,
      value,
      domain: parsedURL.hostname.toLowerCase(),
      hostOnly: true,
      path: defaultCookiePath(parsedURL.pathname),
      secure: false,
      expiresAt: null,
    };

    for (const attribute of pieces) {
      const trimmed = attribute.trim();
      const separator = trimmed.indexOf("=");
      const attributeName = (separator >= 0 ? trimmed.slice(0, separator) : trimmed).trim().toLowerCase();
      const attributeValue = separator >= 0 ? trimmed.slice(separator + 1).trim() : "";
      if (attributeName === "domain" && attributeValue) {
        cookie.domain = attributeValue.replace(/^\./, "").toLowerCase();
        cookie.hostOnly = false;
      } else if (attributeName === "path" && attributeValue.startsWith("/")) {
        cookie.path = attributeValue;
      } else if (attributeName === "secure") {
        cookie.secure = true;
      } else if (attributeName === "max-age") {
        const seconds = Number(attributeValue);
        if (Number.isFinite(seconds)) cookie.expiresAt = Date.now() + seconds * 1000;
      } else if (attributeName === "expires") {
        const timestamp = Date.parse(attributeValue);
        if (Number.isFinite(timestamp)) cookie.expiresAt = timestamp;
      }
    }

    const key = cookieKey(cookie);
    if (cookie.expiresAt !== null && cookie.expiresAt <= Date.now()) {
      this.cookies.delete(key);
    } else {
      this.cookies.set(key, cookie);
    }
  }

  headerFor(url) {
    let parsedURL;
    try {
      parsedURL = new URL(url);
    } catch {
      return "";
    }
    const now = Date.now();
    const values = [];
    for (const [key, cookie] of this.cookies) {
      if (cookie.expiresAt !== null && cookie.expiresAt <= now) {
        this.cookies.delete(key);
        continue;
      }
      if (cookie.secure && parsedURL.protocol !== "https:") continue;
      if (!domainMatches(parsedURL.hostname.toLowerCase(), cookie)) continue;
      if (!pathMatches(parsedURL.pathname, cookie.path)) continue;
      values.push(`${cookie.name}=${cookie.value}`);
    }
    return values.join("; ");
  }

  clear() {
    this.cookies.clear();
  }

  get size() {
    return this.cookies.size;
  }
}

export class Bet9jaApiClient {
  constructor({
    loginURL = DEFAULT_LOGIN_URL,
    initURL = DEFAULT_INIT_URL,
    ticketsBaseURL = DEFAULT_TICKETS_BASE_URL,
    authOrigin = DEFAULT_AUTH_ORIGIN,
    authReferer = DEFAULT_AUTH_REFERER,
    ticketOrigin = DEFAULT_TICKET_ORIGIN,
    ticketReferer = DEFAULT_TICKET_REFERER,
    bookmakerID,
    apiKey,
    timeoutMs = 30000,
    fetchImpl = globalThis.fetch,
    cookieJar = new CookieJar(),
  } = {}) {
    this.loginURL = loginURL;
    this.initURL = initURL;
    this.ticketsBaseURL = ticketsBaseURL.replace(/\/+$/, "");
    this.authOrigin = String(authOrigin || DEFAULT_AUTH_ORIGIN).trim();
    this.authReferer = String(authReferer || `${this.authOrigin}/virtual/?s=new`).trim();
    this.ticketOrigin = String(ticketOrigin || DEFAULT_TICKET_ORIGIN).trim();
    this.ticketReferer = String(ticketReferer || `${this.ticketOrigin}/`).trim();
    this.bookmakerID = bookmakerID;
    this.apiKey = String(apiKey || "").trim();
    this.timeoutMs = Math.max(1000, Number(timeoutMs) || 30000);
    this.fetchImpl = fetchImpl;
    this.cookieJar = cookieJar;
    this.authenticated = false;
    this.credentials = null;
    this.lastLoginAt = null;
    this.sessionID = "";
  }

  get ticketsURL() {
    if (!this.sessionID || !this.apiKey || !Number.isFinite(Number(this.bookmakerID))) return "";
    return `${this.ticketsBaseURL}/${encodeURIComponent(String(this.bookmakerID))}/users/${encodeURIComponent(this.sessionID)}/tickets?key=${encodeURIComponent(this.apiKey)}`;
  }

  credentialsMatch(credentials) {
    return Boolean(
      this.credentials &&
      credentials &&
      this.credentials.username === credentials.username &&
      this.credentials.password === credentials.password,
    );
  }

  clearSession() {
    this.authenticated = false;
    this.cookieJar.clear();
    this.sessionID = "";
  }

  async ensureLoggedIn(credentials) {
    if (!credentials?.username || !credentials?.password) {
      throw new Bet9jaAuthError("Bet9ja dashboard credentials are not configured.");
    }
    if (this.authenticated && this.credentialsMatch(credentials)) return false;
    await this.login(credentials);
    return true;
  }

  async login(credentials) {
    if (!credentials?.username || !credentials?.password) {
      throw new Bet9jaAuthError("Bet9ja dashboard credentials are not configured.");
    }
    this.clearSession();
    const form = new URLSearchParams({
      username: String(credentials.username),
      password: String(credentials.password),
    });
    const result = await this.request(this.loginURL, {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json, text/plain, */*",
        Origin: this.authOrigin,
        Referer: this.authReferer,
      },
      body: form.toString(),
    });
    if (!result.response.ok) {
      throw new Bet9jaAuthError(`Bet9ja login failed with HTTP ${result.response.status}.`);
    }
    if (!loginSucceeded(result.payload)) {
      throw new Bet9jaAuthError(`Bet9ja login was rejected: ${providerMessage(result.payload) || "invalid credentials"}.`);
    }
    let initPayload;
    try {
      initPayload = await this.getInit();
    } catch (error) {
      this.clearSession();
      throw error;
    }
    const sessionID = extractSessionID(initPayload);
    if (!sessionID) {
      this.clearSession();
      throw new Bet9jaAuthError("Bet9ja getInit returned no session id after login.");
    }
    this.sessionID = sessionID;
    this.credentials = { username: String(credentials.username), password: String(credentials.password) };
    this.authenticated = true;
    this.lastLoginAt = new Date().toISOString();
    return { login: result.payload, init: initPayload };
  }

  async getInit() {
    const result = await this.request(this.initURL, {
      method: "GET",
      headers: {
        Accept: "*/*",
        Referer: this.authReferer,
      },
    });
    if (!result.response.ok) {
      throw new Bet9jaAuthError(`Bet9ja getInit failed with HTTP ${result.response.status}.`);
    }
    if (!loginSucceeded(result.payload)) {
      throw new Bet9jaAuthError(`Bet9ja getInit rejected the session: ${providerMessage(result.payload) || "authentication failed"}.`);
    }
    return result.payload;
  }

  async logout() {
    if (!this.authenticated) {
      this.clearSession();
      return null;
    }
    try {
      if (!this.ticketsURL) {
        throw new Bet9jaConfigurationError("Bet9ja bookmaker, API key, and current session settings are required to log out.");
      }
      const result = await this.request(this.ticketsURL, {
        method: "GET",
        headers: this.ticketRequestHeaders(),
      });
      if (!result.response.ok) {
        throw new Bet9jaError(`Bet9ja logout failed with HTTP ${result.response.status}.`, "logout_error");
      }
      return result.payload;
    } finally {
      this.clearSession();
    }
  }

  async placeBet(payload) {
    if (!this.authenticated) {
      throw new Bet9jaAuthError("Bet9ja session is not authenticated.");
    }
    if (!this.ticketsURL) {
      throw new Bet9jaConfigurationError("Bet9ja bookmaker, API key, and current session settings are required to place bets.");
    }
    let result;
    try {
      result = await this.request(this.ticketsURL, {
        method: "POST",
        headers: this.ticketRequestHeaders({ "Content-Type": "application/json" }),
        body: JSON.stringify(payload),
      });
    } catch (error) {
      throw new Bet9jaAmbiguousError(`Bet9ja bet request outcome is unknown: ${normalizeError(error)}`);
    }

    if (isAuthenticationFailure(result.payload, result.response.status)) {
      this.authenticated = false;
      const providerError = providerMessage(result.payload);
      const detail = providerError
        ? ` Provider response: ${providerError}`
        : ` Provider HTTP status: ${result.response.status}.`;
      // TEMPORARY DEBUG: expose both ticket-session values in the dashboard
      // error so production can confirm they are identical. Remove after the
      // session handoff check is complete.
      const ticketURLSessionID = String(this.sessionID || "<missing>");
      const payloadSessionID = String(payload?.[0]?.sender?.endCustomer?.id || "<missing>");
      throw new Bet9jaAuthError(
        `Bet9ja rejected the betting session as expired or unauthenticated.${detail} `
        + `TEMP SESSION CHECK — ticket URL session ID: ${ticketURLSessionID}; `
        + `payload endCustomer.id: ${payloadSessionID}.`,
      );
    }
    if (!result.response.ok) {
      throw new Bet9jaAmbiguousError(`Bet9ja returned HTTP ${result.response.status} after the bet request.`);
    }

    const accepted = findAcceptedTicket(result.payload);
    if (accepted) {
      return {
        ticketId: accepted.ticketId,
        ticketStatus: accepted.ticketStatus,
        quotedOdds: accepted.quotedOdds,
        message: accepted.message,
        payload: result.payload,
      };
    }
    if (isExplicitBetRejection(result.payload)) {
      throw new Bet9jaRejectedError(`Bet9ja rejected the bet: ${providerMessage(result.payload) || "provider rejected the ticket"}.`);
    }
    throw new Bet9jaAmbiguousError("Bet9ja returned no accepted ticket and no definitive rejection.");
  }

  async request(url, init = {}) {
    if (typeof this.fetchImpl !== "function") {
      throw new Bet9jaConfigurationError("Fetch is not available in the auto-betting worker.");
    }
    const headers = new Headers(init.headers || {});
    if (!headers.has("Accept")) headers.set("Accept", "application/json");
    const cookie = this.cookieJar.headerFor(url);
    if (cookie) headers.set("Cookie", cookie);

    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), this.timeoutMs);
    let response;
    try {
      response = await this.fetchImpl(url, { ...init, headers, signal: controller.signal });
    } catch (error) {
      throw new Bet9jaTransportError(`Bet9ja request failed: ${normalizeError(error)}`);
    } finally {
      clearTimeout(timeout);
    }
    this.cookieJar.storeFromResponse(url, response);
    const text = await response.text().catch(() => "");
    let payload = null;
    if (text.trim()) {
      try {
        payload = JSON.parse(text);
      } catch {
        payload = null;
      }
    }
    return { response, payload, text };
  }

  ticketRequestHeaders(extra = {}) {
    return {
      Accept: "*/*",
      Origin: this.ticketOrigin,
      Referer: this.ticketReferer,
      ...extra,
    };
  }
}

export function buildBetPayload(candidate, selection, {
  bookmakerID = 9478,
  sessionID = "",
  limitID = 1424,
  selectionID = "uof:6/sr:sport:1/18/12?total=1.5",
  productType = "VFLM",
  marketName = "Total Goals",
  outcomeName = "over 1.5",
  stakeScale = 10000,
  oddsScale = 10000,
} = {}) {
  if (!candidate || !selection) throw new Bet9jaConfigurationError("A betting candidate and live selection are required.");
  const matchID = Number(candidate.match_id ?? selection.match_id);
  const decimalOdds = Number(selection.decimal_odds);
  const stake = Number(candidate.stake_amount);
  const scheduledAt = Date.parse(candidate.scheduled_at);
  const normalizedBookmakerID = Number(bookmakerID);
  const normalizedLimitID = Number(limitID);
  const normalizedStakeScale = Number(stakeScale);
  const normalizedOddsScale = Number(oddsScale);
  const normalizedSessionID = String(sessionID || "").trim();
  if (!Number.isSafeInteger(matchID) || matchID <= 0) throw new Bet9jaConfigurationError("The candidate match id is invalid.");
  if (!Number.isFinite(decimalOdds) || decimalOdds <= 0) throw new Bet9jaConfigurationError("The live selection odds are invalid.");
  if (!Number.isFinite(stake) || stake <= 0) throw new Bet9jaConfigurationError("The candidate stake is invalid.");
  if (!Number.isFinite(scheduledAt) || scheduledAt <= 0) throw new Bet9jaConfigurationError("The candidate kickoff time is invalid.");
  if (!Number.isSafeInteger(normalizedBookmakerID) || normalizedBookmakerID <= 0) throw new Bet9jaConfigurationError("The Bet9ja bookmaker id is invalid.");
  if (!Number.isSafeInteger(normalizedLimitID) || normalizedLimitID <= 0) throw new Bet9jaConfigurationError("The Bet9ja limit id is invalid.");
  if (!normalizedSessionID) throw new Bet9jaConfigurationError("The current Bet9ja session id is required to place a bet.");
  if (!Number.isFinite(normalizedStakeScale) || normalizedStakeScale <= 0 || !Number.isFinite(normalizedOddsScale) || normalizedOddsScale <= 0) {
    throw new Bet9jaConfigurationError("Bet9ja ticket scales are invalid.");
  }
  const stakeValue = Math.round(stake * normalizedStakeScale);
  const oddsValue = Math.round(decimalOdds * normalizedOddsScale);
  if (!Number.isSafeInteger(stakeValue) || stakeValue <= 0 || !Number.isSafeInteger(oddsValue) || oddsValue <= 0) {
    throw new Bet9jaConfigurationError("Bet9ja ticket amount conversion is invalid.");
  }

  const homeUID = Number(selection.home_team_uid);
  const awayUID = Number(selection.away_team_uid);
  const homeProviderID = String(selection.home_team_provider_id || `sr:competitor:${homeUID}`);
  const awayProviderID = String(selection.away_team_provider_id || `sr:competitor:${awayUID}`);
  const round = Number(candidate.round);
  const outcome = String(outcomeName || selection.outcome_name || "over 1.5");
  const seasonYear = String(selection.season_year || "").trim();
  if (!seasonYear) throw new Bet9jaConfigurationError("The current season provider year is required to place a bet.");
  const tournamentName = `Season ${seasonYear}`;
  const eventName = `Matchday ${Number.isInteger(round) && round > 0 ? round : ""}`.trim();

  return [{
    version: "2.2",
    timestampUtc: String(Date.now()),
    sender: {
      bookmakerId: normalizedBookmakerID,
      channel: "internet",
      currency: "ngn",
      shopId: null,
      terminalId: null,
      limitId: normalizedLimitID,
      endCustomer: {
        id: normalizedSessionID,
        languageId: "en",
      },
    },
    selections: [{
      id: String(selectionID),
      eventId: `vf:match:${matchID}`,
      odds: oddsValue,
      convertedOdds: decimalOdds.toFixed(2),
      convertedOddsType: "dec",
      metaData: {
        lang: "EN-EN",
        marketName: String(marketName || selection.market_name || "Total Goals"),
        outcomeName: outcome,
        outrightType: null,
        marketId: null,
        product: productType,
        status: 1,
        tournament: {
          name: tournamentName,
          sequence: "",
          event: {
            competitors: [
              { id: homeProviderID, name: String(candidate.home_team || ""), abbr: String(selection.home_team_abbreviation || "") },
              { id: awayProviderID, name: String(candidate.away_team || ""), abbr: String(selection.away_team_abbreviation || "") },
            ],
            eventStart: String(scheduledAt),
            name: eventName,
            sequence: "",
          },
        },
      },
      productType,
      selectionIndex: 0,
    }],
    bets: [{
      selectionRefs: [{ selectionIndex: 0, banker: false }],
      selectedSystems: [1],
      stake: { type: "unit", value: String(stakeValue) },
    }],
  }];
}

function findAcceptedTicket(payload) {
  let accepted = null;
  walk(payload, (value) => {
    if (accepted || !value || typeof value !== "object" || Array.isArray(value)) return;
    const status = String(value.ticketStatus || "").toLowerCase();
    if (status === "accepted" && value.ticketId) {
      accepted = {
        ticketId: String(value.ticketId),
        ticketStatus: String(value.ticketStatus),
        quotedOdds: Number.isFinite(Number(value.convertedOdds)) ? Number(value.convertedOdds) : undefined,
        message: String(value.message || ""),
      };
    }
  });
  return accepted;
}

function isExplicitBetRejection(payload) {
  if (!payload || typeof payload !== "object") return false;
  if (Array.isArray(payload.errors) && payload.errors.length > 0) return true;
  if (Number.isFinite(Number(payload.statusCode)) && Number(payload.statusCode) !== 0) return true;
  let rejected = false;
  walk(payload, (value) => {
    if (rejected || !value || typeof value !== "object" || Array.isArray(value)) return;
    const status = String(value.ticketStatus || "").toLowerCase();
    if (status && status !== "accepted") rejected = true;
    if (Number.isFinite(Number(value.code)) && Number(value.code) !== 0) rejected = true;
  });
  return rejected;
}

function loginSucceeded(payload) {
  if (!payload || typeof payload !== "object") return false;
  const authStatus = payload?.data?.auth?.status;
  if (authStatus !== undefined && authStatus !== null) return Number(authStatus) === 1;
  return Number(payload.status) === 1;
}

function extractSessionID(payload) {
  return String(payload?.data?.auth?.data?.sessionId || "").trim();
}

function isAuthenticationFailure(payload, status) {
  if (status === 401 || status === 403) return true;
  if (!payload || typeof payload !== "object") return false;
  const authStatuses = [payload?.status, payload?.auth?.status, payload?.data?.auth?.status]
    .filter((value) => value !== undefined && value !== null)
    .map(Number);
  if (authStatuses.some((value) => value === -1)) return true;
  const message = JSON.stringify(payload).toLowerCase();
  return /rejected_session_validation|unauthori[sz]ed|authentication\s+(?:required|failed)|login\s+required|session\s+(?:expired|invalid|not\s+found|does\s+not\s+exist)|not\s+logged\s+in/.test(message);
}

function providerMessage(payload) {
  if (!payload || typeof payload !== "object") return "";
  const values = [];
  walk(payload, (value) => {
    if (!value || typeof value !== "object" || Array.isArray(value)) return;
    for (const key of ["message", "error", "description"]) {
      if (typeof value[key] === "string" && value[key].trim()) values.push(value[key].trim());
    }
  });
  if (typeof payload.message === "string" && payload.message.trim()) values.push(payload.message.trim());
  return [...new Set(values)].join("; ").slice(0, 300);
}

function walk(value, visit) {
  visit(value);
  if (!value || typeof value !== "object") return;
  for (const child of Array.isArray(value) ? value : Object.values(value)) walk(child, visit);
}

function normalizeError(error) {
  return error instanceof Error ? error.message : String(error);
}

function cookieKey(cookie) {
  return `${cookie.domain}|${cookie.path}|${cookie.name}`;
}

function domainMatches(host, cookie) {
  return cookie.hostOnly ? host === cookie.domain : host === cookie.domain || host.endsWith(`.${cookie.domain}`);
}

function pathMatches(pathname, cookiePath) {
  if (cookiePath === "/") return true;
  if (pathname === cookiePath) return true;
  return pathname.startsWith(cookiePath.endsWith("/") ? cookiePath : `${cookiePath}/`);
}

function defaultCookiePath(pathname) {
  if (!pathname || !pathname.startsWith("/")) return "/";
  const index = pathname.lastIndexOf("/");
  return index <= 0 ? "/" : pathname.slice(0, index);
}

function splitSetCookieHeader(value) {
  return String(value).split(/,(?=[^;,=]+=[^;,]+)/g).map((item) => item.trim()).filter(Boolean);
}

