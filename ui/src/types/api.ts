export type ProblemDetails = { type: string; title: string; status: number; detail?: string; instance?: string; request_id: string; code?: string; metadata?: Record<string, unknown> };
export type HealthComponent = { status: string; details?: Record<string, unknown> };
export type NodeHealth = { status: "ok" | "degraded"; version: string; api_version: "v1"; time: string; request_id: string; components: Record<string, HealthComponent>; degraded_reasons?: string[] };
