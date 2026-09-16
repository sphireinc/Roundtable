import { expect, test } from "@playwright/test";
test("missing configuration is explicit and accessible", async ({ page }) => { await page.goto("/dashboard"); await expect(page.getByRole("alert")).toContainText("Configuration required"); });
