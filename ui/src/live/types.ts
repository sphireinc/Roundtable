export type WorkspaceEvent = {
  workspace_id: string;
  sequence: number;
  type: string;
  payload: Record<string, unknown>;
};
