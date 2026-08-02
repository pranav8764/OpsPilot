import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  ApiError,
  INSTALLATION_REVOKED_CODE,
  getWorkspaces,
  isApiError,
  isInstallationRevokedError,
  isInstallationUsable,
  isRevokedConnection,
  repositoryConnectionHealth,
} from "./api";

function jsonResponse(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as unknown as Response;
}

describe("fetchAPI error reporting", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("carries the HTTP status and the gateway error code", async () => {
    vi.mocked(fetch).mockResolvedValue(
      jsonResponse(500, { error: "LIST_REPOS_FAILED", message: "failed to list repositories" })
    );

    const failure = await getWorkspaces("token").catch((error: unknown) => error);

    expect(isApiError(failure)).toBe(true);
    const apiError = failure as ApiError;
    expect(apiError).toBeInstanceOf(Error);
    expect(apiError.message).toBe("failed to list repositories");
    expect(apiError.status).toBe(500);
    expect(apiError.code).toBe("LIST_REPOS_FAILED");
  });

  it("recognises a 409 INSTALLATION_REVOKED", async () => {
    vi.mocked(fetch).mockResolvedValue(
      jsonResponse(409, { error: INSTALLATION_REVOKED_CODE, message: "installation belongs to another app" })
    );

    const failure = await getWorkspaces("token").catch((error: unknown) => error);

    expect(isInstallationRevokedError(failure)).toBe(true);
    expect((failure as ApiError).status).toBe(409);
  });

  it("prefers a `code` field when the envelope carries one", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(403, { code: "FORBIDDEN", error: "legacy" }));

    const failure = (await getWorkspaces("token").catch((error: unknown) => error)) as ApiError;

    expect(failure.code).toBe("FORBIDDEN");
  });

  it("still produces a specific error when the body is not JSON", async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => {
        throw new Error("not json");
      },
    } as unknown as Response);

    const failure = (await getWorkspaces("token").catch((error: unknown) => error)) as ApiError;

    expect(failure.message).toBe("API Error: 502");
    expect(failure.status).toBe(502);
    expect(failure.code).toBe("");
  });

  it("survives an error body of literal null", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(500, null));

    const failure = await getWorkspaces("token").catch((error: unknown) => error);

    expect(isApiError(failure)).toBe(true);
    expect((failure as ApiError).message).toBe("API Error: 500");
  });

  it("does not flag unrelated failures as revoked installations", () => {
    expect(isInstallationRevokedError(new Error("offline"))).toBe(false);
    expect(isInstallationRevokedError(new ApiError("conflict", 409, "APPROVAL_FAILED"))).toBe(false);
  });
});

describe("connection status helpers", () => {
  it("treats a missing or unknown status as connected", () => {
    expect(repositoryConnectionHealth(undefined)).toBe("connected");
    expect(repositoryConnectionHealth("")).toBe("connected");
    expect(repositoryConnectionHealth("connected")).toBe("connected");
    expect(repositoryConnectionHealth("something-new")).toBe("connected");
    expect(isRevokedConnection(undefined)).toBe(false);
  });

  it("recognises revoked and unhealthy connections", () => {
    expect(repositoryConnectionHealth("revoked")).toBe("revoked");
    expect(repositoryConnectionHealth("REVOKED")).toBe("revoked");
    expect(isRevokedConnection(" revoked ")).toBe(true);
    expect(repositoryConnectionHealth("archived")).toBe("unhealthy");
  });

  it("keeps installations selectable unless GitHub took the grant away", () => {
    expect(isInstallationUsable(undefined)).toBe(true);
    expect(isInstallationUsable("active")).toBe(true);
    expect(isInstallationUsable("error")).toBe(true);
    expect(isInstallationUsable("revoked")).toBe(false);
    expect(isInstallationUsable("suspended")).toBe(false);
    expect(isInstallationUsable("deleted")).toBe(false);
  });
});
