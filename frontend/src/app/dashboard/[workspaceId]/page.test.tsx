import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, INSTALLATION_REVOKED_CODE, INSTALLATION_REVOKED_MESSAGE } from "@/lib/api";
import WorkspaceDetailPage from "./page";

const api = vi.hoisted(() => ({
  getWorkspace: vi.fn(),
  getProjects: vi.fn(),
  getGitHubIntegrationConfig: vi.fn(),
  listGitHubInstallations: vi.fn(),
  listGitHubRepositories: vi.fn(),
  listLogProviderConnections: vi.fn(),
  getWorkspaceUsage: vi.fn(),
  getRepositoryConnection: vi.fn(),
  createGitHubInstallSession: vi.fn(),
}));

const auth = vi.hoisted(() => ({ getToken: vi.fn() }));
const nav = vi.hoisted(() => {
  const replace = vi.fn();
  return {
    replace,
    // next/navigation returns a referentially stable router; mirror that.
    router: { replace, push: vi.fn(), refresh: vi.fn() },
    searchParams: new URLSearchParams(),
  };
});

vi.mock("@clerk/nextjs", () => ({
  useAuth: () => ({ getToken: auth.getToken }),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => nav.router,
  usePathname: () => "/dashboard/workspace-1",
  useSearchParams: () => nav.searchParams,
}));

// Only the network calls are stubbed; the pure status helpers stay real so the
// tests exercise the same branching the browser does.
vi.mock("@/lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api")>()),
  ...api,
}));

const workspace = {
  id: "workspace-1",
  name: "Production",
  slug: "production",
  owner_id: "user-1",
  plan: "pro",
  role: "owner",
  created_at: "2026-07-13T00:00:00Z",
  log_retention_days: 30,
};

const installation = {
  id: "installation-1",
  installation_id: 142620052,
  account_login: "pranav8764",
  account_type: "User",
  repository_selection: "selected",
  status: "active",
  installed_at: "2026-07-13T00:00:00Z",
};

const repository = {
  repository_id: 1,
  owner: "pranav8764",
  name: "ParkIntel",
  full_name: "pranav8764/ParkIntel",
  html_url: "https://github.com/pranav8764/ParkIntel",
  clone_url: "https://github.com/pranav8764/ParkIntel.git",
  default_branch: "main",
  private: false,
};

/**
 * `use()` reads `status`/`value` off an already-settled thenable. Next.js hands
 * the route one; a bare Promise stays pending forever under jsdom.
 */
function settledParams<T>(value: T): Promise<T> {
  return Object.assign(Promise.resolve(value), { status: "fulfilled", value });
}

function renderPage() {
  return render(<WorkspaceDetailPage params={settledParams({ workspaceId: "workspace-1" })} />);
}

beforeEach(() => {
  vi.spyOn(console, "error").mockImplementation(() => undefined);
  nav.replace.mockReset();
  nav.searchParams = new URLSearchParams();
  auth.getToken.mockResolvedValue("test-session-token");
  api.getWorkspace.mockResolvedValue({ workspace });
  api.getProjects.mockResolvedValue({ projects: [] });
  api.getGitHubIntegrationConfig.mockResolvedValue({
    install_url: "https://github.com/apps/archiatros-sre",
    callback_url: "",
    webhook_url: "",
    configured: true,
  });
  api.listGitHubInstallations.mockResolvedValue({ installations: [installation] });
  api.listGitHubRepositories.mockResolvedValue({ repositories: [repository] });
  api.listLogProviderConnections.mockResolvedValue({ connections: [] });
  api.getWorkspaceUsage.mockResolvedValue(null);
  api.getRepositoryConnection.mockResolvedValue({
    repository_connection: null,
    latest_ingestion_job: null,
  });
});

describe("workspace GitHub connection panel", () => {
  it("lists only the repositories the installation grants", async () => {
    renderPage();

    const picker = await screen.findByLabelText("Repository");
    expect(picker).toHaveTextContent("pranav8764/ParkIntel");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("surfaces a repository-list failure with a retry instead of an empty dropdown", async () => {
    api.listGitHubRepositories.mockRejectedValueOnce(
      new ApiError("failed to list repositories", 500, "LIST_REPOS_FAILED")
    );
    renderPage();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Repositories could not be loaded");
    expect(alert).toHaveTextContent("failed to list repositories");
    expect(alert).toHaveTextContent("HTTP 500 LIST_REPOS_FAILED");

    // The misleading empty picker must be gone entirely.
    expect(screen.queryByText("-- Select Repository --")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Repository")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Retry/ }));

    expect(await screen.findByLabelText("Repository")).toHaveTextContent("pranav8764/ParkIntel");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("never offers to install the App when the installations call fails", async () => {
    api.listGitHubInstallations.mockRejectedValueOnce(
      new ApiError("failed to list installations", 500, "LIST_FAILED")
    );
    renderPage();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("GitHub installations could not be loaded");
    expect(alert).toHaveTextContent("HTTP 500 LIST_FAILED");
    expect(screen.queryByRole("button", { name: /Install Archiatros App/ })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Retry/ }));

    expect(await screen.findByLabelText("Repository")).toBeInTheDocument();
  });

  it("explains a 409 INSTALLATION_REVOKED in plain language", async () => {
    api.listGitHubRepositories.mockRejectedValue(
      new ApiError("installation belongs to another app", 409, INSTALLATION_REVOKED_CODE)
    );
    renderPage();

    expect(await screen.findByRole("alert")).toHaveTextContent(INSTALLATION_REVOKED_MESSAGE);
  });

  it("distinguishes no installations from a failed installations call", async () => {
    api.listGitHubInstallations.mockResolvedValue({ installations: [] });
    renderPage();

    expect(
      await screen.findByRole("button", { name: /Install Archiatros App/ })
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("refetches after returning from a completed install", async () => {
    nav.searchParams = new URLSearchParams("github=connected");
    renderPage();

    expect(await screen.findByText(/GitHub App connected/)).toBeInTheDocument();
    await waitFor(() => expect(api.listGitHubInstallations).toHaveBeenCalledTimes(2));
    // The marker is cleared so a manual reload does not refetch again.
    await waitFor(() =>
      expect(nav.replace).toHaveBeenCalledWith("/dashboard/workspace-1", { scroll: false })
    );
  });

  it("keeps revoked repositories out of the picker", async () => {
    api.listGitHubRepositories.mockResolvedValue({
      repositories: [
        repository,
        { ...repository, repository_id: 2, full_name: "pranav8764/Removed", status: "revoked" },
      ],
    });
    renderPage();

    const picker = await screen.findByLabelText("Repository");
    expect(picker).toHaveTextContent("pranav8764/ParkIntel");
    expect(picker).not.toHaveTextContent("pranav8764/Removed");
  });

  it("keeps a pending repository choice across a refresh", async () => {
    renderPage();

    const picker = (await screen.findByLabelText("Repository")) as HTMLSelectElement;
    fireEvent.change(picker, { target: { value: "1" } });
    expect(screen.getByLabelText("Project Name")).toHaveValue("ParkIntel");

    fireEvent.click(screen.getAllByRole("button", { name: /Refresh/ })[0]);

    await waitFor(() => expect(api.listGitHubRepositories).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getByLabelText("Repository")).toHaveValue("1"));
  });

  it("drops a repository choice that GitHub no longer offers", async () => {
    renderPage();

    const picker = (await screen.findByLabelText("Repository")) as HTMLSelectElement;
    fireEvent.change(picker, { target: { value: "1" } });

    // Access removed in GitHub between the first load and the refresh.
    api.listGitHubRepositories.mockResolvedValue({ repositories: [] });
    fireEvent.click(screen.getAllByRole("button", { name: /Refresh/ })[0]);

    expect(
      await screen.findByText(/does not grant Archiatros access to any repository/)
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Repository")).not.toBeInTheDocument();
  });

  it("does not read a failed config call as an unconfigured backend", async () => {
    api.getGitHubIntegrationConfig.mockRejectedValue(
      new ApiError("failed to fetch GitHub configuration", 500, "GITHUB_CONFIG_FAILED")
    );
    renderPage();

    // The connection UI still works off the installations that did load.
    expect(await screen.findByLabelText("Repository")).toBeInTheDocument();
    expect(screen.queryByText(/GitHub App is not configured on the backend/)).not.toBeInTheDocument();
  });

  it("asks for a reconnect when the installation itself was revoked", async () => {
    api.listGitHubInstallations.mockResolvedValue({
      installations: [{ ...installation, status: "revoked" }],
    });
    renderPage();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Reconnect required");
    expect(alert).toHaveTextContent("pranav8764");
    expect(screen.getByRole("button", { name: /Reconnect GitHub App/ })).toBeInTheDocument();
    // A revoked installation can never serve repositories, so it is not offered.
    expect(screen.queryByLabelText("Repository")).not.toBeInTheDocument();
    expect(api.listGitHubRepositories).not.toHaveBeenCalled();
  });
});

describe("project repository status", () => {
  beforeEach(() => {
    api.getProjects.mockResolvedValue({
      projects: [
        {
          id: "project-1",
          workspace_id: "workspace-1",
          name: "ParkIntel",
          description: "",
          repo_url: "",
          repo_provider: "github",
          default_branch: "main",
          status: "active",
          indexing_status: "succeeded",
          created_at: "2026-07-13T00:00:00Z",
          updated_at: "2026-07-13T00:00:00Z",
        },
      ],
    });
  });

  const connection = {
    id: "conn-1",
    workspace_id: "workspace-1",
    project_id: "project-1",
    full_name: "pranav8764/ParkIntel",
    html_url: "https://github.com/pranav8764/ParkIntel",
    selected_branch: "main",
    status: "connected",
  };

  it("renders a revoked connection as Reconnect required with a reconnect action", async () => {
    api.getRepositoryConnection.mockResolvedValue({
      repository_connection: { ...connection, status: "revoked" },
      latest_ingestion_job: null,
    });
    renderPage();

    expect(await screen.findByText("Reconnect required")).toBeInTheDocument();
    expect(
      await screen.findByText(/Access to this repository was removed from the GitHub App/)
    ).toBeInTheDocument();
    expect(screen.queryByText("Connected")).not.toBeInTheDocument();
  });

  it("treats a missing status as connected", async () => {
    api.getRepositoryConnection.mockResolvedValue({
      repository_connection: { ...connection, status: undefined },
      latest_ingestion_job: null,
    });
    renderPage();

    expect(await screen.findByText("Connected")).toBeInTheDocument();
    expect(screen.queryByText("Reconnect required")).not.toBeInTheDocument();
  });
});
