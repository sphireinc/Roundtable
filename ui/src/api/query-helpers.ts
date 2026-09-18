import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { queryKeys } from "./query-keys";

export function workspaceQueryKey(workspaceId: string, domain: string, filters?: Record<string, string>): QueryKey {
  return ["workspace", workspaceId, domain, filters ?? {}];
}

export function invalidateWorkspaceDomain(queryClient: QueryClient, workspaceId: string, domain: string): Promise<void> {
  return queryClient.invalidateQueries({ queryKey: ["workspace", workspaceId, domain] });
}

export function invalidateProposalState(queryClient: QueryClient, workspaceId: string): Promise<void> {
  return queryClient.invalidateQueries({ queryKey: ["workspace", workspaceId, "proposals"] });
}
