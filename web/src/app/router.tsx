import type { ReactElement } from "react";
import { Navigate, createBrowserRouter, createMemoryRouter } from "react-router-dom";

import { APIKeysPage } from "../pages/api-keys";
import { AuditPage } from "../pages/audit";
import { DashboardPage } from "../pages/dashboard";
import { PlaygroundPage } from "../pages/playground";
import { RoutesPage } from "../pages/routes";
import { UsagePage } from "../pages/usage";
import { getConsoleSession, type ConsoleSession } from "../lib/session";
import { AppLayout, type ConsoleNavigationItem, type ConsoleRouteMeta } from "./layout";

type ConsoleRouteDefinition = ConsoleNavigationItem & {
  element: ReactElement;
};

function PlaceholderPage({ title }: { title: string }) {
  return (
    <section>
      <h2>{title}</h2>
    </section>
  );
}

export const adminNavigation = [
  {
    path: "/applications",
    label: "账号申请",
    title: "账号申请",
    description: "审核待处理申请并为新成员分配租户归属。",
    element: <PlaceholderPage title="账号申请" />,
  },
  {
    path: "/tenants",
    label: "租户管理",
    title: "租户管理",
    description: "查看租户状态、成员归属与平台侧治理信息。",
    element: <PlaceholderPage title="租户管理" />,
  },
  {
    path: "/",
    label: "总览",
    title: "总览",
    description: "查看网关健康、路由态势与核心平台指标。",
    element: <DashboardPage />,
  },
  {
    path: "/api-keys",
    label: "API 密钥",
    title: "API 密钥",
    description: "管理平台密钥、权限范围与租户访问状态。",
    element: <APIKeysPage />,
  },
  {
    path: "/routes",
    label: "路由",
    title: "路由",
    description: "检查模型映射、供应商解析与回退策略。",
    element: <RoutesPage />,
  },
  {
    path: "/playground",
    label: "调试场",
    title: "调试场",
    description: "在正式使用前验证模型请求与路由结果。",
    element: <PlaygroundPage />,
  },
  {
    path: "/usage",
    label: "调用观测",
    title: "调用观测",
    description: "查看 Token、成功率、失败分类与调用明细。",
    element: <UsagePage />,
  },
  {
    path: "/audit",
    label: "审计",
    title: "审计",
    description: "追踪请求历史、供应商解析与运维事件。",
    element: <AuditPage />,
  },
] satisfies readonly ConsoleRouteDefinition[];

export const memberNavigation = [
  {
    path: "/me",
    label: "我的总览",
    title: "我的总览",
    description: "查看租户侧账号、调用和配额概览。",
    element: <PlaceholderPage title="我的总览" />,
  },
  {
    path: "/api-keys",
    label: "我的密钥",
    title: "我的密钥",
    description: "管理当前成员创建的 API 密钥与使用范围。",
    element: <PlaceholderPage title="我的密钥" />,
  },
  {
    path: "/usage",
    label: "调用观测",
    title: "调用观测",
    description: "查看当前租户请求量、成功率和成本估算。",
    element: <PlaceholderPage title="调用观测" />,
  },
  {
    path: "/failures",
    label: "失败分析",
    title: "失败分析",
    description: "定位失败类别、时间分布与最近异常请求。",
    element: <PlaceholderPage title="失败分析" />,
  },
  {
    path: "/audit",
    label: "审计记录",
    title: "审计记录",
    description: "查看成员侧密钥操作与租户内审计事件。",
    element: <PlaceholderPage title="审计记录" />,
  },
] satisfies readonly ConsoleRouteDefinition[];

export function getNavigationForRole(role: ConsoleSession["role"]) {
  return role === "admin" ? adminNavigation : memberNavigation;
}

function toRouteMeta(route: ConsoleNavigationItem): ConsoleRouteMeta {
  return {
    title: route.title,
    description: route.description,
  };
}

function createChildRoute(route: ConsoleRouteDefinition) {
  if (route.path === "/") {
    return {
      index: true,
      element: route.element,
      handle: toRouteMeta(route),
    };
  }

  return {
    path: route.path.slice(1),
    element: route.element,
    handle: toRouteMeta(route),
  };
}

function createRouteTree(session: ConsoleSession) {
  const navigation = getNavigationForRole(session.role);
  const children =
    session.role === "member"
      ? [{ index: true, element: <Navigate to="/me" replace /> }, ...navigation.map(createChildRoute)]
      : navigation.map(createChildRoute);

  return [
    {
      path: "/",
      element: <AppLayout navigation={navigation} session={session} />,
      children,
    },
  ];
}

export function createAppRouter(session: ConsoleSession = getConsoleSession()) {
  return createBrowserRouter(createRouteTree(session), {
    future: {
      v7_startTransition: true,
    },
  });
}

export function createTestRouter(
  initialEntries: string[] = ["/"],
  session: ConsoleSession = getConsoleSession(),
) {
  return createMemoryRouter(createRouteTree(session), {
    initialEntries,
    future: {
      v7_startTransition: true,
    },
  });
}
