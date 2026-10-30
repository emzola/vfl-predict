import assert from "node:assert/strict";
import test from "node:test";

import {
  Bet9jaApiClient,
  Bet9jaAuthError,
  buildBetPayload,
} from "./bet9ja-api.mjs";

const candidate = {
  season_id: "46948",
  round: 26,
  match_id: 1412492414,
  home_team: "VL Vienna",
  away_team: "VL Prague",
  scheduled_at: "2026-10-30T01:00:00.000Z",
  stake_amount: 50,
};

const selection = {
  match_id: candidate.match_id,
  home_team_uid: 276504,
  home_team_provider_id: "sr:competitor:276504",
  home_team_abbreviation: "VIE",
  away_team_uid: 276515,
  away_team_provider_id: "sr:competitor:276515",
  away_team_abbreviation: "PRA",
  decimal_odds: 1.4,
  raw_odds: "1.40",
  outcome_name: "over 1.5",
};

test("buildBetPayload converts the documented ticket units and metadata", () => {
  const payload = buildBetPayload(candidate, selection, { sessionID: "session-1" });
  assert.equal(payload.length, 1);
  assert.equal(payload[0].sender.bookmakerId, 9478);
  assert.equal(payload[0].sender.endCustomer.id, "session-1");
  assert.equal(payload[0].selections[0].eventId, "vf:match:1412492414");
  assert.equal(payload[0].selections[0].id, "uof:6/sr:sport:1/18/12?total=1.5");
  assert.equal(payload[0].selections[0].odds, 14000);
  assert.equal(payload[0].selections[0].convertedOdds, "1.40");
  assert.equal(payload[0].bets[0].stake.value, "500000");
  assert.equal(payload[0].selections[0].metaData.tournament.event.competitors[0].id, "sr:competitor:276504");
});

test("Bet9jaApiClient reuses login cookies and parses an accepted ticket", async () => {
  const requests = [];
  const fetchImpl = async (url, init = {}) => {
    const parsed = new URL(url);
    const headers = new Headers(init.headers || {});
    requests.push({ method: init.method || "GET", path: parsed.pathname, cookie: headers.get("Cookie") || "", body: init.body || "" });
    if (parsed.pathname === "/login") {
      return new Response(JSON.stringify({ status: 1, data: { auth: { status: 1 } } }), {
        status: 200,
        headers: { "content-type": "application/json", "set-cookie": "sid=abc; Path=/; HttpOnly" },
      });
    }
    if (parsed.pathname === "/getinit") {
      return new Response(JSON.stringify({ status: 1, data: { auth: { status: 1, data: { sessionId: "session-1" } } } }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }
    if (init.method === "POST") {
      return new Response(JSON.stringify({
        data: [[{ ticketId: "ticket-1", ticketStatus: "accepted", code: 0, message: "PLACED" }]],
        errors: [],
        statusCode: 0,
      }), { status: 200, headers: { "content-type": "application/json" } });
    }
    return new Response(JSON.stringify({ data: { auth: { status: -1 } } }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  };

  const client = new Bet9jaApiClient({
    loginURL: "https://virtual.test/login",
    initURL: "https://virtual.test/getinit",
    ticketsBaseURL: "https://akamai.test/bookmakers",
    bookmakerID: 9478,
    apiKey: "test-key",
    fetchImpl,
  });
  await client.login({ username: "dashboard-user", password: "dashboard-password" });
  const placed = await client.placeBet([{ version: "2.2" }]);
  assert.equal(placed.ticketId, "ticket-1");
  assert.equal(client.sessionID, "session-1");
  await client.logout();
  assert.equal(requests[2].path.includes("/users/session-1/tickets"), true);
  assert.equal(requests[2].cookie, "sid=abc");
  assert.equal(requests[3].cookie, "sid=abc");
  assert.equal(client.authenticated, false);
});

test("session validation rejection is classified as authentication failure", async () => {
  const fetchImpl = async (url, init = {}) => {
    const parsed = new URL(url);
    if (init.method === "POST" && String(init.body).includes("username")) {
      return new Response(JSON.stringify({ status: 1, data: { auth: { status: 1 } } }), { status: 200 });
    }
    if (parsed.pathname === "/getinit") {
      return new Response(JSON.stringify({ status: 1, data: { auth: { status: 1, data: { sessionId: "session-1" } } } }), { status: 200 });
    }
    return new Response(JSON.stringify({
      errors: [{ message: "REJECTED_SESSION_VALIDATION: Session does not exist." }],
      statusCode: 1,
    }), { status: 200 });
  };
  const client = new Bet9jaApiClient({
    loginURL: "https://virtual.test/login",
    initURL: "https://virtual.test/getinit",
    ticketsBaseURL: "https://akamai.test/bookmakers",
    bookmakerID: 9478,
    apiKey: "test-key",
    fetchImpl,
  });
  await client.login({ username: "user", password: "password" });
  await assert.rejects(() => client.placeBet([{ version: "2.2" }]), Bet9jaAuthError);
  assert.equal(client.authenticated, false);
});

test("an explicit provider auth response becomes a safe re-login signal", async () => {
  const fetchImpl = async (url, init = {}) => {
    const parsed = new URL(url);
    if (init.method === "POST" && String(init.body).includes("username")) {
      return new Response(JSON.stringify({ status: 1, data: { auth: { status: 1 } } }), { status: 200 });
    }
    if (parsed.pathname === "/getinit") {
      return new Response(JSON.stringify({ status: 1, data: { auth: { status: 1, data: { sessionId: "session-1" } } } }), { status: 200 });
    }
    return new Response(JSON.stringify({ status: -1, data: { auth: { status: -1 } } }), { status: 200 });
  };
  const client = new Bet9jaApiClient({
    loginURL: "https://tickets.test/login",
    initURL: "https://tickets.test/getinit",
    ticketsBaseURL: "https://tickets.test/bookmakers",
    bookmakerID: 9478,
    apiKey: "test-key",
    fetchImpl,
  });
  await client.login({ username: "user", password: "password" });
  await assert.rejects(() => client.placeBet([{ version: "2.2" }]), Bet9jaAuthError);
  assert.equal(client.authenticated, false);
});

