"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { APIClient } from "@/api/client";
import type { BranchSwitchPreflight, BranchSwitchResponse, RepositoryStatus, Workspace, WorkspaceList } from "@/types/api";
import { ErrorState, Skeleton, StatusPill, ToolbarButton } from "./primitives";

type StreamState = "connecting" | "connected" | "reconnecting" | "disconnected";

function useWorkspaceStream(wsUrl: string, workspaceId: string): StreamState {
  const [state, setState] = useState<StreamState>("connecting");
  const socketRef = useRef<WebSocket | null>(null);
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const url = wsUrl.replace(/\/workspaces\/[^/]+\//, `/workspaces/${encodeURIComponent(workspaceId)}/`);
    const connect = () => {
      if (cancelled) return;
      setState(socketRef.current ? "reconnecting" : "connecting");
      try {
        const socket = new WebSocket(url);
        socketRef.current = socket;
        socket.onopen = () => setState("connected");
        socket.onclose = () => { socketRef.current = null; if (!cancelled) { setState("reconnecting"); timer = setTimeout(connect, 2000); } };
        socket.onerror = () => setState("disconnected");
      } catch { setState("disconnected"); timer = setTimeout(connect, 2000); }
    };
    connect();
    return () => { cancelled = true; if (timer) clearTimeout(timer); socketRef.current?.close(); socketRef.current = null; };
  }, [wsUrl, workspaceId]);
  return state;
}

function shortSha(sha: string): string { return sha ? sha.slice(0, 8) : "unknown"; }

export function ContextControls({ api, wsUrl, workspaceId, onWorkspaceChange }: { api: APIClient; wsUrl: string; workspaceId: string; onWorkspaceChange: (id: string) => void }) {
  const queryClient = useQueryClient();
  const stream = useWorkspaceStream(wsUrl, workspaceId);
  const workspaces = useQuery({ queryKey: ["workspaces"], queryFn: ({ signal }) => api.get<WorkspaceList>("/api/v1/workspaces", signal) });
  const repository = useQuery({ queryKey: ["workspace", workspaceId, "repository"], queryFn: ({ signal }) => api.get<RepositoryStatus>(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/repository`, signal) });
  const [targetBranch, setTargetBranch] = useState("");
  const [dialogOpen, setDialogOpen] = useState(false);
  const [preflight, setPreflight] = useState<BranchSwitchPreflight | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const activeWorkspace = useMemo(() => workspaces.data?.items.find(item => item.id === workspaceId), [workspaces.data, workspaceId]);
  const switchWorkspace = (next: string) => { if (!next || next === workspaceId) return; queryClient.removeQueries({ queryKey: ["workspace", workspaceId] }); onWorkspaceChange(next); };
  const prepareBranchSwitch = async () => {
    const target = targetBranch.trim();
    if (!target || target === repository.data?.branch) return;
    setPending(true); setError(null);
    try {
      const result = await api.post<BranchSwitchPreflight>(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/repository/branch-switch/preflight`, { branch: target }, undefined, { "X-Actor-ID": "human", "X-Actor-Role": "human" });
      setPreflight(result); setDialogOpen(true);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Branch preflight failed"); }
    finally { setPending(false); }
  };
  const executeBranchSwitch = async () => {
    if (!preflight?.allowed) return;
    setPending(true); setError(null);
    try {
      const result = await api.post<BranchSwitchResponse>(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/repository/branch-switch`, { branch: preflight.target_branch }, undefined, { "X-Actor-ID": "human", "X-Actor-Role": "human", "Idempotency-Key": crypto.randomUUID() });
      queryClient.setQueryData(["workspace", workspaceId, "repository"], result.after); setTargetBranch(""); setDialogOpen(false); setPreflight(null);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Branch switch failed"); }
    finally { setPending(false); }
  };
  const status = repository.data;
  return <div className="context-controls">
    <div className="context-field"><label htmlFor="workspace-select">Workspace</label>{workspaces.isPending ? <Skeleton width="170px" height={34} /> : workspaces.isError ? <button className="context-error" onClick={() => workspaces.refetch()}>Workspace unavailable · Retry</button> : <select id="workspace-select" value={workspaceId} onChange={event => switchWorkspace(event.target.value)} aria-label="Workspace">{workspaces.data?.items.length ? workspaces.data.items.map(item => <option value={item.id} key={item.id}>{item.display_name} · {item.root_alias || item.id}</option>) : <option value={workspaceId}>No registered workspaces</option>}</select>}</div>
    <div className="context-field"><label htmlFor="branch-target">Branch</label><div className="branch-control"><input id="branch-target" value={targetBranch || status?.branch || ""} onChange={event => setTargetBranch(event.target.value)} placeholder={status?.branch || "unknown"} aria-describedby="branch-status" disabled={!status || status.detached || pending} /><ToolbarButton onClick={prepareBranchSwitch} disabled={!targetBranch.trim() || targetBranch.trim() === status?.branch || pending}>Review switch</ToolbarButton></div></div>
    <div id="branch-status" className="repository-summary" aria-live="polite">{repository.isPending && <Skeleton width="260px" height={34} />}{repository.isError && <button className="context-error" onClick={() => repository.refetch()}>Repository unavailable · Retry</button>}{status && <><StatusPill status={status.detached ? "detached HEAD" : status.dirty ? "dirty" : "clean"} /><span>HEAD {shortSha(status.head_sha)}</span><span>{status.ahead} ahead · {status.behind} behind</span></>}</div>
    <div className="stream-status" title={stream === "connected" ? "Workspace event stream connected" : "Workspace event stream is reconnecting or disconnected"}><StatusPill status={stream} /><span className="sr-only">{stream === "connected" ? "Live updates connected" : "Live updates are stale until the stream reconnects"}</span></div>
    {activeWorkspace?.status !== "active" && <div className="notice" role="alert"><strong>Workspace unavailable.</strong> This workspace is no longer active on the server. Select another registered workspace or retry.</div>}
    {error && <div className="context-error-block" role="alert"><strong>Action failed.</strong> {error} <button onClick={() => setError(null)}>Dismiss</button></div>}
    {dialogOpen && <div className="dialog-backdrop"><dialog open aria-labelledby="branch-dialog-title" className="rt-dialog"><h2 id="branch-dialog-title">Review branch switch</h2>{preflight?.blockers.length ? <><p>This switch is blocked by authoritative governance state:</p><ul>{preflight.blockers.map(blocker => <li key={blocker.code}>{blocker.message}{blocker.count ? ` (${blocker.count})` : ""}</li>)}</ul><p className="rt-muted">Resolve the blockers, refresh repository status, and retry.</p></> : <p>Switch the governed workspace repository to <strong>{preflight?.target_branch}</strong>? This is a human action and may change the active context for every live view.</p>}<div className="dialog-actions"><ToolbarButton onClick={() => { setDialogOpen(false); setPreflight(null); }} disabled={pending}>Cancel</ToolbarButton>{preflight?.allowed && <ToolbarButton onClick={executeBranchSwitch} disabled={pending}>{pending ? "Switching…" : "Confirm switch"}</ToolbarButton>}</div></dialog></div>}
  </div>;
}
