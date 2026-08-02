import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import WorkspacesPage from "./page";

const api = vi.hoisted(() => ({
  createWorkspace: vi.fn(),
  getWorkspaces: vi.fn(),
  listConnectedRepositories: vi.fn(),
}));
const auth = vi.hoisted(() => ({
  getToken: vi.fn(async () => "test-session-token"),
}));

vi.mock("@clerk/nextjs", () => ({
  useAuth: () => ({ getToken: auth.getToken }),
}));

// Only the network calls are stubbed; the pure status helpers stay real.
vi.mock("@/lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api")>()),
  createWorkspace: api.createWorkspace,
  getWorkspaces: api.getWorkspaces,
  listConnectedRepositories: api.listConnectedRepositories,
}));

const existingWorkspace = {
  id: "workspace-1",
  name: "Production",
  slug: "production",
  owner_id: "user-1",
  plan: "pro",
  role: "owner",
  created_at: "2026-07-13T00:00:00Z",
  log_retention_days: 30,
};

describe("workspace dashboard", () => {
  beforeEach(() => {
    api.createWorkspace.mockReset();
    api.getWorkspaces.mockReset();
    api.listConnectedRepositories.mockReset();
    auth.getToken.mockReset();
    auth.getToken.mockResolvedValue("test-session-token");
    api.getWorkspaces.mockResolvedValue({ workspaces: [existingWorkspace] });
    api.listConnectedRepositories.mockResolvedValue({ connections: [] });
  });

  it("renders the authenticated user's workspaces", async () => {
    render(<WorkspacesPage />);

    expect(await screen.findByRole("heading", { name: "Workspaces" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Production" })).toBeInTheDocument();
    expect(screen.getByText("PRO")).toBeInTheDocument();
    expect(api.getWorkspaces).toHaveBeenCalledWith("test-session-token");
  });

  it("creates a workspace and prepends it to the dashboard", async () => {
    api.getWorkspaces.mockResolvedValueOnce({ workspaces: [] });
    api.createWorkspace.mockResolvedValue({
      workspace: { ...existingWorkspace, id: "workspace-2", name: "Incident response", slug: "incident-response" },
    });
    render(<WorkspacesPage />);

    const nameInput = await screen.findByRole("textbox", { name: "Workspace name" });
    fireEvent.change(nameInput, { target: { value: "Incident response" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    expect(await screen.findByRole("heading", { name: "Incident response" })).toBeInTheDocument();
    expect(api.createWorkspace).toHaveBeenCalledWith("test-session-token", "Incident response");
  });

  it("flags a repository whose access was removed from the GitHub App", async () => {
    const connection = {
      id: "conn-1",
      workspace_id: "workspace-1",
      project_id: "project-1",
      full_name: "pranav8764/ParkIntel",
      html_url: "https://github.com/pranav8764/ParkIntel",
      selected_branch: "main",
      status: "connected",
      last_indexed_commit_sha: "abcdef1234567",
    };
    api.listConnectedRepositories.mockResolvedValue({
      connections: [
        {
          repository_connection: connection,
          latest_ingestion_job: { status: "succeeded" },
          project_name: "ParkIntel",
          workspace_name: "Production",
        },
        {
          repository_connection: { ...connection, id: "conn-2", full_name: "pranav8764/SentinelAPI", status: "revoked" },
          latest_ingestion_job: { status: "succeeded" },
          project_name: "SentinelAPI",
          workspace_name: "Production",
        },
      ],
    });
    render(<WorkspacesPage />);

    expect(await screen.findByText("Reconnect required")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Reconnect pranav8764/SentinelAPI" })).toBeInTheDocument();
    // The healthy connection keeps its ingestion badge.
    expect(screen.getByText("Indexed")).toBeInTheDocument();
  });

  it("shows a recoverable error when workspace loading fails", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    api.getWorkspaces.mockRejectedValueOnce(new Error("API is unavailable"));
    render(<WorkspacesPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent("API is unavailable");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(api.getWorkspaces).toHaveBeenCalledTimes(2));
    expect(await screen.findByRole("heading", { name: "Production" })).toBeInTheDocument();
    consoleError.mockRestore();
  });
});
