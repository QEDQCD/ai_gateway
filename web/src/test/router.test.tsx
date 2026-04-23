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
