"use client";
import { useEffect, type ReactNode } from "react";
import { usePathname } from "next/navigation";
import type { NodeHealth } from "@/types/api";
import type { APIClient } from "@/api/client";
import { useUIState } from "@/state/ui-state";
import { IconButton, StatusPill } from "./primitives";
import { ContextControls } from "./context-controls";

const navigation = [
  ["/dashboard", "Dashboard", "D"], ["/deliberations", "Deliberations", "D"], ["/proposals", "Proposals", "P"], ["/consensus", "Consensus", "C"], ["/claims", "Claims", "C"], ["/sessions", "Sessions", "S"], ["/memory", "Memory Oracle", "M"], ["/policies", "Policies", "P"], ["/approvals", "Approvals", "A"], ["/transactions", "Transactions", "T"], ["/repository", "Repository", "R"], ["/agents", "Agents", "A"], ["/logs", "Logs", "L"], ["/settings", "Settings", "S"],
] as const;

export function AppShell({ workspaceId, buildVersion, health, api, wsUrl, onWorkspaceChange, children }: { workspaceId: string; buildVersion: string; health?: NodeHealth; api: APIClient; wsUrl: string; onWorkspaceChange: (id: string) => void; children: ReactNode }) {
  const pathname = usePathname();
  const { sidebarCollapsed, setSidebarCollapsed } = useUIState();
  useEffect(() => {
    const saved = window.localStorage.getItem("roundtable.sidebar-collapsed");
    if (saved !== null) setSidebarCollapsed(saved === "true");
  }, [setSidebarCollapsed]);
  useEffect(() => { window.localStorage.setItem("roundtable.sidebar-collapsed", String(sidebarCollapsed)); }, [sidebarCollapsed]);
  const database = health?.components.database?.status ?? "unknown";
  const eventStream = health?.components.event_stream?.status ?? "unknown";
  const degraded = health?.degraded_reasons?.length ?? 0;
  return <div className={`app-shell ${sidebarCollapsed ? "rail-collapsed" : ""}`}>
    <aside className="sidebar" aria-label="Roundtable navigation">
      <div className="sidebar-header"><p className="brand">ROUND<span>TABLE</span></p><IconButton label={sidebarCollapsed ? "Expand navigation" : "Collapse navigation"} onClick={() => setSidebarCollapsed(!sidebarCollapsed)}>{sidebarCollapsed ? "→" : "←"}</IconButton></div>
      {!sidebarCollapsed && <p className="muted">Control plane</p>}
      <nav className="primary-nav" aria-label="Primary">
        {navigation.map(([href, label, icon]) => <a className={pathname === href || (href !== "/dashboard" && pathname.startsWith(`${href}/`)) ? "active" : ""} href={href} key={href} title={sidebarCollapsed ? label : undefined} aria-current={pathname === href ? "page" : undefined}><span className="nav-icon" aria-hidden="true">{icon}</span><span className="nav-label">{label}</span></a>)}
      </nav>
      <div className="sidebar-node"><p className="rt-eyebrow">{sidebarCollapsed ? "NODE" : "Local node"}</p>{!sidebarCollapsed && <><StatusPill status={health?.status ?? "unknown"} /><p className="node-detail">API {health?.version ?? "unknown"} · SQLite {database}</p><p className="node-detail">Workspace / {workspaceId}</p><p className="node-detail">Events <StatusPill status={eventStream} /></p>{degraded > 0 && <p className="node-warning" role="status">{degraded} degraded warning{degraded === 1 ? "" : "s"}</p>}</>}</div>
    </aside>
    <section className="shell-main">
      <header className="shell-topbar"><ContextControls api={api} wsUrl={wsUrl} workspaceId={workspaceId} onWorkspaceChange={onWorkspaceChange} /><div className="shell-actions"><a className="toolbar-link" href="/search">Search</a><a className="toolbar-link" href="/notifications">Notifications</a><details className="user-menu"><summary>Human user</summary><div className="user-menu-panel"><span>Local operator</span><a href="/settings">Settings</a></div></details><span className="build">build {buildVersion}</span><StatusPill status={health?.status ?? "unknown"} /></div></header>
      <div className="shell-runbar"><span className="rt-eyebrow">Run state</span><StatusPill status="authoritative" /><span className="rt-muted">State changes are governed by the API transaction manager.</span></div>
      <main className="shell-content">{children}</main>
    </section>
  </div>;
}
