import { expect, test } from "@playwright/test";

test("unauthenticated dashboard navigation reaches the private login page", async ({ page }) => {
  await page.goto("/dashboard", { waitUntil: "commit" });
  await expect(page).toHaveURL(/\/login\?next=%2Fdashboard/);
  await expect(page.getByRole("heading", { name: "Sign in without a password" })).toBeVisible();
  await expect(page.getByText("No public registration, passwords, social login, or guest mode.")).toBeVisible();
});

test("login response remains generic in the deterministic browser fixture", async ({ page }) => {
  await page.route("**/api/v1/auth/request-link", async (route) => {
    await route.fulfill({
      status: 202,
      contentType: "application/json",
      body: JSON.stringify({ message: "If the address is authorized, a sign-in link will be sent." }),
    });
  });
  await page.goto("/login", { waitUntil: "commit" });
  await page.getByLabel("Email address").fill("owner@example.test");
  await page.getByRole("button", { name: "Send sign-in link" }).click();
  await expect(page.getByRole("status")).toContainText("If the address is authorized");
  await expect(page.getByRole("status")).not.toContainText("token");
});
