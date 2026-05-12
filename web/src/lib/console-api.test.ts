import { afterEach, describe, expect, test, vi } from "vitest";

import { deleteProviderModel, runProviderModelHealthcheck } from "./console-api";

vi.mock("./session", () => ({
  clearConsoleSession: vi.fn(),
  getConsoleSession: () => null,
}));

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("console api", () => {
  test("deleteProviderModel encodes provider model id", async () => {
    const fetchMock = vi.fn(async () => {
      return new Response(JSON.stringify({ deleted_id: "ok" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    await deleteProviderModel("route:provider_demo:folder/model v1");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/admin/provider-models/route%3Aprovider_demo%3Afolder%2Fmodel%20v1",
      { method: "DELETE" },
    );
  });

  test("runProviderModelHealthcheck encodes provider model id", async () => {
    const fetchMock = vi.fn(async () => {
      return new Response(JSON.stringify({ item: { id: "ok" } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    await runProviderModelHealthcheck("route:provider_demo:folder/model v1");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/admin/provider-models/route%3Aprovider_demo%3Afolder%2Fmodel%20v1/health-check",
      { method: "POST" },
    );
  });
});
