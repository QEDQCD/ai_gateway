import type { ReactElement } from "react";
import { createBrowserRouter, createMemoryRouter } from "react-router-dom";

import { APIKeysPage } from "../pages/api-keys";
import { AuditPage } from "../pages/audit";
import { DashboardPage } from "../pages/dashboard";
import { PlaygroundPage } from "../pages/playground";
import { RoutesPage } from "../pages/routes";
import { UsagePage } from "../pages/usage";
import { AppLayout, type ConsoleNavigationItem, type ConsoleRouteMeta } from "./layout";

type ConsoleRouteDefinition = ConsoleNavigationItem & {
  element: ReactElement;
};

export const navigation = [
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

const routeTree = [
  {
    path: "/",
    element: <AppLayout navigation={navigation} />,
    children: navigation.map(createChildRoute),
  },
];

export function createAppRouter() {
  return createBrowserRouter(routeTree, {
    future: {
      v7_startTransition: true,
    },
  });
}

export function createTestRouter(initialEntries: string[] = ["/"]) {
  return createMemoryRouter(routeTree, {
    initialEntries,
    future: {
      v7_startTransition: true,
    },
  });
}
