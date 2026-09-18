import { describe, expect, it, vi } from "vitest";
import { APIClient, APIError } from "@/api/client";

const config = { apiBaseUrl: "http://api.test", wsUrl: "ws://api.test/events", workspaceId: "ws-test", buildVersion: "test", enableDevMocks: false };

describe("APIClient", () => {
  it("adds workspace/request headers and parses JSON", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    await new APIClient(config).get<{ ok: boolean }>("/api/v1/status");
    const request = fetchMock.mock.calls[0][1] as RequestInit;
    const headers = request.headers as Headers;
    expect(headers.get("X-Workspace-ID")).toBe("ws-test");
    expect(headers.get("X-Request-ID")).toBeTruthy();
  });

  it("normalizes RFC 7807 errors with request IDs", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ type: "problem", title: "Conflict", status: 409, code: "stale_base", request_id: "req-server" }), { status: 409, headers: { "content-type": "application/problem+json", "X-Request-ID": "req-server" } })));
    await expect(new APIClient(config).post("/api/v1/workspaces/ws-test/proposals", {})).rejects.toMatchObject({ problem: { type: "problem", title: "Conflict", status: 409, code: "stale_base", request_id: "req-server" } } satisfies Partial<APIError>);
  });

  it("preserves explicit human headers for governed mutations", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    await new APIClient(config).post("/api/v1/workspaces/ws-test/repository/branch-switch/preflight", { branch: "feature" }, undefined, { "X-Actor-ID": "human", "X-Actor-Role": "human" });
    const headers = fetchMock.mock.calls[0][1].headers as Headers;
    expect(headers.get("X-Actor-ID")).toBe("human");
    expect(headers.get("X-Actor-Role")).toBe("human");
  });
});
