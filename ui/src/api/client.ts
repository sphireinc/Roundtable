import type { ProblemDetails } from "@/types/api";
import type { RuntimeConfig } from "./config";
export class APIError extends Error {
  constructor(public readonly problem: ProblemDetails) {
    super(problem.title);
    this.name = "APIError";
  }
  get requestId(): string { return this.problem.request_id; }
}

export class APIClient {
  constructor(private readonly config: RuntimeConfig) {}

  async get<T>(path: string, signal?: AbortSignal): Promise<T> { return this.request<T>(path, { method: "GET", signal }); }
  async post<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> { return this.request<T>(path, { method: "POST", body: JSON.stringify(body), signal }); }
  async patch<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> { return this.request<T>(path, { method: "PATCH", body: JSON.stringify(body), signal }); }
  async delete<T>(path: string, signal?: AbortSignal): Promise<T> { return this.request<T>(path, { method: "DELETE", signal }); }

  private async request<T>(path: string, init: RequestInit): Promise<T> {
    const requestId = typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `req-${Date.now()}`;
    const headers = new Headers(init.headers);
    headers.set("Accept", "application/json");
    headers.set("X-Request-ID", requestId);
    headers.set("X-Workspace-ID", this.config.workspaceId);
    if (init.body !== undefined) headers.set("Content-Type", "application/json");
    const response = await fetch(new URL(path, this.config.apiBaseUrl), { ...init, headers });
    if (!response.ok) throw new APIError(await this.problemFrom(response, requestId));
    if (response.status === 204) return undefined as T;
    const contentType = response.headers.get("content-type") || "";
    if (contentType.includes("application/json")) return await response.json() as T;
    return await response.text() as T;
  }

  private async problemFrom(response: Response, fallbackRequestId: string): Promise<ProblemDetails> {
    const requestId = response.headers.get("X-Request-ID") || fallbackRequestId;
    const fallback: ProblemDetails = { type: "about:blank", title: "Request failed", status: response.status, request_id: requestId };
    try {
      const value = await response.json() as Partial<ProblemDetails>;
      return { ...fallback, ...value, status: value.status || response.status, request_id: value.request_id || requestId };
    } catch {
      return { ...fallback, detail: response.statusText || "The server returned an error." };
    }
  }
}
