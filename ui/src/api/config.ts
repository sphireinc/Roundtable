export type RuntimeConfig = { apiBaseUrl: string; wsUrl: string; workspaceId: string; buildVersion: string; enableDevMocks: boolean };
export type ConfigResult = { config: RuntimeConfig } | { error: string };
const publicEnv: Record<string, string | undefined> = {
  // Keep these as direct process.env references: Next.js statically inlines
  // NEXT_PUBLIC_* values into the browser bundle.
  NEXT_PUBLIC_API_BASE_URL: process.env.NEXT_PUBLIC_API_BASE_URL,
  NEXT_PUBLIC_WS_URL: process.env.NEXT_PUBLIC_WS_URL,
  NEXT_PUBLIC_WORKSPACE_ID: process.env.NEXT_PUBLIC_WORKSPACE_ID,
  NEXT_PUBLIC_BUILD_VERSION: process.env.NEXT_PUBLIC_BUILD_VERSION,
  NEXT_PUBLIC_ENABLE_DEV_MOCKS: process.env.NEXT_PUBLIC_ENABLE_DEV_MOCKS,
};

export function loadRuntimeConfig(env: Record<string, string | undefined> = publicEnv): ConfigResult {
  const apiBaseUrl = env.NEXT_PUBLIC_API_BASE_URL?.trim(); const wsUrl = env.NEXT_PUBLIC_WS_URL?.trim(); const workspaceId = env.NEXT_PUBLIC_WORKSPACE_ID?.trim();
  const missing = [!apiBaseUrl && "NEXT_PUBLIC_API_BASE_URL", !wsUrl && "NEXT_PUBLIC_WS_URL", !workspaceId && "NEXT_PUBLIC_WORKSPACE_ID"].filter(Boolean) as string[];
  if (missing.length > 0) return { error: `UI configuration is incomplete. Missing: ${missing.join(", ")}` };
  return { config: { apiBaseUrl: apiBaseUrl!, wsUrl: wsUrl!, workspaceId: workspaceId!, buildVersion: env.NEXT_PUBLIC_BUILD_VERSION?.trim() || "dev", enableDevMocks: env.NEXT_PUBLIC_ENABLE_DEV_MOCKS === "true" } };
}
