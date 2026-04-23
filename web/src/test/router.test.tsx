import { render, screen } from "@testing-library/react";
import { RouterProvider } from "react-router-dom";
import { expect, test } from "vitest";

import { createTestRouter } from "../app/router";

test("renders the overview shell smoke test", async () => {
  render(<RouterProvider router={createTestRouter()} />);

  expect(
    await screen.findByRole("heading", { level: 1, name: "Overview" }),
  ).toBeInTheDocument();
  expect(
    screen.getByText("Monitor gateway health, routing posture, and core platform signals."),
  ).toBeInTheDocument();
  expect(screen.getByText("Requests 24h")).toBeInTheDocument();
});

test("renders the routes shell smoke test", async () => {
  render(<RouterProvider router={createTestRouter(["/routes"])} />);

  expect(
    await screen.findByRole("heading", { level: 1, name: "Routes" }),
  ).toBeInTheDocument();
  expect(
    screen.getByText("Inspect model mappings, provider resolution, and fallback behavior."),
  ).toBeInTheDocument();
  expect(screen.getByRole("heading", { level: 2, name: "Routes" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { level: 3, name: "Routing Policy" })).toBeInTheDocument();
});
