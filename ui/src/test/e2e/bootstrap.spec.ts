import { expect, test } from "@playwright/test";
test("missing configuration is explicit and accessible", async ({ page }) => { await page.goto("/dashboard"); await expect(page.getByRole("alert")).toContainText("Configuration required"); });
test("dashboard exposes a retryable server failure", async ({ page }) => {
  await page.route("**/api/v1/status", route => route.fulfill({ status: 503, contentType: "application/problem+json", body: JSON.stringify({ title: "Unavailable", status: 503, request_id: "req-e2e" }) }));
  await page.goto("/dashboard");
  await expect(page.getByRole("alert")).toContainText("Unable to reach the API");
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
});
