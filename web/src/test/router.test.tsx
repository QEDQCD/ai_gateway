import { render, screen } from "@testing-library/react";
import { RouterProvider } from "react-router-dom";

import { createTestRouter } from "../app/router";

test("renders dashboard route", async () => {
  render(<RouterProvider router={createTestRouter()} />);
  expect(await screen.findByText("Overview")).toBeInTheDocument();
});
