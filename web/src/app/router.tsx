import type { ReactElement } from "react";
import { createBrowserRouter, createMemoryRouter } from "react-router-dom";

import { APIKeysPage } from "../pages/api-keys";
import { AuditPage } from "../pages/audit";
import { DashboardPage } from "../pages/dashboard";
import { KnowledgeBasePage } from "../pages/knowledge-base";
import { PlaygroundPage } from "../pages/playground";
import { RoutesPage } from "../pages/routes";
import { AppLayout, type ConsoleNavigationItem, type ConsoleRouteMeta } from "./layout";

type ConsoleRouteDefinition = ConsoleNavigationItem & {
  element: ReactElement;
};

export const navigation = [
  {
    path: "/",
    label: "Overview",
    title: "Overview",
    description: "Monitor gateway health, routing posture, and core platform signals.",
    element: <DashboardPage />,
  },
  {
    path: "/api-keys",
    label: "API Keys",
    title: "API Keys",
    description: "Manage platform keys, scopes, and tenant access posture.",
    element: <APIKeysPage />,
  },
  {
    path: "/routes",
    label: "Routes",
    title: "Routes",
    description: "Inspect model mappings, provider resolution, and fallback behavior.",
    element: <RoutesPage />,
  },
  {
    path: "/playground",
    label: "Playground",
    title: "Playground",
    description: "Validate requests against the gateway before production usage.",
    element: <PlaygroundPage />,
  },
  {
    path: "/knowledge-base",
    label: "Knowledge Base",
    title: "Knowledge Base",
    description: "Review document ingestion, chunking, and RAG readiness.",
    element: <KnowledgeBasePage />,
  },
  {
    path: "/audit",
    label: "Audit",
    title: "Audit",
    description: "Trace request history, provider resolution, and operational events.",
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
  return createBrowserRouter(routeTree);
}

export function createTestRouter(initialEntries: string[] = ["/"]) {
  return createMemoryRouter(routeTree, { initialEntries });
}
