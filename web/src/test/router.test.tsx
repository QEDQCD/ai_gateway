import { render, screen } from "@testing-library/react";
import { RouterProvider } from "react-router-dom";
import { expect, test } from "vitest";

import { createTestRouter } from "../app/router";

test("renders dashboard route", async () => {
  render(<RouterProvider router={createTestRouter()} />);
  expect(
    await screen.findByRole("heading", { level: 2, name: "Overview" }),
  ).toBeInTheDocument();
});
