import type { ReactElement } from "react";
import { Navigate, createBrowserRouter, createMemoryRouter } from "react-router-dom";

import { APIKeysPage } from "../pages/api-keys";
import { AdminApplicationsPage } from "../pages/admin-applications";
import { AdminTenantsPage } from "../pages/admin-tenants";
import { AuditPage } from "../pages/audit";
import { DashboardPage } from "../pages/dashboard";
import { MemberFailuresPage } from "../pages/member-failures";
import { MemberOverviewPage } from "../pages/member-overview";
import { MemberUsagePage } from "../pages/member-usage";
import { PlaygroundPage } from "../pages/playground";
import { RoutesPage } from "../pages/routes";
import { UsagePage } from "../pages/usage";
import { getConsoleSession, type ConsoleSession } from "../lib/session";
import { AppLayout, type ConsoleNavigationItem, type ConsoleRouteMeta } from "./layout";

type ConsoleRouteDefinition = ConsoleNavigationItem & {
  element: ReactElement;
};

const adminHiddenNavigation = [
  {
    path: "/routes",
    label: "路由",
    title: "路由",
    description: "检查模型映射、路由标签与执行策略。",
    element: <RoutesPage />,
  },
  {
    path: "/playground",
    label: "调试场",
    title: "调试场",
    description: "在正式使用前验证平台接口与处理链路结果。",
    element: <PlaygroundPage />,
  },
] satisfies readonly ConsoleRouteDefinition[];

export const adminNavigation = [
  {
    path: "/applications",
    label: "账号申请",
    title: "账号申请",
    description: "审核待处理申请并为新成员分配租户归属。",
    element: <AdminApplicationsPage />,
  },
  {
    path: "/tenants",
    label: "租户管理",
    title: "租户管理",
    description: "查看租户状态、成员归属与平台侧治理信息。",
    element: <AdminTenantsPage />,
  },
  {
    path: "/",
    label: "总览",
    title: "总览",
    description: "查看网关健康、租户态势与核心平台指标。",
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
    description: "追踪请求历史、处理链路与运维事件。",
    element: <AuditPage />,
  },
] satisfies readonly ConsoleRouteDefinition[];

export const memberNavigation = [
  {
    path: "/me",
    label: "我的总览",
    title: "我的总览",
    description: "查看租户侧账号、调用和配额概览。",
    element: <MemberOverviewPage />,
  },
  {
    path: "/api-keys",
    label: "API 密钥",
    title: "API 密钥",
    description: "自助创建、轮换与停用平台密钥。",
    element: <APIKeysPage />,
  },
  {
    path: "/usage",
    label: "调用记录",
    title: "调用记录",
    description: "查看当前租户请求、Token 与状态。",
    element: <MemberUsagePage />,
  },
  {
    path: "/failures",
    label: "失败记录",
    title: "失败记录",
    description: "查看失败分类、阶段和可重试性。",
    element: <MemberFailuresPage />,
  },
  {
    path: "/audit",
    label: "审计轨迹",
    title: "审计轨迹",
    description: "查看关键操作和风控提示。",
    element: <AuditPage />,
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
      : [...navigation, ...adminHiddenNavigation].map(createChildRoute);

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
