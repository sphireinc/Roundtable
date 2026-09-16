import type { ProblemDetails } from "@/types/api";
import type { RuntimeConfig } from "./config";
export class APIError extends Error { constructor(public readonly problem: ProblemDetails) { super(problem.title); this.name = "APIError"; } }
export class APIClient {
  constructor(private readonly config: RuntimeConfig) {}
  async get<T>(path: string, signal?: AbortSignal): Promise<T> { return this.request<T>(path, { method: "GET", signal }); }
  private async request<T>(path: string, init: RequestInit): Promise<T> {
    const requestId = crypto.randomUUID(); const response = await fetch(new URL(path, this.config.apiBaseUrl), { ...init, headers: { Accept: "application/json", "X-Request-ID": requestId, "X-Workspace-ID": this.config.workspaceId, ...init.headers } });
    if (!response.ok) { const problem = await response.json().catch(() => ({ type: "about:blank", title: "Request failed", status: response.status, request_id: response.headers.get("X-Request-ID") || requestId })) as ProblemDetails; throw new APIError(problem); }
    return await response.json() as T;
  }
}
