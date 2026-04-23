import { render, screen } from "@testing-library/react";
import { RouterProvider } from "react-router-dom";
import { expect, test } from "vitest";

import { createTestRouter } from "../app/router";

test("renders dashboard overview shell", async () => {
  render(<RouterProvider router={createTestRouter()} />);

  expect(
    await screen.findByRole("heading", { level: 2, name: "Overview" }),
  ).toBeInTheDocument();
  expect(screen.getByText("Requests 24h")).toBeInTheDocument();
  expect(screen.getByRole("heading", { level: 3, name: "Route Health" })).toBeInTheDocument();
});

test("renders topbar metadata for a matched secondary route", async () => {
  render(<RouterProvider router={createTestRouter(["/knowledge-base"])} />);

  expect(
    await screen.findByRole("heading", { level: 1, name: "Knowledge Base" }),
  ).toBeInTheDocument();
  expect(
    screen.getByText("Review document ingestion, chunking, and RAG readiness."),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { level: 3, name: "RAG Query Flow" }),
  ).toBeInTheDocument();
});
