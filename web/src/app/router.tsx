import { createBrowserRouter, createMemoryRouter } from "react-router-dom";

import { DashboardPage } from "../pages/dashboard";
import { AppLayout, navigation, type ConsoleRouteMeta } from "./layout";

const routeTree = [
  {
    path: "/",
    element: <AppLayout />,
    children: [
      {
        index: true,
        element: <DashboardPage />,
        handle: {
          title: navigation[0].title,
          description: navigation[0].description,
        } satisfies ConsoleRouteMeta,
      },
    ],
  },
];

export function createAppRouter() {
  return createBrowserRouter(routeTree);
}

export function createTestRouter(initialEntries: string[] = ["/"]) {
  return createMemoryRouter(routeTree, { initialEntries });
}
