import { expect, test } from "@playwright/test";

const fixtures = Array.from({ length: 8 }, (_, index) => ({
  match_id: 1000 + index,
  home_team: `Home ${index + 1}`,
  away_team: `Away ${index + 1}`,
  scheduled_at: "2030-01-01T12:00:00Z",
  status: "scheduled",
}));

test.beforeEach(async ({ context, page }) => {
  await context.addCookies([{ name: "vfl_session", value: "deterministic-test-session", url: "http://127.0.0.1:3100" }]);
  await page.route("**/api/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/auth/me")) {
      await route.fulfill({ json: { email: "owner@example.test" } });
      return;
    }
    if (path.endsWith("/dashboard/overview")) {
      await route.fulfill({
        json: {
          checked_at: "2030-01-01T10:00:00Z",
          current_season: { season_id: "season-test", provider_year: "2030" },
          current_round: { round: 1, state: "provisional", fixture_count: 8, earliest_kickoff: "2030-01-01T12:00:00Z" },
          upcoming_fixtures: fixtures,
          complete_frozen_rounds: 0,
          feed_freshness: { latest_feed_observation: "2030-01-01T10:00:00Z" },
          worker_status: [],
          forward_testing: { has_genuine_data: false, settled_fixtures: 0 },
          active_model: null,
        },
      });
      return;
    }
    await route.fulfill({ json: {} });
  });
});

test("renders a complete upcoming round from deterministic API fixtures", async ({ page }) => {
  await page.goto("/dashboard", { waitUntil: "commit" });
  await expect(page.getByRole("heading", { name: "Prediction workspace" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Round 1", exact: true })).toBeVisible();
  await expect(page.getByText("8 stored fixtures", { exact: true })).toBeVisible();
  await expect(page.getByText("Pending genuine data", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Home 8 vs Away 8" })).toBeVisible();
});

test("mobile navigation remains keyboard-visible", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/dashboard", { waitUntil: "commit" });
  await expect(page.getByRole("heading", { name: "Prediction workspace" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Round 1", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Menu" }).click();
  await expect(page.getByRole("navigation", { name: "Mobile navigation" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Predictions" })).toBeVisible();
});
