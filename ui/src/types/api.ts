export type ProblemDetails = { type: string; title: string; status: number; detail?: string; request_id: string; code?: string };
export type HealthComponent = { status: string; details?: Record<string, unknown> };
export type NodeHealth = { status: "ok" | "degraded"; version: string; api_version: "v1"; time: string; request_id: string; components: Record<string, HealthComponent>; degraded_reasons?: string[] };
