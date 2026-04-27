import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { AppLayout } from "../app/layout";
import { createTestRouter } from "../app/router";
import type { ConsoleSession } from "../lib/session";

const defaultConsoleSession: ConsoleSession = {
  role: "admin",
  user_id: "user_admin_demo",
};

const useConsoleSessionMock = vi.fn<() => ConsoleSession>(() => defaultConsoleSession);

vi.mock("../lib/session", () => ({
  getDefaultSession: () => defaultConsoleSession,
  getConsoleSession: () => useConsoleSessionMock(),
  useConsoleSession: () => useConsoleSessionMock(),
}));

type MockResponseMap = Record<string, unknown>;
type MockRequestAssertions = Partial<Record<string, (init?: RequestInit) => void>>;

function defaultSystemStatus() {
  return {
    console_stage: "控制台预览版",
    run_mode: "数据库模式",
    gateway_health: "健康",
    quota_protection: "已启用",
    console_entry: "31873",
    gateway_admin_api: "32658",
    internal_services: ["31427"],
    hidden_modules: ["RAG 控制台", "知识库"],
  };
}

function mockSession(session: Partial<ConsoleSession> = {}) {
  useConsoleSessionMock.mockReturnValue({
    ...defaultConsoleSession,
    ...session,
  });
}

function renderRoute(path: string = "/") {
  render(
    <RouterProvider router={createTestRouter([path])} future={{ v7_startTransition: true }} />,
  );
}

function mockFetch(responses: MockResponseMap, requestAssertions: MockRequestAssertions = {}) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    requestAssertions[url]?.(init);

    if (!(url in responses)) {
      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    }

    return new Response(JSON.stringify(responses[url]), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });

  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

beforeEach(() => {
  vi.restoreAllMocks();
  mockSession();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("控制台路由", () => {
  test("admin session 渲染 admin navigation", async () => {
    mockSession({ role: "admin" });
    mockFetch({
      "/api/admin/system/status": defaultSystemStatus(),
    });

    render(
      <RouterProvider
        router={createTestRouter(["/applications"])}
        future={{ v7_startTransition: true }}
      />,
    );

    expect(await screen.findByRole("link", { name: "账号申请" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "租户管理" })).toBeInTheDocument();
  });

  test("member session 渲染 member navigation", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    mockFetch({
      "/api/admin/system/status": defaultSystemStatus(),
    });

    render(
      <RouterProvider router={createTestRouter(["/me"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByRole("link", { name: "我的总览" })).toBeInTheDocument();
    expect(screen.queryByText("账号申请")).not.toBeInTheDocument();
  });

  test("member session 只请求 /me/overview 且不会请求任何 /api/admin/* 接口", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/me/overview") {
        return new Response(
          JSON.stringify({
            tenant_id: "tenant_demo",
            tenant_name: "Demo Tenant",
            active_api_keys: 2,
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(
      <RouterProvider router={createTestRouter(["/me"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByRole("heading", { level: 1, name: "我的总览" })).toBeInTheDocument();
    await waitFor(() => {
      expect(fetchMock.mock.calls.map(([input]) => String(input))).toEqual(["/me/overview"]);
    });
  });

  test("member session 访问根路径时会跳转到 /me", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    mockFetch({
      "/me/overview": {
        tenant_id: "tenant_demo",
        tenant_name: "Demo Tenant",
        active_api_keys: 1,
      },
    });

    render(
      <RouterProvider router={createTestRouter(["/"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByRole("heading", { level: 1, name: "我的总览" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "我的总览" })).toHaveAttribute("aria-current", "page");
  });

  test("AppLayout 在空导航时使用安全兜底元信息", async () => {
    const fetchMock = mockFetch({
      "/api/admin/system/status": defaultSystemStatus(),
    });
    const router = createMemoryRouter(
      [
        {
          path: "/",
          element: <AppLayout navigation={[]} />,
          children: [{ index: true, element: <div>空导航内容</div> }],
        },
      ],
      { initialEntries: ["/"] },
    );

    render(<RouterProvider router={router} future={{ v7_startTransition: true }} />);

    expect(await screen.findByText("空导航内容")).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "控制台" })).toBeInTheDocument();
    expect(screen.getByText("请选择左侧导航以查看对应页面。")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/system/status");
  });

  test("总览页使用 /api/admin/overview 数据", async () => {
    const fetchMock = mockFetch({
      "/api/admin/overview": {
        stats: [
          { label: "24 小时请求量", value: "128 万" },
          { label: "成功率", value: "99.42%" },
          { label: "配额使用率", value: "74%" },
          { label: "活跃 API 密钥", value: "184" },
        ],
        route_health: [{ columns: ["gpt-4o-mini", "OpenAI 主线路由", "218 ms", "健康"] }],
        top_models: [{ columns: ["gpt-4o-mini", "612k", "48%", "对话"] }],
        recent_alerts: [{ columns: ["09:42", "配额告警", "tenant_beta"] }],
        audit_snapshot: [{ columns: ["tenant_alpha", "/v1/chat/completions", "200"] }],
      },
    });

    renderRoute();

    expect(await screen.findByRole("heading", { level: 1, name: "总览" })).toBeInTheDocument();
    expect(screen.getByText("24 小时请求量")).toBeInTheDocument();
    expect(screen.getByText("OpenAI 主线路由")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/overview");
  });

  test("控制台导航隐藏知识库入口", async () => {
    mockFetch({
      "/api/admin/overview": {
        stats: [],
        route_health: [],
        top_models: [],
        recent_alerts: [],
        audit_snapshot: [],
      },
      "/api/admin/system/status": defaultSystemStatus(),
    });

    renderRoute("/");

    expect(await screen.findByRole("heading", { level: 1, name: "总览" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "知识库" })).not.toBeInTheDocument();
  });

  test("侧栏隐藏左下状态块但顶部 badge 继续展示系统状态", async () => {
    const fetchMock = mockFetch({
      "/api/admin/overview": {
        stats: [],
        route_health: [],
        top_models: [],
        recent_alerts: [],
        audit_snapshot: [],
      },
      "/api/admin/system/status": defaultSystemStatus(),
    });

    renderRoute("/");

    expect(await screen.findByRole("heading", { level: 1, name: "总览" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /控制台阶段/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /启动模式/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /网关健康/ })).not.toBeInTheDocument();
    expect(screen.queryByText("控制台能力边界")).not.toBeInTheDocument();
    expect(screen.getByText("健康")).toBeInTheDocument();
    expect(screen.getByText("配额保护 已启用")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/system/status");
  });

  test("API 密钥页使用 /api/admin/api-keys 数据", async () => {
    const fetchMock = mockFetch({
      "/api/admin/api-keys": {
        items: [
          {
            id: "key_1",
            name: "生产网关",
            tenant: "tenant_alpha",
            status: "启用",
            scopes: ["chat", "rag"],
            last_used_at: "2 分钟前",
          },
        ],
        credential_mode: "平台密钥与上游凭证分离管理。",
      },
    });

    renderRoute("/api-keys");

    expect(await screen.findByRole("heading", { level: 1, name: "API 密钥" })).toBeInTheDocument();
    expect(await screen.findByText("生产网关")).toBeInTheDocument();
    expect(screen.getByText("平台密钥与上游凭证分离管理。")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/api-keys");
  });

  test("API 密钥页新建后只展示脱敏值并可复制完整密钥", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", {
      clipboard: { writeText },
    });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/api-keys" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [],
            credential_mode: "平台 API Key 与上游凭据分离",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/api-keys" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          tenant_id: "tenant_alpha",
          name: "new-key",
          scopes: ["chat"],
        });

        return new Response(
          JSON.stringify({
            item: {
              id: "pak_new",
              name: "new-key",
              tenant: "tenant_alpha",
              status: "启用",
              scopes: ["chat"],
              last_used_at: "2026-04-24T12:00:00+08:00",
            },
            raw_key: "ak_live_new_secret",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/api-keys");

    fireEvent.click(await screen.findByRole("button", { name: "新建密钥" }));
    fireEvent.change(screen.getByLabelText("租户 ID"), { target: { value: "tenant_alpha" } });
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "new-key" } });
    fireEvent.click(screen.getByRole("button", { name: /选择权限范围/ }));
    expect(screen.getByLabelText("chat")).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

    expect(await screen.findByText("新建密钥已完成")).toBeInTheDocument();
    expect(
      screen.getByText((content) => content.includes("一次性密钥：") && content.includes("••••")),
    ).toBeInTheDocument();
    expect(document.body).not.toHaveTextContent("ak_live_new_secret");
    fireEvent.click(screen.getByRole("button", { name: "复制完整密钥" }));
    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("ak_live_new_secret");
      expect(screen.getByText("完整密钥已复制到剪贴板。")).toBeInTheDocument();
    });
    expect(screen.getByText("new-key")).toBeInTheDocument();
  });

  test("API 密钥页在无 navigator.clipboard 时也能回退复制完整密钥", async () => {
    const execCommand = vi.fn().mockReturnValue(true);
    Object.defineProperty(document, "execCommand", {
      value: execCommand,
      configurable: true,
    });
    vi.stubGlobal("navigator", {});

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/api-keys" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [],
            credential_mode: "平台 API Key 与上游凭据分离",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/api-keys" && init?.method === "POST") {
        return new Response(
          JSON.stringify({
            item: {
              id: "pak_new",
              name: "fallback-key",
              tenant: "tenant_alpha",
              status: "启用",
              scopes: ["chat"],
              last_used_at: "2026-04-24T12:00:00+08:00",
            },
            raw_key: "ak_live_fallback_secret",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/api-keys");

    fireEvent.click(await screen.findByRole("button", { name: "新建密钥" }));
    fireEvent.change(screen.getByLabelText("租户 ID"), { target: { value: "tenant_alpha" } });
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "fallback-key" } });
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

    expect(await screen.findByText("新建密钥已完成")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "复制完整密钥" }));

    await waitFor(() => {
      expect(execCommand).toHaveBeenCalledWith("copy");
      expect(screen.getByText("完整密钥已复制到剪贴板。")).toBeInTheDocument();
    });
  });

  test("API 密钥页在 clipboard.writeText 被拒绝时也会继续回退复制", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("NotAllowedError"));
    const execCommand = vi.fn().mockReturnValue(true);
    Object.defineProperty(document, "execCommand", {
      value: execCommand,
      configurable: true,
    });
    vi.stubGlobal("navigator", {
      clipboard: { writeText },
    });

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/api-keys" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [],
            credential_mode: "平台 API Key 与上游凭据分离",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/api-keys" && init?.method === "POST") {
        return new Response(
          JSON.stringify({
            item: {
              id: "pak_new",
              name: "clipboard-reject-key",
              tenant: "tenant_alpha",
              status: "启用",
              scopes: ["chat"],
              last_used_at: "2026-04-24T12:00:00+08:00",
            },
            raw_key: "ak_live_clipboard_reject_secret",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/api-keys");

    fireEvent.click(await screen.findByRole("button", { name: "新建密钥" }));
    fireEvent.change(screen.getByLabelText("租户 ID"), { target: { value: "tenant_alpha" } });
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "clipboard-reject-key" } });
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

    expect(await screen.findByText("新建密钥已完成")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "复制完整密钥" }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("ak_live_clipboard_reject_secret");
      expect(execCommand).toHaveBeenCalledWith("copy");
      expect(screen.getByText("完整密钥已复制到剪贴板。")).toBeInTheDocument();
    });
  });

  test("API 密钥页轮换时回填当前权限范围并可复制完整密钥", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", {
      clipboard: { writeText },
    });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/api-keys" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                id: "pak_live_console",
                name: "生产网关",
                tenant: "tenant_alpha",
                status: "启用",
                scopes: ["chat", "rag"],
                last_used_at: "2026-04-24T11:58:00+08:00",
              },
            ],
            credential_mode: "平台 API Key 与上游凭据分离",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/api-keys/pak_live_console/rotate" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          name: "生产网关-轮换",
          scopes: ["chat", "rag"],
        });

        return new Response(
          JSON.stringify({
            item: {
              id: "pak_rotated",
              name: "生产网关-轮换",
              tenant: "tenant_alpha",
              status: "启用",
              scopes: ["chat", "rag"],
              last_used_at: "2026-04-24T12:01:00+08:00",
            },
            raw_key: "ak_live_rotated_secret",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/api-keys");

    fireEvent.click(await screen.findByRole("button", { name: "选择 生产网关" }));
    fireEvent.click(screen.getByRole("button", { name: "轮换密钥" }));
    fireEvent.click(screen.getByRole("button", { name: "选择新权限范围" }));
    expect(screen.getByLabelText("chat")).toBeChecked();
    expect(screen.getByLabelText("rag")).toBeChecked();
    fireEvent.change(screen.getByLabelText("新名称"), { target: { value: "生产网关-轮换" } });
    fireEvent.click(screen.getByRole("button", { name: "确认轮换" }));

    expect(await screen.findByText("轮换操作已完成")).toBeInTheDocument();
    expect(document.body).not.toHaveTextContent("ak_live_rotated_secret");
    fireEvent.click(screen.getByRole("button", { name: "复制完整密钥" }));
    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("ak_live_rotated_secret");
      expect(screen.getByText("完整密钥已复制到剪贴板。")).toBeInTheDocument();
    });
    expect(screen.getByText("生产网关-轮换")).toBeInTheDocument();
  });

  test("API 密钥页可以切换权限范围并删除未使用密钥", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/api-keys" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                id: "pak_unused",
                name: "unused-key",
                tenant: "tenant_alpha",
                status: "启用",
                scopes: ["chat"],
                last_used_at: "2026-04-24T12:00:00+08:00",
              },
            ],
            credential_mode: "平台 API Key 与上游凭据分离",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/api-keys" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          tenant_id: "tenant_alpha",
          name: "scope-test",
          scopes: ["rag"],
        });

        return new Response(
          JSON.stringify({
            item: {
              id: "pak_scope",
              name: "scope-test",
              tenant: "tenant_alpha",
              status: "启用",
              scopes: ["rag"],
              last_used_at: "2026-04-24T12:00:00+08:00",
            },
            raw_key: "ak_scope_secret",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/api-keys/pak_unused" && init?.method === "DELETE") {
        return new Response(
          JSON.stringify({
            item: {
              id: "pak_unused",
              name: "unused-key",
              tenant: "tenant_alpha",
              status: "已删除",
              scopes: ["chat"],
              last_used_at: "2026-04-24T12:00:00+08:00",
            },
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/api-keys");

    fireEvent.click(await screen.findByRole("button", { name: "新建密钥" }));
    fireEvent.change(screen.getByLabelText("租户 ID"), { target: { value: "tenant_alpha" } });
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "scope-test" } });
    fireEvent.click(screen.getByRole("button", { name: "选择权限范围" }));
    fireEvent.click(screen.getByLabelText("chat"));
    fireEvent.click(screen.getByLabelText("rag"));
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

    expect(await screen.findByText("scope-test")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "选择 unused-key" }));
    fireEvent.click(screen.getByRole("button", { name: "删除密钥" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

    await waitFor(() => {
      expect(screen.queryByText("unused-key")).not.toBeInTheDocument();
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/admin/api-keys/pak_unused",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  test("API 密钥页支持停用已选中的密钥", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/api-keys" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                id: "pak_live_console",
                name: "生产网关",
                tenant: "tenant_alpha",
                status: "启用",
                scopes: ["chat", "rag"],
                last_used_at: "2026-04-24T11:58:00+08:00",
              },
            ],
            credential_mode: "平台 API Key 与上游凭据分离",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/api-keys/pak_live_console/deactivate" && init?.method === "POST") {
        return new Response(
          JSON.stringify({
            item: {
              id: "pak_live_console",
              name: "生产网关",
              tenant: "tenant_alpha",
              status: "停用",
              scopes: ["chat", "rag"],
              last_used_at: "2026-04-24T11:58:00+08:00",
            },
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/api-keys");

    fireEvent.click(await screen.findByRole("button", { name: "选择 生产网关" }));
    fireEvent.click(screen.getByRole("button", { name: "停用密钥" }));
    fireEvent.click(screen.getByRole("button", { name: "确认停用" }));

    expect(await screen.findByText("停用操作已完成")).toBeInTheDocument();
    expect(screen.getByText("停用")).toBeInTheDocument();
  });

  test("账号申请页使用 /api/admin/applications 数据并支持审批", async () => {
    mockSession({ role: "admin", user_id: "user_admin_demo" });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/applications" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                id: "app_pending",
                email: "pending@example.com",
                name: "待审批用户",
                company_name: "Pending Co",
                use_case: "租户接入",
                status: "pending",
                created_at: "2026-04-25T09:02:03+08:00",
              },
            ],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/applications/app_pending/approve" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          actor_id: "user_admin_demo",
          comment: "通过控制台审批",
          tenant_id: "tenant_demo",
        });

        return new Response(
          JSON.stringify({
            item: {
              id: "app_pending",
              email: "pending@example.com",
              name: "待审批用户",
              company_name: "Pending Co",
              use_case: "租户接入",
              status: "approved",
              created_at: "2026-04-25T09:02:03+08:00",
            },
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/applications");

    expect((await screen.findAllByText("待审批用户")).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "选择 待审批用户" }));
    fireEvent.change(screen.getByLabelText("租户 ID"), { target: { value: "tenant_demo" } });
    fireEvent.click(screen.getByRole("button", { name: "审批通过" }));

    expect(await screen.findByText("审批已完成")).toBeInTheDocument();
    expect(screen.getByText("approved")).toBeInTheDocument();
  });

  test("账号申请页在审批提交中锁定上下文，成功结果不受后续选择或租户输入漂移影响", async () => {
    mockSession({ role: "admin", user_id: "user_admin_demo" });
    let resolveApproval: (() => void) | null = null;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/applications" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                id: "app_alice",
                email: "alice@example.com",
                name: "Alice",
                company_name: "Alice Co",
                use_case: "租户接入",
                status: "pending",
                created_at: "2026-04-25T09:02:03+08:00",
              },
              {
                id: "app_bob",
                email: "bob@example.com",
                name: "Bob",
                company_name: "Bob Co",
                use_case: "分析集成",
                status: "pending",
                created_at: "2026-04-25T10:02:03+08:00",
              },
            ],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/applications/app_alice/approve" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          actor_id: "user_admin_demo",
          comment: "通过控制台审批",
          tenant_id: "tenant_alice",
        });

        return new Promise<Response>((resolve) => {
          resolveApproval = () =>
            resolve(
              new Response(
                JSON.stringify({
                  item: {
                    id: "app_alice",
                    email: "alice@example.com",
                    name: "Alice",
                    company_name: "Alice Co",
                    use_case: "租户接入",
                    status: "approved",
                    created_at: "2026-04-25T09:02:03+08:00",
                  },
                }),
                {
                  status: 200,
                  headers: { "Content-Type": "application/json" },
                },
              ),
            );
        });
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/applications");

    expect(await screen.findByRole("button", { name: "选择 Alice" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("租户 ID"), { target: { value: "tenant_alice" } });
    fireEvent.click(screen.getByRole("button", { name: "审批通过" }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/admin/applications/app_alice/approve",
        expect.objectContaining({ method: "POST" }),
      );
    });

    expect(screen.getByRole("button", { name: "选择 Bob" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "重置" })).toBeDisabled();

    resolveApproval?.();

    expect(await screen.findByText("审批已完成")).toBeInTheDocument();
    expect(screen.getByText("申请人：Alice")).toBeInTheDocument();
    expect(screen.getByText("租户：tenant_alice")).toBeInTheDocument();
  });

  test("租户管理页聚合 overview、api-keys 和 usage overview 数据", async () => {
    const fetchMock = mockFetch({
      "/api/admin/overview": {
        stats: [
          { label: "24 小时请求量", value: "128 万" },
          { label: "成功率", value: "99.42%" },
          { label: "配额使用率", value: "74%" },
          { label: "活跃 API 密钥", value: "184" },
        ],
        route_health: [],
        top_models: [],
        recent_alerts: [],
        audit_snapshot: [],
      },
      "/api/admin/api-keys": {
        items: [
          {
            id: "key_1",
            name: "生产网关",
            tenant: "tenant_alpha",
            status: "启用",
            scopes: ["chat", "rag"],
            last_used_at: "2 分钟前",
          },
          {
            id: "key_2",
            name: "测试网关",
            tenant: "tenant_beta",
            status: "停用",
            scopes: ["chat"],
            last_used_at: "1 小时前",
          },
        ],
        credential_mode: "平台密钥与上游凭证分离管理。",
      },
      "/api/admin/usage/overview": {
        total_requests: 4200,
        success_rate: "99.20%",
        total_tokens: "320 万",
        average_latency: "184 ms",
        estimated_share: "37%",
      },
    });

    renderRoute("/tenants");

    expect(await screen.findByRole("heading", { level: 1, name: "租户管理" })).toBeInTheDocument();
    expect(screen.getByText("tenant_alpha")).toBeInTheDocument();
    expect(screen.getByText("生产网关")).toBeInTheDocument();
    expect(screen.getByText("320 万")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/overview");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/api-keys");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/overview");
  });

  test("member 总览页使用 /me/overview 数据", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = mockFetch({
      "/me/overview": {
        tenant_id: "tenant_demo",
        tenant_name: "Demo Tenant",
        active_api_keys: 3,
      },
    });

    renderRoute("/me");

    expect((await screen.findAllByText("Demo Tenant")).length).toBeGreaterThan(0);
    expect((await screen.findAllByText("tenant_demo")).length).toBeGreaterThan(0);
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/me/overview");
  });

  test("member API 密钥页走 /me 接口且不显示租户输入或删除操作", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/me/api-keys" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                id: "mk_1",
                name: "我的密钥",
                tenant: "tenant_demo",
                status: "启用",
                scopes: ["chat"],
                last_used_at: "刚刚",
                owner_user_id: "user_member_a",
              },
            ],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/me/api-keys" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          name: "member-key",
          scopes: ["chat"],
        });

        return new Response(
          JSON.stringify({
            item: {
              id: "mk_new",
              name: "member-key",
              tenant: "tenant_demo",
              status: "启用",
              scopes: ["chat"],
              last_used_at: "刚刚",
              owner_user_id: "user_member_a",
            },
            raw_key: "mk_live_secret",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/api-keys");

    expect(await screen.findByRole("button", { name: "选择 我的密钥" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "删除密钥" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "新建密钥" }));
    expect(screen.queryByLabelText("租户 ID")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "member-key" } });
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

    expect(await screen.findByText("新建密钥已完成")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/me/api-keys");
  });

  test("member API 密钥页在 rotate 和 deactivate 时命中 /me 分支并更新本地状态", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/me/api-keys" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                id: "mk_live",
                name: "member-live-key",
                tenant: "tenant_demo",
                status: "启用",
                scopes: ["chat", "rag"],
                last_used_at: "刚刚",
                owner_user_id: "user_member_a",
              },
            ],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/me/api-keys/mk_live/rotate" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          name: "member-rotated-key",
          scopes: ["chat", "rag"],
        });

        return new Response(
          JSON.stringify({
            item: {
              id: "mk_rotated",
              name: "member-rotated-key",
              tenant: "tenant_demo",
              status: "启用",
              scopes: ["chat", "rag"],
              last_used_at: "刚刚",
              owner_user_id: "user_member_a",
            },
            raw_key: "mk_rotated_secret",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/me/api-keys/mk_rotated/deactivate" && init?.method === "POST") {
        return new Response(
          JSON.stringify({
            item: {
              id: "mk_rotated",
              name: "member-rotated-key",
              tenant: "tenant_demo",
              status: "停用",
              scopes: ["chat", "rag"],
              last_used_at: "刚刚",
              owner_user_id: "user_member_a",
            },
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/api-keys");

    fireEvent.click(await screen.findByRole("button", { name: "选择 member-live-key" }));
    fireEvent.click(screen.getByRole("button", { name: "轮换密钥" }));
    fireEvent.change(screen.getByLabelText("新名称"), { target: { value: "member-rotated-key" } });
    fireEvent.click(screen.getByRole("button", { name: "确认轮换" }));

    expect(await screen.findByText("轮换操作已完成")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      "/me/api-keys/mk_live/rotate",
      expect.objectContaining({ method: "POST" }),
    );
    expect(screen.getByText("member-rotated-key")).toBeInTheDocument();
    expect(screen.getAllByText("停用").length).toBeGreaterThan(0);
    expect(screen.getByText("名称：member-rotated-key")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "停用密钥" }));
    fireEvent.click(screen.getByRole("button", { name: "确认停用" }));

    expect(await screen.findByText("停用操作已完成")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      "/me/api-keys/mk_rotated/deactivate",
      expect.objectContaining({ method: "POST" }),
    );
    expect(screen.getByText("状态：停用")).toBeInTheDocument();
  });

  test("member 调用观测页使用 /me usage overview 和 requests 数据", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = mockFetch({
      "/me/usage/overview": {
        total_requests: 120,
        success_rate: "98.40%",
        total_tokens: "12 万",
        average_latency: "228 ms",
        estimated_share: "14%",
      },
      "/me/usage/requests?limit=20&offset=0": {
        items: [
          {
            request_id: "req_1",
            tenant: "tenant_demo",
            endpoint: "/v1/chat/completions",
            model: "gpt-4o-mini",
            status: "成功",
            total_tokens: "1280",
            latency: "210 ms",
            usage_source: "member_key",
          },
        ],
        total: 1,
        limit: 20,
        offset: 0,
      },
    });

    renderRoute("/usage");

    expect(await screen.findByText("98.40%")).toBeInTheDocument();
    expect(screen.getByText("req_1")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/me/usage/overview");
    expect(fetchMock).toHaveBeenCalledWith("/me/usage/requests?limit=20&offset=0");
  });

  test("member 失败分析页使用 /me/failures 数据", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = mockFetch({
      "/me/failures": {
        breakdown: [
          { label: "上游超时", value: "8 次" },
          { label: "配额限制", value: "2 次" },
        ],
        recent_events: ["10:02 上游超时 /v1/chat/completions req_42"],
      },
    });

    renderRoute("/failures");

    expect(await screen.findByText("上游超时")).toBeInTheDocument();
    expect(screen.getByText("10:02 上游超时 /v1/chat/completions req_42")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/me/failures");
  });

  test("member 审计页使用 /me/audit-events 数据而不是 admin audit", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = mockFetch({
      "/me/audit-events": {
        items: [
          {
            time: "2026-04-28 10:03",
            event_type: "api_key.rotate",
            target_type: "api_key",
            target_id: "mk_1",
            detail: "轮换 member key",
          },
        ],
      },
    });

    renderRoute("/audit");

    expect(await screen.findByText("api_key.rotate")).toBeInTheDocument();
    expect(screen.getByText("轮换 member key")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/me/audit-events");
    expect(fetchMock).not.toHaveBeenCalledWith("/api/admin/audit");
  });

  test("路由页使用 /api/admin/routes 数据", async () => {
    const fetchMock = mockFetch({
      "/api/admin/routes": {
        stats: [
          { label: "启用供应商", value: "4" },
          { label: "模型映射", value: "19" },
          { label: "回退策略", value: "已启用" },
          { label: "启动模式", value: "启用中" },
        ],
        items: [
          {
            requested_model: "gpt-4o-mini",
            resolved_provider: "OpenAI 主线路由",
            credential: "provider_qwen_primary",
            latency: "218 ms",
            status: "健康",
          },
        ],
        policy_summary: ["模型优先解析已启用。", "请求会先匹配托管凭证，再按照回退策略分发。"],
      },
    });

    renderRoute("/routes");

    expect(await screen.findByRole("heading", { level: 1, name: "路由" })).toBeInTheDocument();
    expect(screen.getByText("模型优先解析已启用。")).toBeInTheDocument();
    expect(screen.getByText("provider_qwen_primary")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/routes");
  });

  test("审计页使用 /api/admin/audit 数据", async () => {
    const fetchMock = mockFetch({
      "/api/admin/audit": {
        metrics: [
          { label: "最近 24 小时请求", value: "64" },
          { label: "失败请求", value: "3" },
          { label: "限流次数", value: "1" },
          { label: "上游错误", value: "1" },
        ],
        events: [
          {
            time: "09:40",
            type: "request_failed",
            status: "失败",
            detail: "路由回退后恢复成功",
          },
        ],
        items: [
          {
            time: "09:42",
            tenant: "tenant_alpha",
            endpoint: "/v1/chat/completions",
            request_model: "qwen-flash",
            upstream_model: "qwen-plus",
            status: "200",
            provider: "OpenAI 主线路由",
            latency: "218 ms",
            usage_source: "上游返回",
          },
        ],
        summaries: [
          { title: "错误摘要", content: "配额超限和路由回退事件会在这里汇总。" },
          { title: "限流情况", content: "最近一小时内有 2 次租户限流。" },
        ],
      },
    });

    renderRoute("/audit");

    expect(await screen.findByRole("heading", { level: 1, name: "审计" })).toBeInTheDocument();
    expect(screen.getByText("最近事件流")).toBeInTheDocument();
    expect(screen.getByText("配额超限和路由回退事件会在这里汇总。")).toBeInTheDocument();
    expect(screen.getByText("/v1/chat/completions")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/audit");
  });

  test("审计页优先展示 usage 日志聚合结果", async () => {
    const fetchMock = mockFetch({
      "/api/admin/audit": {
        metrics: [
          { label: "最近 24 小时请求", value: "128" },
          { label: "失败请求", value: "4" },
          { label: "限流次数", value: "2" },
          { label: "上游错误", value: "1" },
        ],
        events: [
          {
            time: "04-24 19:08",
            type: "request_failed",
            status: "限流",
            detail: "请求失败（429）",
          },
        ],
        items: [
          {
            time: "04-24 19:08",
            tenant: "tenant_demo",
            endpoint: "/v1/chat/completions",
            request_model: "qwen-flash",
            upstream_model: "qwen-flash",
            status: "成功",
            provider: "DashScope 主路由",
            latency: "82 ms",
            usage_source: "上游返回",
          },
        ],
        summaries: [{ title: "真实摘要", content: "最近 24 小时共 128 次请求，其中 4 次失败。" }],
      },
    });

    renderRoute("/audit");

    expect(await screen.findByText("最近 24 小时请求")).toBeInTheDocument();
    expect(screen.getByText("真实摘要")).toBeInTheDocument();
    expect(screen.getAllByText("qwen-flash")).toHaveLength(2);
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/audit");
  });

  test("调试场页可以加载数据并提交最近一次请求结果", async () => {
    const fetchMock = mockFetch(
      {
        "/api/admin/playground": {
          available_models: ["qwen-plus", "text-embedding-v3"],
          last_run: {
            resolved_provider: "OpenAI 主线路由",
            endpoint: "/v1/chat/completions",
            latency: "218 ms",
            status: "200 OK",
            response: "旧结果",
            platform_key: "prod-gateway",
          },
        },
        "/api/admin/playground/chat": {
          resolved_provider: "OpenAI 备用线路",
          endpoint: "/v1/chat/completions",
          latency: "245 ms",
          status: "200 OK",
          response: "这是新的模型响应。",
          platform_key: "prod-gateway",
        },
      },
      {
        "/api/admin/playground/chat": (init) => {
          expect(init?.method).toBe("POST");
          expect(init?.headers).toMatchObject({ "Content-Type": "application/json" });
          expect(JSON.parse(String(init?.body))).toEqual({
            model: "qwen-plus",
            prompt: "请总结最近一次发布。",
          });
        },
      },
    );

    renderRoute("/playground");

    expect(await screen.findByRole("heading", { level: 1, name: "调试场" })).toBeInTheDocument();
    expect(await screen.findByDisplayValue("qwen-plus")).toBeInTheDocument();
    expect(screen.getByDisplayValue("请总结最近一次发布。")).toBeInTheDocument();
    expect(screen.getByText("旧结果")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "提交请求" }));

    await waitFor(() => {
      expect(screen.getByText("这是新的模型响应。")).toBeInTheDocument();
    });

    expect(fetchMock).toHaveBeenCalledWith("/api/admin/playground");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/admin/playground/chat",
      expect.objectContaining({ method: "POST" }),
    );
  });

  test("调用观测页使用 usage 接口数据", async () => {
    const fetchMock = mockFetch({
      "/api/admin/usage/overview": {
        total_requests: 128,
        success_rate: "98.40%",
        total_tokens: "24,560",
        average_latency: "182 ms",
        estimated_share: "12.00%",
      },
      "/api/admin/usage/trends": {
        requests: [
          { label: "04-24 18:00", value: "64" },
          { label: "04-24 19:00", value: "64" },
        ],
        tokens: [
          { label: "04-24 18:00", value: "12,100" },
          { label: "04-24 19:00", value: "12,460" },
        ],
        success: [
          { label: "04-24 18:00", value: "97.00%" },
          { label: "04-24 19:00", value: "99.80%" },
        ],
      },
      "/api/admin/usage/latency-wall?window=24h": {
        window_label: "最近 24 小时",
        buckets: ["04-24 18:00", "04-24 19:00"],
        lanes: [
          {
            model: "qwen-flash",
            provider: "DashScope 主路由",
            success_rate: "98.00%",
            average_latency: "182 ms",
            cells: [
              { bucket_label: "04-24 18:00", latency: "148 ms", status: "健康", requests: "12 次" },
              { bucket_label: "04-24 19:00", latency: "922 ms", status: "失败", requests: "2 次" },
            ],
          },
        ],
      },
      "/api/admin/usage/failures": {
        breakdown: [
          { label: "限流", value: "3 次" },
          { label: "上游服务异常", value: "1 次" },
        ],
        recent_events: ["04-24 19:08 · 限流 · 请求失败（429）"],
      },
      "/api/admin/usage/requests": {
        items: [
          {
            request_id: "llmreq_demo_002",
            tenant: "tenant_demo",
            endpoint: "/v1/embeddings",
            model: "text-embedding-3-small",
            status: "限流",
            total_tokens: "16",
            latency: "95 ms",
            usage_source: "估算",
          },
        ],
        total: 1,
        limit: 20,
        offset: 0,
      },
      "/api/admin/usage/requests?limit=20&offset=0": {
        items: [
          {
            request_id: "llmreq_demo_002",
            tenant: "tenant_demo",
            endpoint: "/v1/embeddings",
            model: "text-embedding-3-small",
            status: "限流",
            total_tokens: "16",
            latency: "95 ms",
            usage_source: "估算",
          },
        ],
        total: 1,
        limit: 20,
        offset: 0,
      },
    });

    renderRoute("/usage");

    expect(await screen.findByRole("heading", { level: 1, name: "调用观测" })).toBeInTheDocument();
    expect(screen.getByText("查看 Token、成功率、失败分类与调用明细。")).toBeInTheDocument();
    expect(screen.getByText("模型延时健康墙")).toBeInTheDocument();
    expect(screen.getByText("总调用数")).toBeInTheDocument();
    expect(screen.getByText("趋势概览")).toBeInTheDocument();
    expect(screen.getByText("04-24 19:08 · 限流 · 请求失败（429）")).toBeInTheDocument();
    expect(screen.getByText("llmreq_demo_002")).toBeInTheDocument();
    expect(screen.getByText("DashScope 主路由")).toBeInTheDocument();

    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/overview");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/trends");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/latency-wall?window=24h");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/failures");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/requests?limit=20&offset=0");
  });

  test("调用观测页使用可视化组件展示状态、事件流与来源 pill", async () => {
    mockFetch({
      "/api/admin/usage/overview": {
        total_requests: 128,
        success_rate: "98.40%",
        total_tokens: "24,560",
        average_latency: "182 ms",
        estimated_share: "12.00%",
      },
      "/api/admin/usage/trends": {
        requests: [
          { label: "04-24 18:00", value: "64" },
          { label: "04-24 19:00", value: "64" },
        ],
        tokens: [
          { label: "04-24 18:00", value: "12,100" },
          { label: "04-24 19:00", value: "12,460" },
        ],
        success: [
          { label: "04-24 18:00", value: "97.00%" },
          { label: "04-24 19:00", value: "99.80%" },
        ],
      },
      "/api/admin/usage/latency-wall?window=24h": {
        window_label: "最近 24 小时",
        buckets: ["04-24 18:00", "04-24 19:00"],
        lanes: [
          {
            model: "qwen-flash",
            provider: "DashScope 主路由",
            success_rate: "98.00%",
            average_latency: "182 ms",
            cells: [
              { bucket_label: "04-24 18:00", latency: "148 ms", status: "健康", requests: "12 次" },
              { bucket_label: "04-24 19:00", latency: "922 ms", status: "失败", requests: "2 次" },
            ],
          },
        ],
      },
      "/api/admin/usage/failures": {
        breakdown: [
          { label: "限流", value: "3 次" },
          { label: "上游服务异常", value: "1 次" },
        ],
        recent_events: ["04-24 19:08 · 限流 · 请求失败（429）"],
      },
      "/api/admin/usage/requests?limit=20&offset=0": {
        items: [
          {
            request_id: "llmreq_demo_002",
            tenant: "tenant_demo",
            endpoint: "/v1/embeddings",
            model: "text-embedding-3-small",
            status: "限流",
            total_tokens: "16",
            latency: "95 ms",
            usage_source: "估算",
          },
        ],
        total: 1,
        limit: 20,
        offset: 0,
      },
    });

    renderRoute("/usage");

    expect(await screen.findByText("实时运行视图")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "最近 24 小时" })).toBeInTheDocument();
    expect(screen.getByText("异常事件流")).toBeInTheDocument();
    expect(screen.getByLabelText("状态 限流")).toBeInTheDocument();
    expect(screen.getByLabelText("来源 估算")).toBeInTheDocument();
    expect(screen.getAllByText("健康").length).toBeGreaterThan(0);
    expect(screen.getAllByText("失败").length).toBeGreaterThan(0);
  });

  test("调用观测页在部分请求失败时优先显示错误", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/usage/overview") {
        return Promise.resolve(new Response("boom", { status: 500 }));
      }
      if (url === "/api/admin/usage/trends") {
        return new Promise<Response>(() => {});
      }
      if (url === "/api/admin/usage/latency-wall?window=24h") {
        return Promise.resolve(
          new Response(JSON.stringify({ window_label: "最近 24 小时", buckets: [], lanes: [] }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (url === "/api/admin/usage/failures") {
        return Promise.resolve(
          new Response(JSON.stringify({ breakdown: [], recent_events: [] }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (url === "/api/admin/usage/requests?limit=20&offset=0") {
        return Promise.resolve(
          new Response(JSON.stringify({ items: [], total: 0, limit: 20, offset: 0 }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/usage");

    expect(await screen.findByText("请求失败（500）：boom")).toBeInTheDocument();
  });

  test("调用观测页支持明细翻页", async () => {
    const fetchMock = mockFetch({
      "/api/admin/usage/overview": {
        total_requests: 41,
        success_rate: "98.40%",
        total_tokens: "24,560",
        average_latency: "182 ms",
        estimated_share: "12.00%",
      },
      "/api/admin/usage/trends": {
        requests: [],
        tokens: [],
        success: [],
      },
      "/api/admin/usage/latency-wall?window=24h": {
        window_label: "最近 24 小时",
        buckets: [],
        lanes: [],
      },
      "/api/admin/usage/failures": {
        breakdown: [],
        recent_events: [],
      },
      "/api/admin/usage/requests?limit=20&offset=0": {
        items: [
          {
            request_id: "llmreq_page_1",
            tenant: "tenant_demo",
            endpoint: "/v1/chat/completions",
            model: "qwen-flash",
            status: "成功",
            total_tokens: "32",
            latency: "80 ms",
            usage_source: "上游返回",
          },
        ],
        total: 41,
        limit: 20,
        offset: 0,
      },
      "/api/admin/usage/requests?limit=20&offset=20": {
        items: [
          {
            request_id: "llmreq_page_2",
            tenant: "tenant_demo",
            endpoint: "/v1/chat/completions",
            model: "qwen-flash",
            status: "成功",
            total_tokens: "28",
            latency: "76 ms",
            usage_source: "上游返回",
          },
        ],
        total: 41,
        limit: 20,
        offset: 20,
      },
    });

    renderRoute("/usage");

    expect(await screen.findByText("llmreq_page_1")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));

    await waitFor(() => {
      expect(screen.getByText("llmreq_page_2")).toBeInTheDocument();
    });

    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/requests?limit=20&offset=20");
  });

  test("调用观测页在分页边界正确禁用按钮并支持返回上一页", async () => {
    const fetchMock = mockFetch({
      "/api/admin/usage/overview": {
        total_requests: 41,
        success_rate: "98.40%",
        total_tokens: "24,560",
        average_latency: "182 ms",
        estimated_share: "12.00%",
      },
      "/api/admin/usage/trends": {
        requests: [],
        tokens: [],
        success: [],
      },
      "/api/admin/usage/latency-wall?window=24h": {
        window_label: "最近 24 小时",
        buckets: [],
        lanes: [],
      },
      "/api/admin/usage/failures": {
        breakdown: [],
        recent_events: [],
      },
      "/api/admin/usage/requests?limit=20&offset=0": {
        items: [
          {
            request_id: "llmreq_page_1",
            tenant: "tenant_demo",
            endpoint: "/v1/chat/completions",
            model: "qwen-flash",
            status: "成功",
            total_tokens: "32",
            latency: "80 ms",
            usage_source: "上游返回",
          },
        ],
        total: 41,
        limit: 20,
        offset: 0,
      },
      "/api/admin/usage/requests?limit=20&offset=20": {
        items: [
          {
            request_id: "llmreq_page_2",
            tenant: "tenant_demo",
            endpoint: "/v1/chat/completions",
            model: "qwen-flash",
            status: "成功",
            total_tokens: "28",
            latency: "76 ms",
            usage_source: "上游返回",
          },
        ],
        total: 41,
        limit: 20,
        offset: 20,
      },
    });

    renderRoute("/usage");

    expect(await screen.findByText("llmreq_page_1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "下一页" })).toBeEnabled();

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));

    await waitFor(() => {
      expect(screen.getByText("llmreq_page_2")).toBeInTheDocument();
    });

    expect(screen.getByRole("button", { name: "上一页" })).toBeEnabled();

    fireEvent.click(screen.getByRole("button", { name: "上一页" }));

    await waitFor(() => {
      expect(screen.getByText("llmreq_page_1")).toBeInTheDocument();
    });

    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/requests?limit=20&offset=20");
    expect(fetchMock).toHaveBeenCalledTimes(8);
  });
});
