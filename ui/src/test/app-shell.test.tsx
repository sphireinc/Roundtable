import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import React, { createElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { APIClient } from "@/api/client";
import { AppShell } from "@/components/app-shell";

vi.mock("next/navigation", () => ({ usePathname: () => "/dashboard" }));
vi.mock("@/components/context-controls", () => ({
  ContextControls: () => createElement("div", { "data-testid": "project-context-controls" }, "Workspace and branch controls"),
}));
vi.mock("@/components/primitives", () => ({
  IconButton: ({ label }: { label: string }) => createElement("button", { "aria-label": label }),
  StatusPill: ({ status }: { status: string }) => createElement("span", null, status),
}));

describe("AppShell project status strip", () => {
  beforeEach(() => window.localStorage.clear());

  it("groups project controls and transaction governance in one prominent region", () => {
    const queryClient = new QueryClient();
    render(<QueryClientProvider client={queryClient}><AppShell
      workspaceId="ws-1"
      buildVersion="test"
      api={{} as APIClient}
      wsUrl="ws://localhost/events"
      onWorkspaceChange={vi.fn()}
    ><div>Page content</div></AppShell></QueryClientProvider>);

    const strip = screen.getByRole("region", { name: "Project status" });
    expect(strip).toContainElement(screen.getByTestId("project-context-controls"));
    expect(strip).toHaveTextContent("Shared state");
    expect(strip).toHaveTextContent("authoritative");
    expect(strip).toHaveTextContent("transaction manager");
    expect(document.querySelector(".shell-runbar")).toBeNull();
  });
});
