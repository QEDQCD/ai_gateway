import { render, screen } from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { expect, test } from "vitest";

import { AppLayout } from "../app/layout";
import { createTestRouter } from "../app/router";

test("renders dashboard route", async () => {
  render(<RouterProvider router={createTestRouter()} />);
  expect(
    await screen.findByRole("heading", { level: 2, name: "Overview" }),
  ).toBeInTheDocument();
});

test("renders topbar metadata from the matched route handle", async () => {
  const router = createMemoryRouter([
    {
      path: "/",
      element: <AppLayout />,
      children: [
        {
          index: true,
          element: <div>Custom dashboard</div>,
          handle: {
            title: "Custom Overview",
            description: "Topbar metadata should come from the current route handle.",
          },
        },
      ],
    },
  ]);

  render(<RouterProvider router={router} />);

  expect(
    await screen.findByRole("heading", { level: 1, name: "Custom Overview" }),
  ).toBeInTheDocument();
  expect(
    screen.getByText("Topbar metadata should come from the current route handle."),
  ).toBeInTheDocument();
});
