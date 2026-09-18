import { expect, test } from "@playwright/test";
test("configuration state is explicit and accessible", async ({ page }) => { await page.goto("/dashboard"); await expect(page.locator("body")).toContainText(/Configuration required|Control center/); });
test("dashboard exposes a retryable server failure", async ({ page }) => {
  await page.route("**/api/v1/status", route => route.fulfill({ status: 503, contentType: "application/problem+json", body: JSON.stringify({ title: "Unavailable", status: 503, request_id: "req-e2e" }) }));
  await page.goto("/dashboard");
  await expect(page.getByText("Unable to reach the API")).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry", exact: true })).toBeVisible();
});

test("dashboard shows authoritative workspace and repository context", async ({ page }) => {
  await page.route("**/api/v1/workspaces", route => { const id = route.request().headers()["x-workspace-id"] || "ws-test"; return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [{ id, display_name: "Roundtable", root_alias: "roundtable", canonical_repository_identity: "local", status: "active", default_branch: "main", created_at: new Date().toISOString(), revision: 1 }], next_cursor: null }) }); });
  await page.route("**/api/v1/workspaces/*/repository", route => { const id = route.request().url().match(/workspaces\/([^/]+)\//)?.[1] || "ws-test"; return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ workspace_id: id, branch: "main", head_sha: "0123456789abcdef", dirty: false, ahead: 0, behind: 0, detached: false, remotes: [], index: [], protected_paths: { changed: 0 } }) }); });
  await page.route("**/api/v1/status", route => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ status: "ok", version: "test", api_version: "v1", time: new Date().toISOString(), request_id: "req-e2e", components: {} }) }));
  await page.goto("/dashboard");
  await expect(page.getByLabel("Workspace")).toBeVisible();
  await expect(page.getByText("HEAD 012345")).toBeVisible();
  await expect(page.getByText("clean")).toBeVisible();
});
