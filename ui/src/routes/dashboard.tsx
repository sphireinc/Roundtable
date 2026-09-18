"use client";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { APIClient } from "@/api/client";
import { loadRuntimeConfig } from "@/api/config";
import { queryKeys } from "@/api/query-keys";
import type { NodeHealth } from "@/types/api";
import { ConfigError } from "@/components/config-error";
import { StatusBadge } from "@/components/status-badge";
import { AppShell } from "@/components/app-shell";
export function DashboardRoute() {
  const result = loadRuntimeConfig(); const config = "config" in result ? result.config : null;
  const [workspaceId, setWorkspaceId] = useState(config?.workspaceId ?? "unconfigured");
  const selectedConfig = config ? { ...config, workspaceId } : null;
  const api = selectedConfig ? new APIClient(selectedConfig) : null;
  const query = useQuery({ queryKey: queryKeys.health(workspaceId), queryFn: ({ signal }) => api!.get<NodeHealth>("/api/v1/status", signal), enabled: api !== null });
  if (!config) return <ConfigError message={"error" in result ? result.error : "UI configuration is unavailable"} />;
  return <AppShell workspaceId={workspaceId} buildVersion={config.buildVersion} health={query.data} api={api!} wsUrl={config.wsUrl} onWorkspaceChange={setWorkspaceId}><section className="content"><header className="page-heading"><div><p className="eyebrow">Workspace / {workspaceId}</p><h1>Control center</h1></div></header><section className="hero"><div><p className="eyebrow">Authoritative node status</p><h2>Governance at a glance</h2><p className="muted">Server state is loaded over REST and reconciled from the workspace event stream.</p></div>{query.isPending && <div className="skeleton card" aria-label="Loading status" />}{query.isError && <div className="card" role="alert"><StatusBadge status="error" /><p>Unable to reach the API.</p><button className="button secondary" onClick={() => query.refetch()}>Retry</button></div>}{query.data && <div className="card"><StatusBadge status={query.data.status} /><p className="metric">{Object.keys(query.data.components).length}</p><p className="muted">health components</p><time dateTime={query.data.time}>{query.data.time}</time></div>}</section>{query.data?.degraded_reasons?.length ? <section className="notice" role="status"><strong>Degraded:</strong> {query.data.degraded_reasons.join("; ")}</section> : null}<section className="grid" aria-label="Control center sections"><article className="panel"><p className="eyebrow">Live queue</p><h3>Proposals</h3><p className="metric">—</p><p className="muted">Select a workspace to load governed proposals.</p></article><article className="panel"><p className="eyebrow">Attention</p><h3>Human approvals</h3><p className="metric">—</p><p className="muted">No client-side completion is inferred.</p></article><article className="panel"><p className="eyebrow">Event stream</p><h3>Reconnection</h3><StatusBadge status="ready" /><p className="muted">Live connection state is shown above; reconnecting views remain visibly stale.</p></article></section></section></AppShell>;
}
