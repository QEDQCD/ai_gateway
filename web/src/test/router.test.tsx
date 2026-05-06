import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { AppLayout } from "../app/layout";
import { createTestRouter } from "../app/router";
import type { ConsoleSession } from "../lib/session";

const defaultConsoleSession: ConsoleSession = {
  role: "admin",
  user_id: "user_admin_demo",
  email: "admin@example.com",
  name: "平台管理员",
  token: "",
  expires_at: "2026-05-01T00:00:00Z",
};

const useConsoleSessionMock = vi.fn<() => ConsoleSession | null>(() => defaultConsoleSession);

vi.mock("../lib/session", () => ({
  getDefaultSession: () => defaultConsoleSession,
  getConsoleSession: () => useConsoleSessionMock(),
  useConsoleSession: () => useConsoleSessionMock(),
}));

type MockResponseMap = Record<string, unknown>;
type MockRequestAssertions = Partial<Record<string, (init?: RequestInit) => void>>;
const hiddenKnowledgeTerm = ["知", "识库"].join("");
const providerAlias = ["provider", "qwen", "primary"].join("_");

function defaultSystemStatus() {
  return {
    console_stage: "控制台预览版",
    run_mode: "数据库模式",
    gateway_health: "健康",
    quota_protection: "已启用",
    console_entry: "31873",
    gateway_admin_api: "32658",
    internal_services: ["internal-search"],
    hidden_modules: ["内部检索能力", "高级路由设置"],
  };
}

function createPricingModelsMock() {
  return [
    {
      model: "gpt-4o-mini",
      input_price: "2.00 ￥/M",
      output_price: "20.00 ￥/M",
      cached_price: "0.50 ￥/M",
    },
  ];
}

function createUsageOverviewMock(overrides: Record<string, unknown> = {}) {
  return {
    total_requests: 128,
    success_rate: "98.40%",
    total_tokens: "24,560",
    input_tokens: "12,100",
    output_tokens: "12,000",
    cached_tokens: "460",
    average_latency: "182 ms",
    estimated_share: "12.00%",
    input_cost: "0.12 ￥",
    output_cost: "0.36 ￥",
    cached_cost: "0.04 ￥",
    total_cost: "0.52 ￥",
    pricing_models: createPricingModelsMock(),
    ...overrides,
  };
}

function createUsageRequestMock(overrides: Record<string, unknown> = {}) {
  return {
    request_id: "llmreq_demo_001",
    tenant: "tenant_demo",
    endpoint: "/v1/chat/completions",
    model: "gpt-4o-mini",
    resolved_model: "gpt-4o-mini",
    task_class: "",
    routing_reason: "",
    target_model_tier: "",
    status: "成功",
    total_tokens: "1,280",
    input_tokens: "840",
    output_tokens: "400",
    cached_tokens: "40",
    latency: "210 ms",
    usage_source: "上游返回",
    input_cost: "0.08 ￥",
    output_cost: "0.40 ￥",
    cached_cost: "0.01 ￥",
    total_cost: "0.49 ￥",
    input_price: "2.00 ￥/M",
    output_price: "20.00 ￥/M",
    cached_price: "0.50 ￥/M",
    ...overrides,
  };
}

function createAdminOverviewMock(overrides: Record<string, unknown> = {}) {
  return {
    stats: [],
    route_health: [],
    top_models: [],
    recent_alerts: [],
    audit_snapshot: [],
    platform_metrics: [],
    tenant_posture: [],
    ...overrides,
  };
}

function mockSession(session: Partial<ConsoleSession> = {}) {
  useConsoleSessionMock.mockReturnValue({
    ...defaultConsoleSession,
    ...session,
  });
}

function mockAnonymousSession() {
  useConsoleSessionMock.mockReturnValue(null);
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
    const responseKey =
      url === "/api/admin/usage/overview?window=24h" && "/api/admin/usage/overview" in responses
        ? "/api/admin/usage/overview"
        : url === "/api/admin/usage/trends?window=24h" && "/api/admin/usage/trends" in responses
          ? "/api/admin/usage/trends"
          : url === "/api/admin/usage/failures?window=24h" && "/api/admin/usage/failures" in responses
            ? "/api/admin/usage/failures"
            : url;

    if (!(responseKey in responses)) {
      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    }

    return new Response(JSON.stringify(responses[responseKey]), {
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
  test("未登录时渲染应用内登录页", async () => {
    mockAnonymousSession();

    render(
      <RouterProvider router={createTestRouter(["/"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByRole("heading", { level: 1, name: "登录 AI Gateway 控制台" })).toBeInTheDocument();
    expect(screen.getByLabelText("账号")).toBeInTheDocument();
    expect(screen.getByLabelText("密码")).toBeInTheDocument();
  });

  test("未登录时登录页写入页面标题与公开摘要", async () => {
    mockAnonymousSession();

    render(
      <RouterProvider router={createTestRouter(["/login"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByRole("heading", { level: 1, name: "登录 AI Gateway 控制台" })).toBeInTheDocument();
    expect(document.title).toContain("AI Gateway");
    expect(
      screen.getByText(/统一管理平台 API Key、调用日志、失败记录与租户级 Token 消耗/),
    ).toBeInTheDocument();
    expect(document.head.querySelector('meta[name="description"]')?.getAttribute("content")).toContain(
      "统一管理平台 API Key",
    );
  });

  test("未登录时提供账号申请入口并可跳转到申请页", async () => {
    mockAnonymousSession();

    render(
      <RouterProvider router={createTestRouter(["/login"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByRole("link", { name: "申请账号" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "申请账号" }));

    expect(await screen.findByRole("heading", { level: 2, name: "申请接入" })).toBeInTheDocument();
  });

  test("申请页提交表单后展示已提交状态", async () => {
    mockAnonymousSession();
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/console/captcha" && !init?.method) {
        return new Response(
          JSON.stringify({
            captcha_id: "cap_demo",
            image_data: "data:image/png;base64,AAAA",
            expires_at: "2026-04-29T00:00:00Z",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      if (url === "/api/console/captcha/verify" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          captcha_id: "cap_demo",
          captcha_code: "A7KQ",
        });

        return new Response(
          JSON.stringify({
            captcha_pass_token: "cp_demo",
            expires_at: "2026-04-29T00:00:00Z",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      if (url === "/api/console/applications" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          email: "new-user@example.com",
          name: "新用户",
          company_name: "New Co",
          use_case: "测试接入",
          password: "Example1234",
          captcha_pass_token: "cp_demo",
        });

        return new Response(
          JSON.stringify({
            item: {
              id: "app_new_pending",
              email: "new-user@example.com",
              name: "新用户",
              company_name: "New Co",
              use_case: "测试接入",
              status: "pending",
              created_at: "2026-04-28T16:00:00+08:00",
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

    render(
      <RouterProvider router={createTestRouter(["/apply"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByAltText("图形验证码")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "提交申请" })).toBeEnabled();

    fireEvent.change(await screen.findByLabelText("邮箱"), {
      target: { value: "new-user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("姓名"), {
      target: { value: "新用户" },
    });
    fireEvent.change(screen.getByLabelText("公司"), {
      target: { value: "New Co" },
    });
    fireEvent.change(screen.getByLabelText("接入用途"), {
      target: { value: "测试接入" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "Example1234" },
    });
    fireEvent.change(screen.getByLabelText("确认密码"), {
      target: { value: "Example1234" },
    });
    fireEvent.change(screen.getByLabelText("验证码"), {
      target: { value: "A7KQ" },
    });
    fireEvent.click(screen.getByRole("button", { name: "验证验证码" }));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "提交申请" })).toBeEnabled();
    });
    fireEvent.click(screen.getByRole("button", { name: "提交申请" }));

    expect(await screen.findByText("申请已提交")).toBeInTheDocument();
    expect(screen.getByText("状态：pending")).toBeInTheDocument();
  });

  test("验证码通过后提交按钮仍可点击，并给出明确校验错误", async () => {
    mockAnonymousSession();
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/console/captcha" && !init?.method) {
        return new Response(
          JSON.stringify({
            captcha_id: "cap_demo",
            image_data: "data:image/png;base64,AAAA",
            expires_at: "2026-04-29T00:00:00Z",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      if (url === "/api/console/captcha/verify" && init?.method === "POST") {
        return new Response(
          JSON.stringify({
            captcha_pass_token: "cp_demo",
            expires_at: "2026-04-29T00:00:00Z",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      if (url === "/api/console/applications" && init?.method === "POST") {
        throw new Error("form validation should block request");
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    render(
      <RouterProvider router={createTestRouter(["/apply"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByAltText("图形验证码")).toBeInTheDocument();

    fireEvent.change(await screen.findByLabelText("邮箱"), {
      target: { value: "new-user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("姓名"), {
      target: { value: "新用户" },
    });
    fireEvent.change(screen.getByLabelText("公司"), {
      target: { value: "New Co" },
    });
    fireEvent.change(screen.getByLabelText("接入用途"), {
      target: { value: "测试接入" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "Example1234" },
    });
    fireEvent.change(screen.getByLabelText("确认密码"), {
      target: { value: "Mismatch1234" },
    });
    fireEvent.change(screen.getByLabelText("验证码"), {
      target: { value: "A7KQ" },
    });
    fireEvent.click(screen.getByRole("button", { name: "验证验证码" }));

    expect(await screen.findByText("验证码已通过")).toBeInTheDocument();

    const submitButton = screen.getByRole("button", { name: "提交申请" });
    expect(submitButton).toBeEnabled();

    fireEvent.click(submitButton);

    expect(await screen.findByText("两次输入的密码不一致。")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  test("申请页会阻止不包含 @ 的邮箱并提示中文错误", async () => {
    mockAnonymousSession();
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/console/captcha" && !init?.method) {
        return new Response(
          JSON.stringify({
            captcha_id: "cap_demo",
            image_data: "data:image/png;base64,AAAA",
            expires_at: "2026-04-29T00:00:00Z",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      if (url === "/api/console/captcha/verify" && init?.method === "POST") {
        return new Response(
          JSON.stringify({
            captcha_pass_token: "cp_demo",
            expires_at: "2026-04-29T00:00:00Z",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      if (url === "/api/console/applications" && init?.method === "POST") {
        throw new Error("email validation should block request");
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    render(
      <RouterProvider router={createTestRouter(["/apply"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByAltText("图形验证码")).toBeInTheDocument();

    fireEvent.change(await screen.findByLabelText("邮箱"), {
      target: { value: "invalid-email" },
    });
    fireEvent.change(screen.getByLabelText("姓名"), {
      target: { value: "新用户" },
    });
    fireEvent.change(screen.getByLabelText("公司"), {
      target: { value: "New Co" },
    });
    fireEvent.change(screen.getByLabelText("接入用途"), {
      target: { value: "测试接入" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "Example1234" },
    });
    fireEvent.change(screen.getByLabelText("确认密码"), {
      target: { value: "Example1234" },
    });
    fireEvent.change(screen.getByLabelText("验证码"), {
      target: { value: "A7KQ" },
    });
    fireEvent.click(screen.getByRole("button", { name: "验证验证码" }));
    expect(await screen.findByText("验证码已通过")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "提交申请" }));

    expect(await screen.findByText("邮箱格式不合法，请输入包含 @ 的邮箱地址。")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

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
    expect(screen.queryByRole("link", { name: "路由" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "调试场" })).not.toBeInTheDocument();
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

      if (url === "/api/me/overview") {
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
      expect(fetchMock.mock.calls.map(([input]) => String(input))).toEqual(["/api/me/overview"]);
    });
  });

  test("member session 访问根路径时会跳转到 /me", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    mockFetch({
      "/api/me/overview": {
        tenant_id: "tenant_demo",
        tenant_name: "Demo Tenant",
        active_api_keys: 1,
        quota: {
          configured: true,
          request_limit: 500000,
          requests_used: 120000,
          requests_remaining: 380000,
          token_limit: 10000000,
          tokens_used: 2400000,
          tokens_remaining: 7600000,
          resets_at: "2026-05-01T00:00:00+08:00",
        },
      },
    });

    render(
      <RouterProvider router={createTestRouter(["/"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByRole("heading", { level: 1, name: "我的总览" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "我的总览" })).toHaveAttribute("aria-current", "page");
  });

  test("member 总览页展示真实租户额度卡片", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    mockFetch({
      "/api/me/overview": {
        tenant_id: "tenant_demo",
        tenant_name: "Demo Tenant",
        active_api_keys: 2,
        quota: {
          configured: true,
          request_limit: 500000,
          requests_used: 120000,
          requests_remaining: 380000,
          token_limit: 10000000,
          tokens_used: 2400000,
          tokens_remaining: 7600000,
          resets_at: "2026-05-01T00:00:00+08:00",
        },
      },
    });

    render(
      <RouterProvider router={createTestRouter(["/me"])} future={{ v7_startTransition: true }} />,
    );

    expect(await screen.findByText("本月请求额度")).toBeInTheDocument();
    expect(screen.getByText("120,000 / 500,000")).toBeInTheDocument();
    expect(screen.getByText("2,400,000 / 10,000,000")).toBeInTheDocument();
  });

  test("member 总览页支持提交账户注销申请", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/me/overview") {
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

      if (url === "/api/me/account-deletion-applications" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({ reason: "项目结束，不再使用" });
        return new Response(
          JSON.stringify({
            item: {
              id: "ada_member_pending",
              user_id: "user_member_a",
              tenant_id: "tenant_demo",
              user_email: "member-a@example.com",
              user_name: "Member A",
              reason: "项目结束，不再使用",
              status: "pending",
              disabled_api_keys: 0,
              created_at: "2026-05-06T09:02:03+08:00",
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

    renderRoute("/me");

    fireEvent.change(await screen.findByLabelText("注销原因"), {
      target: { value: "项目结束，不再使用" },
    });
    fireEvent.click(screen.getByRole("button", { name: "提交注销申请" }));

    expect(await screen.findByText("注销申请已提交：pending")).toBeInTheDocument();
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
      "/api/admin/overview": createAdminOverviewMock({
        stats: [
          { label: "24 小时请求量", value: "128 万" },
          { label: "成功率", value: "99.42%" },
          { label: "配额使用率", value: "74%" },
          { label: "活跃 API 密钥", value: "184" },
        ],
        route_health: [{ columns: ["gpt-4o-mini", "default-route", "218 ms", "健康"] }],
        top_models: [{ columns: ["gpt-4o-mini", "612k", "48%", "对话"] }],
        recent_alerts: [{ columns: ["09:42", "配额告警", "tenant_beta"] }],
        audit_snapshot: [{ columns: ["tenant_alpha", "/v1/chat/completions", "200"] }],
        platform_metrics: [{ label: "活跃租户数", value: "3" }],
        tenant_posture: [
          { columns: ["tenant_alpha", "健康", "3", "2", "3456789", "120000 / 3456789"] },
        ],
      }),
    });

    renderRoute();

    expect(await screen.findByRole("heading", { level: 1, name: "总览" })).toBeInTheDocument();
    expect(screen.getByText("24 小时请求量")).toBeInTheDocument();
    expect(screen.getByText("平台默认线路")).toBeInTheDocument();
    expect(screen.getByText("活跃租户数")).toBeInTheDocument();
    expect(screen.getAllByText("tenant_alpha").length).toBeGreaterThan(0);
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/overview");
  });

  test("控制台导航不再展示已收口模块入口", async () => {
    mockFetch({
      "/api/admin/overview": createAdminOverviewMock(),
      "/api/admin/system/status": defaultSystemStatus(),
    });

    renderRoute("/");

    expect(await screen.findByRole("heading", { level: 1, name: "总览" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "内部文档" })).not.toBeInTheDocument();
  });

  test("侧栏隐藏左下状态块但顶部 badge 继续展示系统状态", async () => {
    const fetchMock = mockFetch({
      "/api/admin/overview": createAdminOverviewMock(),
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
    expect(await screen.findByRole("button", { name: "选择 生产网关" })).toBeInTheDocument();
    expect(screen.getByText("平台密钥与上游凭证分离管理。")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/api-keys");
  });

  test("API 密钥页展示稳定详情区并支持历史密钥回显复制", async () => {
    mockFetch({
      "/api/admin/api-keys": {
        items: [
          {
            id: "pak_live_console",
            name: "生产网关",
            tenant: "tenant_alpha",
            status: "启用",
            scopes: ["chat"],
            last_used_at: "2026-04-28T10:00:00+08:00",
            created_by_user_id: "user_admin_demo",
            expires_at: "2026-05-28T10:00:00+08:00",
          },
        ],
      },
      "/api/admin/api-keys/pak_live_console/secret": {
        api_key_id: "pak_live_console",
        masked_key: "agw_••••••••demo",
        revealable: true,
        legacy_unrecoverable: false,
        expires_at: "2026-05-28T10:00:00+08:00",
      },
      "/api/admin/api-keys/pak_live_console/secret/copy": {
        api_key_id: "pak_live_console",
        masked_key: "agw_••••••••demo",
        full_key: "agw-live-secret",
        revealable: true,
        legacy_unrecoverable: false,
        expires_at: "2026-05-28T10:00:00+08:00",
      },
    });

    const clipboardWriteText = vi.fn(async () => undefined);
    vi.stubGlobal("navigator", {
      clipboard: {
        writeText: clipboardWriteText,
      },
    });

    renderRoute("/api-keys");

    const detailSection = await screen.findByLabelText("已选密钥详情");
    expect(within(detailSection).queryByRole("button", { name: "加载密钥摘要" })).not.toBeInTheDocument();
    expect(within(detailSection).getByText("agw_••••••••demo")).toBeInTheDocument();
    expect(within(detailSection).queryByText("agw-live-secret")).not.toBeInTheDocument();

    fireEvent.click(within(detailSection).getByRole("button", { name: "复制完整密钥" }));
    await waitFor(() => {
      expect(clipboardWriteText).toHaveBeenCalledWith("agw-live-secret");
    });
    expect(await screen.findByText("完整密钥已复制到剪贴板。")).toBeInTheDocument();
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
    fireEvent.click(screen.getByRole("button", { name: "复制本次完整密钥" }));
    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("ak_live_new_secret");
      expect(screen.getByText("完整密钥已复制到剪贴板。")).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "选择 new-key" })).toBeInTheDocument();
  });

  test("API 密钥页在无 navigator.clipboard 时也能回退复制完整密钥", async () => {
    const execCommand = vi.fn(function (this: unknown, command: string) {
      return this === document && command === "copy";
    });
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
    fireEvent.click(screen.getByRole("button", { name: "复制本次完整密钥" }));

    await waitFor(() => {
      expect(execCommand).toHaveBeenCalledWith("copy");
      expect(screen.getByText("完整密钥已复制到剪贴板。")).toBeInTheDocument();
    });
  });

  test("API 密钥页在 clipboard.writeText 被拒绝时也会继续回退复制", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("NotAllowedError"));
    const execCommand = vi.fn(function (this: unknown, command: string) {
      return this === document && command === "copy";
    });
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
    fireEvent.click(screen.getByRole("button", { name: "复制本次完整密钥" }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("ak_live_clipboard_reject_secret");
      expect(execCommand).toHaveBeenCalledWith("copy");
      expect(screen.getByText("完整密钥已复制到剪贴板。")).toBeInTheDocument();
    });
  });

  test("API 密钥页在自动复制全部失败时会回退到人工复制完整密钥", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("NotAllowedError"));
    const execCommand = vi.fn(function (this: unknown, command: string) {
      return this === document && command === "copy" ? false : false;
    });
    const promptMock = vi.fn().mockReturnValue("");
    Object.defineProperty(document, "execCommand", {
      value: execCommand,
      configurable: true,
    });
    vi.stubGlobal("navigator", {
      clipboard: { writeText },
    });
    vi.stubGlobal("prompt", promptMock);

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
              name: "manual-copy-key",
              tenant: "tenant_alpha",
              status: "启用",
              scopes: ["chat"],
              last_used_at: "2026-04-24T12:00:00+08:00",
            },
            raw_key: "ak_live_manual_copy_secret",
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
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "manual-copy-key" } });
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

    expect(await screen.findByText("新建密钥已完成")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "复制本次完整密钥" }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("ak_live_manual_copy_secret");
      expect(execCommand).toHaveBeenCalledWith("copy");
      expect(promptMock).toHaveBeenCalledWith(
        "浏览器自动复制不可用，请手动复制完整密钥：",
        "ak_live_manual_copy_secret",
      );
      expect(
        screen.getByText("浏览器自动复制不可用，请在弹窗中手动复制完整密钥。"),
      ).toBeInTheDocument();
    });
    expect(document.body).not.toHaveTextContent("ak_live_manual_copy_secret");
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
    fireEvent.click(screen.getByRole("button", { name: "复制本次完整密钥" }));
    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("ak_live_rotated_secret");
      expect(screen.getByText("完整密钥已复制到剪贴板。")).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "选择 生产网关-轮换" })).toBeInTheDocument();
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

    expect(await screen.findByText("名称：scope-test")).toBeInTheDocument();

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
      if (url === "/api/admin/routes" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                requested_model: "qwen-flash",
                route_label: "Qwen",
                credential: "qwen",
                latency: "10 ms",
                status: "healthy",
                provider_group: "qwen",
              },
              {
                requested_model: "mimo-v2.5-pro",
                route_label: "MIMO",
                credential: "mimo",
                latency: "20 ms",
                status: "healthy",
                provider_group: "mimo",
              },
            ],
            stats: [],
            policy_summary: [],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
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
          token_limit: 10000000,
          allowed_models: ["qwen-flash", "mimo-v2.5-pro"],
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
    fireEvent.change(screen.getByLabelText("Token 上限"), { target: { value: "10000000" } });
    fireEvent.click(screen.getByRole("button", { name: "审批通过" }));

    expect(await screen.findByText("审批已完成")).toBeInTheDocument();
    expect(screen.getByText("approved")).toBeInTheDocument();
  });

  test("账号申请页支持审批账户注销申请并提交清理动作", async () => {
    mockSession({ role: "admin", user_id: "user_admin_demo" });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/routes" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [],
            stats: [],
            policy_summary: [],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/applications" && !init?.method) {
        return new Response(JSON.stringify({ items: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/account-deletion-applications" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                id: "ada_pending",
                user_id: "user_member_a",
                tenant_id: "tenant_demo",
                user_email: "member-a@example.com",
                user_name: "Member A",
                reason: "不再使用",
                status: "pending",
                disabled_api_keys: 0,
                created_at: "2026-05-06T09:02:03+08:00",
              },
            ],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (
        url === "/api/admin/account-deletion-applications/ada_pending/approve" &&
        init?.method === "POST"
      ) {
        expect(JSON.parse(String(init.body))).toEqual({
          actor_id: "user_admin_demo",
          comment: "同意注销申请",
        });
        return new Response(
          JSON.stringify({
            item: {
              id: "ada_pending",
              user_id: "user_member_a",
              tenant_id: "tenant_demo",
              user_email: "member-a@example.com",
              user_name: "Member A",
              reason: "不再使用",
              status: "approved",
              disabled_api_keys: 1,
              created_at: "2026-05-06T09:02:03+08:00",
              reviewed_at: "2026-05-06T09:05:03+08:00",
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

    fireEvent.click(await screen.findByRole("button", { name: "选择 Member A" }));
    expect(await screen.findByRole("dialog", { name: "注销审批" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "审批注销" }));

    expect(await screen.findByText("注销审批已通过")).toBeInTheDocument();
    expect(screen.getByText("已清理 API Key：1 个")).toBeInTheDocument();
  });

  test("账号申请页支持拒绝审批", async () => {
    mockSession({ role: "admin", user_id: "user_admin_demo" });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/system/status") {
        return new Response(JSON.stringify(defaultSystemStatus()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/admin/routes" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                requested_model: "qwen-flash",
                route_label: "Qwen",
                credential: "qwen",
                latency: "10 ms",
                status: "healthy",
                provider_group: "qwen",
              },
              {
                requested_model: "mimo-v2.5-pro",
                route_label: "MIMO",
                credential: "mimo",
                latency: "20 ms",
                status: "healthy",
                provider_group: "mimo",
              },
            ],
            stats: [],
            policy_summary: [],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/api/admin/applications" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                id: "app_reject",
                email: "reject@example.com",
                name: "待拒绝用户",
                company_name: "Reject Co",
                use_case: "测试接入",
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
      if (url === "/api/admin/applications/app_reject/reject" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          actor_id: "user_admin_demo",
          comment: "通过控制台审批",
        });

        return new Response(
          JSON.stringify({
            item: {
              id: "app_reject",
              email: "reject@example.com",
              name: "待拒绝用户",
              company_name: "Reject Co",
              use_case: "测试接入",
              status: "rejected",
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

    fireEvent.click(await screen.findByRole("button", { name: "选择 待拒绝用户" }));
    fireEvent.click(screen.getByRole("button", { name: "拒绝审批" }));

    expect(await screen.findByText("审批已拒绝")).toBeInTheDocument();
    expect(screen.getByText("rejected")).toBeInTheDocument();
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
      if (url === "/api/admin/routes" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [
              {
                requested_model: "qwen-flash",
                route_label: "Qwen",
                credential: "qwen",
                latency: "10 ms",
                status: "healthy",
                provider_group: "qwen",
              },
              {
                requested_model: "mimo-v2.5-pro",
                route_label: "MIMO",
                credential: "mimo",
                latency: "20 ms",
                status: "healthy",
                provider_group: "mimo",
              },
            ],
            stats: [],
            policy_summary: [],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
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
          token_limit: 10000000,
          allowed_models: ["qwen-flash", "mimo-v2.5-pro"],
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

    fireEvent.click(await screen.findByRole("button", { name: "选择 Alice" }));
    expect(await screen.findByRole("dialog", { name: "审批操作" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("租户 ID"), { target: { value: "tenant_alice" } });
    fireEvent.change(screen.getByLabelText("Token 上限"), { target: { value: "10000000" } });
    fireEvent.click(screen.getByRole("button", { name: "审批通过" }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/admin/applications/app_alice/approve",
        expect.objectContaining({ method: "POST" }),
      );
    });

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "选择 Bob" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "重置" })).toBeDisabled();
    });

    resolveApproval?.();

    expect(await screen.findByText("审批已完成")).toBeInTheDocument();
    expect(screen.getByText("申请人：Alice")).toBeInTheDocument();
    expect(screen.getByText("租户：tenant_alice")).toBeInTheDocument();
  });

  test("账号申请页重置按钮恢复默认审批表单并清空上一条审批结果", async () => {
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
                id: "app_alice_reset",
                email: "alice@example.com",
                name: "Alice",
                company_name: "Alice Co",
                use_case: "租户接入",
                status: "pending",
                created_at: "2026-04-25T09:02:03+08:00",
              },
              {
                id: "app_bob_reset",
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
      if (url === "/api/admin/applications/app_alice_reset/approve" && init?.method === "POST") {
        expect(JSON.parse(String(init.body))).toEqual({
          actor_id: "user_admin_demo",
          comment: "通过控制台审批",
          tenant_id: "tenant_alice",
          token_limit: 10000000,
          allowed_models: ["qwen-flash", "mimo-v2.5-pro"],
        });

        return new Response(
          JSON.stringify({
            item: {
              id: "app_alice_reset",
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
        );
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/applications");

    fireEvent.click(await screen.findByRole("button", { name: "选择 Alice" }));
    expect(await screen.findByRole("dialog", { name: "审批操作" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("租户 ID"), { target: { value: "tenant_alice" } });
    fireEvent.click(screen.getByRole("button", { name: "审批通过" }));

    expect(await screen.findByText("审批已完成")).toBeInTheDocument();
    expect(screen.getByText("申请人：Alice")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "选择 Bob" }));
    fireEvent.change(screen.getByLabelText("租户 ID"), { target: { value: "tenant_custom_bob" } });
    fireEvent.change(screen.getByLabelText("Token 上限"), { target: { value: "123456" } });
    fireEvent.change(screen.getByLabelText("审批备注"), { target: { value: "自定义备注" } });
    fireEvent.click(screen.getByRole("button", { name: "重置" }));

    await waitFor(() => {
      expect(screen.queryByText("审批已完成")).not.toBeInTheDocument();
    });
    expect(screen.getByLabelText("租户 ID")).toHaveValue("tenant_bob_co");
    expect(screen.getByLabelText("Token 上限")).toHaveValue(10000000);
    expect(screen.getByLabelText("审批备注")).toHaveValue("通过控制台审批");
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
        ...createUsageOverviewMock({
          total_requests: 4200,
          success_rate: "99.20%",
          total_tokens: "320 万",
          input_tokens: "180 万",
          output_tokens: "120 万",
          cached_tokens: "20 万",
          average_latency: "184 ms",
          estimated_share: "37%",
          input_cost: "1.20 万￥",
          output_cost: "4.80 万￥",
          cached_cost: "0.20 万￥",
          total_cost: "6.20 万￥",
        }),
      },
    });

    renderRoute("/tenants");

    expect(await screen.findByRole("heading", { level: 1, name: "租户管理" })).toBeInTheDocument();
    expect(screen.getByText("tenant_alpha")).toBeInTheDocument();
    expect(screen.getByText("生产网关")).toBeInTheDocument();
    expect(screen.getByText("320 万")).toBeInTheDocument();
    expect(screen.getAllByText("6.20 万￥").length).toBeGreaterThan(0);
    expect(screen.getAllByText("gpt-4o-mini").length).toBeGreaterThan(0);
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/overview");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/api-keys");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/overview?window=24h");
  });

  test("member 总览页使用 /me/overview 数据", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = mockFetch({
      "/api/me/overview": {
        tenant_id: "tenant_demo",
        tenant_name: "Demo Tenant",
        active_api_keys: 3,
      },
    });

    renderRoute("/me");

    expect((await screen.findAllByText("Demo Tenant")).length).toBeGreaterThan(0);
    expect((await screen.findAllByText("tenant_demo")).length).toBeGreaterThan(0);
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/me/overview");
  });

  test("member 总览请求会携带控制台会话头", async () => {
    const sessionToken = "session_token_demo";
    mockSession({
      role: "member",
      tenant_id: "tenant_demo",
      user_id: "user_member_a",
      token: sessionToken,
    });
    const fetchMock = mockFetch(
      {
        "/api/me/overview": {
          tenant_id: "tenant_demo",
          tenant_name: "Demo Tenant",
          active_api_keys: 2,
        },
      },
      {
        "/api/me/overview": (init) => {
          const headers = new Headers(init?.headers);
          expect(headers.get("X-Console-Session")).toBe(sessionToken);
        },
      },
    );

    renderRoute("/me");

    expect((await screen.findAllByText("Demo Tenant")).length).toBeGreaterThan(0);
    expect(fetchMock).toHaveBeenCalledWith("/api/me/overview", expect.any(Object));
  });

  test("member API 密钥页走 /me 接口且不显示租户输入或删除操作", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/me/api-keys" && !init?.method) {
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
      if (url === "/api/me/api-keys" && init?.method === "POST") {
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
    expect(fetchMock).toHaveBeenCalledWith("/api/me/api-keys");
  });

  test("member 创建密钥时立即展示提交中状态并在成功后滚动到结果区域", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const scrollIntoView = vi.fn();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView,
    });

    let resolveCreate: ((response: Response) => void) | null = null;
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/me/api-keys" && !init?.method) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [],
            }),
            {
              status: 200,
              headers: { "Content-Type": "application/json" },
            },
          ),
        );
      }

      if (url === "/api/me/api-keys" && init?.method === "POST") {
        return new Promise<Response>((resolve) => {
          resolveCreate = resolve;
        });
      }

      throw new Error(`Unexpected fetch url: ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/api-keys");

    fireEvent.click(await screen.findByRole("button", { name: "新建密钥" }));
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "member-pending-key" } });
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

    expect(screen.getByRole("button", { name: "创建中..." })).toBeDisabled();

    resolveCreate?.(
      new Response(
        JSON.stringify({
          item: {
            id: "mk_pending_done",
            name: "member-pending-key",
            tenant: "tenant_demo",
            status: "启用",
            scopes: ["chat"],
            last_used_at: "刚刚",
            owner_user_id: "user_member_a",
          },
          raw_key: "mk_pending_secret",
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    expect(await screen.findByText("新建密钥已完成")).toBeInTheDocument();
    expect(scrollIntoView).toHaveBeenCalled();
  });

  test("member 点击新建密钥时会自动滚动到创建面板", async () => {
    mockSession({ role: "member", tenant_id: "tenant_123", user_id: "user_member_123" });
    const scrollIntoView = vi.fn();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView,
    });

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/me/api-keys" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [],
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

    expect(await screen.findByRole("heading", { name: "新建密钥" })).toBeInTheDocument();
    expect(scrollIntoView).toHaveBeenCalled();
  });

  test("member API 密钥页在透明复制受限的浏览器中也能复制完整密钥", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("NotAllowedError"));
    const execCommand = vi.fn(function (this: unknown, command: string) {
      if (this !== document) {
        return false;
      }
      if (command !== "copy") {
        return false;
      }
      const textarea = document.body.querySelector("textarea");
      if (!(textarea instanceof HTMLTextAreaElement)) {
        return false;
      }
      return textarea.style.opacity !== "0";
    });
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: execCommand,
    });
    vi.stubGlobal("navigator", {
      clipboard: { writeText },
    });
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/me/api-keys" && !init?.method) {
        return new Response(
          JSON.stringify({
            items: [],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      if (url === "/api/me/api-keys" && init?.method === "POST") {
        return new Response(
          JSON.stringify({
            item: {
              id: "mk_copy_member",
              name: "member-copy-key",
              tenant: "tenant_demo",
              status: "启用",
              scopes: ["chat"],
              last_used_at: "刚刚",
              owner_user_id: "user_member_a",
            },
            raw_key: "mk_copy_secret",
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
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "member-copy-key" } });
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

    expect(await screen.findByText("新建密钥已完成")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "复制本次完整密钥" }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("mk_copy_secret");
      expect(execCommand).toHaveBeenCalledWith("copy");
      expect(screen.getByText("完整密钥已复制到剪贴板。")).toBeInTheDocument();
    });
  });

  test("member API 密钥页在 rotate 和 deactivate 时命中 /me 分支并更新本地状态", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/me/api-keys" && !init?.method) {
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
      if (url === "/api/me/api-keys/mk_live/rotate" && init?.method === "POST") {
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
      if (url === "/api/me/api-keys/mk_rotated/deactivate" && init?.method === "POST") {
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
      "/api/me/api-keys/mk_live/rotate",
      expect.objectContaining({ method: "POST" }),
    );
    expect(screen.getByRole("button", { name: "选择 member-rotated-key" })).toBeInTheDocument();
    expect(screen.getAllByText("停用").length).toBeGreaterThan(0);
    expect(screen.getByText("名称：member-rotated-key")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "停用密钥" }));
    fireEvent.click(screen.getByRole("button", { name: "确认停用" }));

    expect(await screen.findByText("停用操作已完成")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/me/api-keys/mk_rotated/deactivate",
      expect.objectContaining({ method: "POST" }),
    );
    expect(screen.getByText("状态：停用")).toBeInTheDocument();
  });

  test("member 调用观测页使用 /me usage overview 和 requests 数据", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = mockFetch({
      "/api/me/usage/overview": createUsageOverviewMock({
        total_requests: 120,
        total_tokens: "12 万",
        input_tokens: "7.2 万",
        output_tokens: "4.1 万",
        cached_tokens: "0.7 万",
        average_latency: "228 ms",
        estimated_share: "14%",
        input_cost: "0.32 ￥",
        output_cost: "1.20 ￥",
        cached_cost: "0.04 ￥",
        total_cost: "1.56 ￥",
      }),
      "/api/me/usage/requests?limit=20&offset=0": {
        items: [
          createUsageRequestMock({
            request_id: "req_1",
            tenant: "tenant_demo",
            resolved_model: "qwen-plus",
            task_class: "coding_complex",
            target_model_tier: "gateway-chat-reasoning",
            usage_source: "member_key",
            input_tokens: "840",
            output_tokens: "400",
            cached_tokens: "40",
            input_cost: "0.32 ￥",
            output_cost: "0.80 ￥",
            cached_cost: "0.08 ￥",
            total_cost: "1.20 ￥",
          }),
        ],
        total: 1,
        limit: 20,
        offset: 0,
      },
    });

    renderRoute("/usage");

    expect(await screen.findByText("98.40%")).toBeInTheDocument();
    expect(screen.getByText("req_1")).toBeInTheDocument();
    expect(screen.getByText("1.56 ￥")).toBeInTheDocument();
    expect(screen.getByText("840")).toBeInTheDocument();
    expect(screen.getByText("1.20 ￥")).toBeInTheDocument();
    expect(screen.getAllByText("qwen-plus").length).toBeGreaterThan(0);
    expect(screen.getByText("复杂编码请求")).toBeInTheDocument();
    expect(screen.getByText("强模型档位")).toBeInTheDocument();
    expect(screen.getAllByText("2.00 ￥/M").length).toBeGreaterThan(0);
    expect(fetchMock).toHaveBeenCalledWith("/api/me/usage/overview");
    expect(fetchMock).toHaveBeenCalledWith("/api/me/usage/requests?limit=20&offset=0");
  });

  test("member 失败分析页使用 /me/failures 数据", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = mockFetch({
      "/api/me/failures": {
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
    expect(fetchMock).toHaveBeenCalledWith("/api/me/failures");
  });

  test("member 审计页使用 /me/audit-events 数据而不是 admin audit", async () => {
    mockSession({ role: "member", tenant_id: "tenant_demo", user_id: "user_member_a" });
    const fetchMock = mockFetch({
      "/api/me/audit-events": {
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
    expect(fetchMock).toHaveBeenCalledWith("/api/me/audit-events");
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
            requested_model: "qwen-flash",
            route_label: "default-route",
            credential: providerAlias,
            latency: "218 ms",
            status: "健康",
            provider_group: "qwen",
          },
          {
            requested_model: "mimo-v2.5-pro",
            route_label: "default-route",
            credential: "provider_mimo_primary",
            latency: "286 ms",
            status: "健康",
            provider_group: "mimo",
          },
          {
            requested_model: "rag-query",
            route_label: "shared-route",
            credential: "provider_rag_service",
            latency: "312 ms",
            status: "告警",
            provider_group: "other",
          },
        ],
        policy_summary: [
          "模型优先解析已启用。",
          `请求会先匹配 ${providerAlias}，再按照 DashScope 主路由分发到${hiddenKnowledgeTerm}链路。`,
        ],
      },
    });

    renderRoute("/routes");

    expect(await screen.findByRole("heading", { level: 1, name: "路由" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "Qwen 路由观测" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "MIMO 路由观测" })).toBeInTheDocument();
    expect(screen.getAllByRole("columnheader", { name: "平台路由结果" })).toHaveLength(2);
    expect(screen.getByText("模型优先解析已启用。")).toBeInTheDocument();
    expect(
      screen.getByText("请求会先匹配 平台托管凭证，再按照 平台默认线路分发到内部检索能力链路。"),
    ).toBeInTheDocument();
    expect(screen.getByText("平台接入源")).toBeInTheDocument();
    expect(screen.getByText("qwen-flash")).toBeInTheDocument();
    expect(screen.getByText("mimo-v2.5-pro")).toBeInTheDocument();
    expect(screen.getAllByText("平台托管凭证")).toHaveLength(2);
    expect(screen.getAllByText("平台默认线路")).toHaveLength(2);
    expect(screen.queryByText("rag-query")).not.toBeInTheDocument();
    expect(screen.queryByText(new RegExp(providerAlias))).not.toBeInTheDocument();
    expect(screen.queryByText(/DashScope/)).not.toBeInTheDocument();
    expect(screen.queryByText(new RegExp(hiddenKnowledgeTerm))).not.toBeInTheDocument();
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
            status: "DashScope 限流",
            detail: `${hiddenKnowledgeTerm}链路回退到 OpenAI 主线路由后恢复成功`,
          },
        ],
        items: [
          {
            time: "09:42",
            tenant: "tenant_alpha",
            endpoint: "/v1/chat/completions",
            request_model: "qwen-flash",
            resolved_model: "qwen-plus",
            upstream_model: "qwen-plus",
            task_class: "coding_complex",
            target_model_tier: "gateway-chat-reasoning",
            routing_reason: "keyword:debug,pattern:code_fence",
            status: "200",
            route_label: "default-route",
            latency: "218 ms",
            usage_source: "上游返回",
            total_cost: "2.50 ￥",
          },
        ],
        summaries: [
          { title: "错误摘要", content: `配额超限和 ${providerAlias} 回退事件会在这里汇总。` },
          { title: "限流情况", content: "最近一小时内有 2 次租户限流。" },
        ],
      },
    });

    renderRoute("/audit");

    expect(await screen.findByRole("heading", { level: 1, name: "审计" })).toBeInTheDocument();
    expect(screen.getByText("最近事件流")).toBeInTheDocument();
    expect(screen.getByText("配额超限和 平台托管凭证 回退事件会在这里汇总。")).toBeInTheDocument();
    expect(screen.getByText("平台上游 限流")).toBeInTheDocument();
    expect(screen.getByText("内部检索能力链路回退到 平台默认线路后恢复成功")).toBeInTheDocument();
    expect(screen.getByText("/v1/chat/completions")).toBeInTheDocument();
    expect(screen.getAllByText("qwen-plus").length).toBeGreaterThan(0);
    expect(screen.getByText("复杂编码请求")).toBeInTheDocument();
    expect(screen.getByText("强模型档位")).toBeInTheDocument();
    expect(screen.getByText("命中关键词：debug；包含代码块")).toBeInTheDocument();
    expect(screen.getAllByText("总费用").length).toBeGreaterThan(0);
    expect(screen.getByText("2.50 ￥")).toBeInTheDocument();
    expect(screen.queryByText(new RegExp(providerAlias))).not.toBeInTheDocument();
    expect(screen.queryByText(/OpenAI/)).not.toBeInTheDocument();
    expect(screen.queryByText(new RegExp(hiddenKnowledgeTerm))).not.toBeInTheDocument();
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
            route_label: "default-route",
            latency: "82 ms",
            usage_source: "上游返回",
            total_cost: "0.32 ￥",
          },
        ],
        summaries: [{ title: "真实摘要", content: "最近 24 小时共 128 次请求，其中 4 次失败。" }],
      },
    });

    renderRoute("/audit");

    expect(await screen.findByText("最近 24 小时请求")).toBeInTheDocument();
    expect(screen.getByText("真实摘要")).toBeInTheDocument();
    expect(screen.getAllByText("qwen-flash")).toHaveLength(2);
    expect(screen.getByText("0.32 ￥")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/audit");
  });

  test("调试场页可以加载数据并提交最近一次请求结果", async () => {
    const fetchMock = mockFetch(
      {
        "/api/admin/playground": {
          available_models: ["qwen-plus", "text-embedding-v3"],
          last_run: {
            route_label: "default-route",
            endpoint: "/v1/chat/completions",
            latency: "218 ms",
            status: "200 OK",
            response: "旧结果",
            platform_key: "prod-gateway",
          },
        },
        "/api/admin/playground/chat": {
          route_label: "backup-route",
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
    expect(screen.getByText("平台路由结果")).toBeInTheDocument();
    expect(screen.getAllByText("平台默认线路").length).toBeGreaterThan(0);
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
      "/api/admin/usage/overview": createUsageOverviewMock(),
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
        costs: [
          { label: "04-24 18:00", value: "0.20 ￥" },
          { label: "04-24 19:00", value: "0.32 ￥" },
        ],
      },
      "/api/admin/usage/latency-wall?window=24h": {
        window_label: "最近 24 小时",
        buckets: ["04-24 18:00", "04-24 19:00"],
        lanes: [
          {
            model: "text-embedding-v4",
            route_label: "default-route",
            success_rate: "100.00%",
            average_latency: "64 ms",
            cells: [
              { bucket_label: "04-24 18:00", latency: "64 ms", status: "健康", requests: "4 次" },
              { bucket_label: "04-24 19:00", latency: "--", status: "空闲", requests: "0 次" },
            ],
          },
          {
            model: "qwen-flash",
            route_label: "default-route",
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
        recent_events: [`04-24 19:08 · DashScope 限流 · ${hiddenKnowledgeTerm}请求失败（429）`],
      },
      "/api/admin/usage/requests": {
        items: [createUsageRequestMock({
          request_id: "llmreq_demo_002",
          endpoint: "/v1/embeddings",
          model: "text-embedding-3-small",
          status: "限流",
          total_tokens: "16",
          input_tokens: "12",
          output_tokens: "0",
          cached_tokens: "4",
          latency: "95 ms",
          usage_source: "估算",
          input_cost: "0.03 ￥",
          output_cost: "0.00 ￥",
          cached_cost: "0.01 ￥",
          total_cost: "0.04 ￥",
        })],
        total: 1,
        limit: 20,
        offset: 0,
      },
      "/api/admin/usage/requests?limit=20&offset=0": {
        items: [createUsageRequestMock({
          request_id: "llmreq_demo_002",
          endpoint: "/v1/embeddings",
          model: "text-embedding-3-small",
          status: "限流",
          total_tokens: "16",
          input_tokens: "12",
          output_tokens: "0",
          cached_tokens: "4",
          latency: "95 ms",
          usage_source: "估算",
          input_cost: "0.03 ￥",
          output_cost: "0.00 ￥",
          cached_cost: "0.01 ￥",
          total_cost: "0.04 ￥",
        })],
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
    expect(screen.getAllByText("总费用").length).toBeGreaterThan(0);
    expect(screen.getByText("0.52 ￥")).toBeInTheDocument();
    expect(screen.getByText("费用趋势")).toBeInTheDocument();
    expect(screen.getByText("0.32 ￥")).toBeInTheDocument();
    expect(screen.getByText("04-24 19:08 · 平台上游 限流 · 内部检索能力请求失败（429）")).toBeInTheDocument();
    expect(screen.getByText("llmreq_demo_002")).toBeInTheDocument();
    expect(screen.getByText("0.04 ￥")).toBeInTheDocument();
    expect(screen.getAllByText("2.00 ￥/M").length).toBeGreaterThan(0);
    expect(screen.getAllByText("gpt-4o-mini").length).toBeGreaterThan(0);
    expect(screen.queryByText("text-embedding-v4")).not.toBeInTheDocument();
    expect(screen.getByText("平台默认线路")).toBeInTheDocument();
    expect(screen.queryByText(/DashScope/)).not.toBeInTheDocument();
    expect(screen.queryByText(new RegExp(hiddenKnowledgeTerm))).not.toBeInTheDocument();

    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/overview?window=24h");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/trends?window=24h");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/latency-wall?window=24h");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/failures?window=24h");
    expect(fetchMock).toHaveBeenCalledWith("/api/admin/usage/requests?limit=20&offset=0");
  });

  test("调用观测页展示智能路由分类与路由原因", async () => {
    mockFetch({
      "/api/admin/usage/overview": createUsageOverviewMock(),
      "/api/admin/usage/trends": { requests: [], tokens: [], success: [], costs: [] },
      "/api/admin/usage/latency-wall?window=24h": { window_label: "最近 24 小时", buckets: [], lanes: [] },
      "/api/admin/usage/failures": { breakdown: [], recent_events: [] },
      "/api/admin/usage/requests?limit=20&offset=0": {
        items: [
          createUsageRequestMock({
            task_class: "coding_complex",
            target_model_tier: "gateway-chat-reasoning",
            routing_reason: "keyword:debug,pattern:code_fence",
            resolved_model: "qwen-plus",
          }),
        ],
        total: 1,
        limit: 20,
        offset: 0,
      },
    });

    renderRoute("/usage");

    expect(await screen.findByText("复杂编码请求")).toBeInTheDocument();
    expect(screen.getByText("强模型档位")).toBeInTheDocument();
    expect(screen.getByText("命中关键词：debug；包含代码块")).toBeInTheDocument();
    expect(screen.getByText("qwen-plus")).toBeInTheDocument();
  });

  test("调用观测页使用可视化组件展示状态、事件流与来源 pill", async () => {
    mockFetch({
      "/api/admin/usage/overview": createUsageOverviewMock(),
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
        costs: [
          { label: "04-24 18:00", value: "0.20 ￥" },
          { label: "04-24 19:00", value: "0.32 ￥" },
        ],
      },
      "/api/admin/usage/latency-wall?window=24h": {
        window_label: "最近 24 小时",
        buckets: ["04-24 18:00", "04-24 19:00"],
        lanes: [
          {
            model: "qwen-flash",
            route_label: "default-route",
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
        recent_events: [`04-24 19:08 · DashScope 限流 · ${hiddenKnowledgeTerm}请求失败（429）`],
      },
      "/api/admin/usage/requests?limit=20&offset=0": {
        items: [createUsageRequestMock({
          request_id: "llmreq_demo_002",
          endpoint: "/v1/embeddings",
          model: "text-embedding-3-small",
          status: "限流",
          total_tokens: "16",
          input_tokens: "12",
          output_tokens: "0",
          cached_tokens: "4",
          latency: "95 ms",
          usage_source: "估算",
          input_cost: "0.03 ￥",
          output_cost: "0.00 ￥",
          cached_cost: "0.01 ￥",
          total_cost: "0.04 ￥",
        })],
        total: 1,
        limit: 20,
        offset: 0,
      },
    });

    renderRoute("/usage");

    expect(await screen.findByText("实时运行视图")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "最近 24 小时" }).length).toBeGreaterThan(0);
    expect(screen.getByText("异常事件流")).toBeInTheDocument();
    expect(screen.getByLabelText("状态 限流")).toBeInTheDocument();
    expect(screen.getByLabelText("来源 估算")).toBeInTheDocument();
    expect(screen.getAllByText("健康").length).toBeGreaterThan(0);
    expect(screen.getAllByText("失败").length).toBeGreaterThan(0);
  });

  test("调用观测页在部分请求失败时优先显示错误", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();

      if (url === "/api/admin/usage/overview?window=24h") {
        return Promise.resolve(new Response("boom", { status: 500 }));
      }
      if (url === "/api/admin/usage/trends?window=24h") {
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
      if (url === "/api/admin/usage/failures?window=24h") {
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
      "/api/admin/usage/overview": createUsageOverviewMock({ total_requests: 41 }),
      "/api/admin/usage/trends": {
        requests: [],
        tokens: [],
        success: [],
        costs: [],
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
        items: [createUsageRequestMock({
          request_id: "llmreq_page_1",
          model: "qwen-flash",
          total_tokens: "32",
          input_tokens: "18",
          output_tokens: "10",
          cached_tokens: "4",
          latency: "80 ms",
        })],
        total: 41,
        limit: 20,
        offset: 0,
      },
      "/api/admin/usage/requests?limit=20&offset=20": {
        items: [createUsageRequestMock({
          request_id: "llmreq_page_2",
          model: "qwen-flash",
          total_tokens: "28",
          input_tokens: "16",
          output_tokens: "8",
          cached_tokens: "4",
          latency: "76 ms",
        })],
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
      "/api/admin/usage/overview": createUsageOverviewMock({ total_requests: 41 }),
      "/api/admin/usage/trends": {
        requests: [],
        tokens: [],
        success: [],
        costs: [],
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
        items: [createUsageRequestMock({
          request_id: "llmreq_page_1",
          model: "qwen-flash",
          total_tokens: "32",
          input_tokens: "18",
          output_tokens: "10",
          cached_tokens: "4",
          latency: "80 ms",
        })],
        total: 41,
        limit: 20,
        offset: 0,
      },
      "/api/admin/usage/requests?limit=20&offset=20": {
        items: [createUsageRequestMock({
          request_id: "llmreq_page_2",
          model: "qwen-flash",
          total_tokens: "28",
          input_tokens: "16",
          output_tokens: "8",
          cached_tokens: "4",
          latency: "76 ms",
        })],
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
