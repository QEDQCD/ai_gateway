# Web Console Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the React web console shell with a fixed sidebar, top bar, six primary routes, and realistic static page structures for the AI Gateway control plane.

**Architecture:** The web app stays intentionally thin in this task: Vite + React + TypeScript provide the app shell, React Router owns the route tree, and each page composes stable static cards/tables that look ready for Task 9 data wiring. Shared UI is kept minimal and focused on shell composition rather than a full component library.

**Tech Stack:** React 18, TypeScript, Vite, Vitest, React Testing Library, React Router DOM, CSS.

---

## File Structure

- `web/package.json` - real dependencies and scripts for Vite/Vitest
- `web/tsconfig.json` - TypeScript compiler settings
- `web/vite.config.ts` - Vite + Vitest config
- `web/index.html` - HTML entry
- `web/src/main.tsx` - React bootstrap
- `web/src/styles.css` - shell styling
- `web/src/app/router.tsx` - route tree plus browser/test router factories
- `web/src/app/layout.tsx` - sidebar shell, top bar shell, and navigation metadata
- `web/src/pages/dashboard.tsx` - Overview page
- `web/src/pages/api-keys.tsx` - API Keys page
- `web/src/pages/routes.tsx` - Routes page
- `web/src/pages/playground.tsx` - Playground page
- `web/src/pages/knowledge-base.tsx` - Knowledge Base page
- `web/src/pages/audit.tsx` - Audit page
- `web/src/test/router.test.tsx` - route smoke test
- `web/src/test/setup.ts` - Vitest DOM setup
- `web/src/vite-env.d.ts` - Vite type declarations

## Task 1: Bootstrap the Web Toolchain and Write the Failing Route Test

**Files:**
- Modify: `web/package.json`
- Create: `web/tsconfig.json`
- Create: `web/vite.config.ts`
- Create: `web/src/test/setup.ts`
- Create: `web/src/vite-env.d.ts`
- Create: `web/src/test/router.test.tsx`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: Replace the minimal package manifest with a real React/Vite/Vitest setup**

```json
{
  "name": "ai-gateway-web",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "test": "vitest run"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-router-dom": "^6.30.1"
  },
  "devDependencies": {
    "@testing-library/jest-dom": "^6.6.3",
    "@testing-library/react": "^16.3.0",
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.4",
    "jsdom": "^25.0.1",
    "typescript": "^5.6.3",
    "vite": "^5.4.10",
    "vitest": "^2.1.4"
  }
}
```

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "Bundler",
    "allowImportingTsExtensions": false,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "types": ["vitest/globals"]
  },
  "include": ["src"]
}
```

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
  },
});
```

```ts
import "@testing-library/jest-dom/vitest";
```

```ts
/// <reference types="vite/client" />
```

- [ ] **Step 2: Write the route smoke test before creating the router**

```tsx
import { render, screen } from "@testing-library/react";
import { RouterProvider } from "react-router-dom";

import { createTestRouter } from "../app/router";

test("renders dashboard route", async () => {
  render(<RouterProvider router={createTestRouter()} />);
  expect(await screen.findByText("Overview")).toBeInTheDocument();
});
```

- [ ] **Step 3: Run the smoke test and verify it fails because the router does not exist yet**

Run: `cd $PROJECT_ROOT/web && npm test -- --runInBand`
Expected: FAIL with an import error similar to `Cannot find module '../app/router'`

## Task 2: Build the App Shell, Router, and Default Overview Route

**Files:**
- Create: `web/index.html`
- Create: `web/src/main.tsx`
- Create: `web/src/styles.css`
- Create: `web/src/app/router.tsx`
- Create: `web/src/app/layout.tsx`
- Create: `web/src/pages/dashboard.tsx`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: Add the HTML entry point**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>AI Gateway Console</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 2: Add the React bootstrap and core shell styles**

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import { RouterProvider } from "react-router-dom";

import { createAppRouter } from "./app/router";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <RouterProvider router={createAppRouter()} />
  </React.StrictMode>,
);
```

```css
:root {
  color: #172033;
  background: #f4f6f8;
  font-family: "Segoe UI", "Helvetica Neue", Arial, sans-serif;
  line-height: 1.5;
  font-weight: 400;
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
  min-width: 320px;
  min-height: 100vh;
}

a {
  color: inherit;
  text-decoration: none;
}

.app-shell {
  display: grid;
  grid-template-columns: 248px minmax(0, 1fr);
  min-height: 100vh;
}

.sidebar {
  background: #152132;
  color: #eef3f8;
  padding: 24px 20px;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.sidebar__brand {
  font-size: 20px;
  font-weight: 700;
}

.sidebar__nav {
  display: grid;
  gap: 8px;
}

.sidebar__link {
  padding: 10px 12px;
  border-radius: 10px;
  color: #c5d0dd;
}

.sidebar__link--active {
  background: #243247;
  color: #ffffff;
}

.sidebar__status {
  margin-top: auto;
  display: grid;
  gap: 8px;
}

.shell-main {
  display: grid;
  grid-template-rows: auto 1fr;
  min-width: 0;
}

.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20px 28px;
  border-bottom: 1px solid #dde4ea;
  background: #ffffff;
}

.topbar__meta h1 {
  margin: 0;
  font-size: 24px;
}

.topbar__meta p {
  margin: 4px 0 0;
  color: #5c6778;
}

.topbar__badges {
  display: flex;
  gap: 10px;
}

.status-badge {
  padding: 8px 12px;
  border-radius: 999px;
  font-size: 13px;
  font-weight: 600;
}

.status-badge--healthy {
  background: #e7f5ec;
  color: #0e6a37;
}

.status-badge--neutral {
  background: #eef2f6;
  color: #304154;
}

.page-content {
  padding: 28px;
}

.page-grid {
  display: grid;
  gap: 20px;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}

.two-column-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.section-card {
  background: #ffffff;
  border: 1px solid #dde4ea;
  border-radius: 16px;
  padding: 20px;
}

.section-card h2,
.section-card h3 {
  margin: 0 0 12px;
}

.stat-card__label {
  margin: 0;
  color: #647285;
  font-size: 14px;
}

.stat-card__value {
  margin: 8px 0 0;
  font-size: 28px;
  font-weight: 700;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
}

.data-table th,
.data-table td {
  padding: 10px 0;
  border-bottom: 1px solid #edf1f4;
  text-align: left;
  font-size: 14px;
}

.page-actions {
  display: flex;
  gap: 12px;
}

.button-shell {
  border: 1px solid #c9d4df;
  background: #ffffff;
  color: #223043;
  border-radius: 10px;
  padding: 10px 14px;
  font-weight: 600;
}

@media (max-width: 960px) {
  .app-shell {
    grid-template-columns: 1fr;
  }

  .stats-grid,
  .two-column-grid {
    grid-template-columns: 1fr;
  }
}
```

- [ ] **Step 3: Add the layout shell and split routing into production and test router factories**

```tsx
import { NavLink, Outlet } from "react-router-dom";

export type ConsoleRouteMeta = {
  title: string;
  description: string;
};

export const navigation = [
  {
    path: "/",
    label: "Overview",
    title: "Overview",
    description: "Monitor gateway health, routing posture, and core platform signals.",
  },
];

export function AppLayout() {
  const current = navigation[0];

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="sidebar__brand">AI Gateway Console</div>
        <nav className="sidebar__nav">
          {navigation.map((item) => (
            <NavLink
              key={item.path}
              end={item.path === "/"}
              to={item.path}
              className={({ isActive }) =>
                isActive ? "sidebar__link sidebar__link--active" : "sidebar__link"
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar__status">
          <span className="status-badge status-badge--neutral">MVP</span>
          <span className="status-badge status-badge--healthy">Gateway Healthy</span>
        </div>
      </aside>
      <div className="shell-main">
        <header className="topbar">
          <div className="topbar__meta">
            <h1>{current.title}</h1>
            <p>{current.description}</p>
          </div>
          <div className="topbar__badges">
            <span className="status-badge status-badge--healthy">Gateway Healthy</span>
            <span className="status-badge status-badge--neutral">Quota Guard Active</span>
          </div>
        </header>
        <main className="page-content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
```

```tsx
import { createBrowserRouter, createMemoryRouter } from "react-router-dom";

import { AppLayout, navigation, type ConsoleRouteMeta } from "./layout";
import { DashboardPage } from "../pages/dashboard";

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
```

```tsx
export function DashboardPage() {
  return <h2>Overview</h2>;
}
```

- [ ] **Step 4: Run the smoke test and verify it passes with the default route**

Run: `cd $PROJECT_ROOT/web && npm test -- --runInBand`
Expected: PASS with `renders dashboard route`

## Task 3: Expand the Route Tree and Add the Six Primary Pages

**Files:**
- Modify: `web/src/app/router.tsx`
- Modify: `web/src/app/layout.tsx`
- Create: `web/src/pages/api-keys.tsx`
- Create: `web/src/pages/routes.tsx`
- Create: `web/src/pages/playground.tsx`
- Create: `web/src/pages/knowledge-base.tsx`
- Create: `web/src/pages/audit.tsx`
- Modify: `web/src/pages/dashboard.tsx`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: Replace the dashboard stub with a realistic Overview page shell**

```tsx
function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <section className="section-card">
      <p className="stat-card__label">{label}</p>
      <p className="stat-card__value">{value}</p>
    </section>
  );
}

function TableShell({
  title,
  columns,
  rows,
}: {
  title: string;
  columns: string[];
  rows: string[][];
}) {
  return (
    <section className="section-card">
      <h3>{title}</h3>
      <table className="data-table">
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column}>{column}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.join("-")}>
              {row.map((cell) => (
                <td key={cell}>{cell}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

export function DashboardPage() {
  return (
    <div className="page-grid">
      <div className="stats-grid">
        <StatCard label="Requests 24h" value="1.28M" />
        <StatCard label="Success Rate" value="99.42%" />
        <StatCard label="Quota Usage" value="74%" />
        <StatCard label="Active API Keys" value="184" />
      </div>
      <div className="two-column-grid">
        <TableShell
          title="Route Health"
          columns={["Requested Model", "Resolved Provider", "Latency", "Status"]}
          rows={[
            ["gpt-4o-mini", "OpenAI Primary", "218 ms", "Healthy"],
            ["text-embedding-3-small", "OpenAI Primary", "64 ms", "Healthy"],
            ["RAG Query", "RAG Service", "312 ms", "Warning"],
          ]}
        />
        <TableShell
          title="Top Models"
          columns={["Model", "Requests", "Share", "Mode"]}
          rows={[
            ["gpt-4o-mini", "612k", "48%", "Chat"],
            ["text-embedding-3-small", "301k", "24%", "Embedding"],
            ["RAG Query", "92k", "7%", "Knowledge"],
          ]}
        />
      </div>
      <div className="two-column-grid">
        <TableShell
          title="Recent Alerts"
          columns={["Time", "Type", "Scope"]}
          rows={[
            ["09:42", "Quota warning", "tenant_beta"],
            ["08:17", "Route fallback", "gpt-4o-mini"],
            ["07:03", "Latency spike", "rag-service"],
          ]}
        />
        <TableShell
          title="Audit Snapshot"
          columns={["Tenant", "Endpoint", "Status"]}
          rows={[
            ["tenant_alpha", "/v1/chat/completions", "200"],
            ["tenant_beta", "/v1/rag/query", "200"],
            ["tenant_gamma", "/v1/embeddings", "429"],
          ]}
        />
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Add the remaining page shells with stable titles and realistic sections**

```tsx
export function APIKeysPage() {
  return (
    <div className="page-grid">
      <div className="page-actions">
        <button className="button-shell">Create Key</button>
        <button className="button-shell">Rotate Key</button>
        <button className="button-shell">Disable Key</button>
      </div>
      <section className="section-card">
        <h2>API Keys</h2>
        <table className="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Tenant</th>
              <th>Status</th>
              <th>Scope</th>
              <th>Last Used</th>
            </tr>
          </thead>
          <tbody>
            <tr><td>prod-gateway</td><td>tenant_alpha</td><td>Active</td><td>chat, rag</td><td>2m ago</td></tr>
            <tr><td>batch-worker</td><td>tenant_beta</td><td>Active</td><td>embeddings</td><td>14m ago</td></tr>
          </tbody>
        </table>
      </section>
      <section className="section-card">
        <h3>Credential Model</h3>
        <p>Platform API keys stay separate from provider credentials. BYOK is reserved for later phases.</p>
      </section>
    </div>
  );
}
```

```tsx
export function RoutesPage() {
  return (
    <div className="page-grid">
      <div className="stats-grid">
        <section className="section-card"><p className="stat-card__label">Active Providers</p><p className="stat-card__value">4</p></section>
        <section className="section-card"><p className="stat-card__label">Model Mappings</p><p className="stat-card__value">19</p></section>
        <section className="section-card"><p className="stat-card__label">Fallback Policy</p><p className="stat-card__value">Enabled</p></section>
        <section className="section-card"><p className="stat-card__label">Bootstrap Mode</p><p className="stat-card__value">Active</p></section>
      </div>
      <section className="section-card">
        <h2>Routes</h2>
        <table className="data-table">
          <thead>
            <tr>
              <th>Requested Model</th>
              <th>Resolved Provider</th>
              <th>Credential</th>
              <th>Latency</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            <tr><td>gpt-4o-mini</td><td>OpenAI Primary</td><td>provider_qwen_primary</td><td>218 ms</td><td>Healthy</td></tr>
            <tr><td>text-embedding-3-small</td><td>OpenAI Primary</td><td>provider_qwen_primary</td><td>64 ms</td><td>Healthy</td></tr>
            <tr><td>RAG Query</td><td>RAG Service</td><td>rag-service</td><td>312 ms</td><td>Warning</td></tr>
          </tbody>
        </table>
      </section>
      <section className="section-card">
        <h3>Routing Policy</h3>
        <p>Bootstrap Mode: enabled</p>
        <p>Model-first Resolution: active</p>
        <p>Requests resolve to managed credentials before upstream dispatch, then fall back according to route policy.</p>
      </section>
    </div>
  );
}
```

```tsx
export function PlaygroundPage() {
  return (
    <div className="page-grid">
      <div className="two-column-grid">
        <section className="section-card">
          <h2>Playground</h2>
          <p>Model Selector: qwen-plus / text-embedding-v3 / rag-query</p>
          <p>Request Body: chat payload preview with tenant, model, and sampling controls.</p>
          <div className="page-actions">
            <button className="button-shell">Send Routed Request</button>
            <button className="button-shell">Reset Draft</button>
          </div>
        </section>
        <section className="section-card">
          <h3>Last Response</h3>
          <p>Resolved Provider: OpenAI Primary</p>
          <p>Endpoint: /v1/chat/completions</p>
          <p>Latency: 218 ms</p>
          <p>Status: 200 OK</p>
        </section>
      </div>
      <section className="section-card">
        <h3>Execution Meta</h3>
        <p>Platform Key: prod-gateway</p>
        <p>Resolved Provider: OpenAI Primary</p>
        <p>Endpoint: /v1/chat/completions</p>
      </section>
    </div>
  );
}
```

```tsx
export function KnowledgeBasePage() {
  return (
    <div className="page-grid">
      <div className="stats-grid">
        <section className="section-card"><p className="stat-card__label">Documents</p><p className="stat-card__value">184</p></section>
        <section className="section-card"><p className="stat-card__label">Chunks</p><p className="stat-card__value">12.4k</p></section>
        <section className="section-card"><p className="stat-card__label">Last Ingest</p><p className="stat-card__value">8m ago</p></section>
        <section className="section-card"><p className="stat-card__label">Queue Status</p><p className="stat-card__value">Healthy</p></section>
      </div>
      <section className="section-card">
        <h2>Knowledge Base</h2>
        <table className="data-table">
          <thead>
            <tr>
              <th>Knowledge Base</th>
              <th>Documents</th>
              <th>Status</th>
              <th>Updated At</th>
            </tr>
          </thead>
          <tbody>
            <tr><td>Product Docs</td><td>84</td><td>Ready</td><td>09:10</td></tr>
            <tr><td>Support Archive</td><td>62</td><td>Indexing</td><td>08:44</td></tr>
          </tbody>
        </table>
      </section>
      <div className="two-column-grid">
        <section className="section-card">
          <h3>RAG Query Flow</h3>
          <p>Query enters the gateway, resolves to the RAG service, then joins retrieval context before final response assembly.</p>
        </section>
        <section className="section-card">
          <h3>Ingest Queue</h3>
          <p>3 files pending chunk refresh, 1 index rebuild in progress, no failed ingest jobs.</p>
        </section>
      </div>
    </div>
  );
}
```

```tsx
export function AuditPage() {
  return (
    <div className="page-grid">
      <div className="page-actions">
        <button className="button-shell">Endpoint</button>
        <button className="button-shell">Status</button>
        <button className="button-shell">Tenant</button>
      </div>
      <section className="section-card">
        <h2>Audit</h2>
        <table className="data-table">
          <thead>
            <tr>
              <th>Time</th>
              <th>Tenant</th>
              <th>Endpoint</th>
              <th>Status</th>
              <th>Provider</th>
              <th>Latency</th>
            </tr>
          </thead>
          <tbody>
            <tr><td>09:42</td><td>tenant_alpha</td><td>/v1/chat/completions</td><td>200</td><td>OpenAI Primary</td><td>218 ms</td></tr>
            <tr><td>09:39</td><td>tenant_beta</td><td>/v1/rag/query</td><td>200</td><td>RAG Service</td><td>312 ms</td></tr>
          </tbody>
        </table>
      </section>
      <div className="two-column-grid">
        <section className="section-card">
          <h3>Error Summary</h3>
          <p>Quota exceeded and routing fallback events surface here for operational review.</p>
        </section>
        <section className="section-card">
          <h3>Quota Exceeded</h3>
          <p>2 tenant throttles detected in the last hour, both isolated to batch embedding workloads.</p>
        </section>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Expand the router to all six routes and drive the top bar from route metadata**

```tsx
import { NavLink, Outlet, useMatches } from "react-router-dom";

export type ConsoleRouteMeta = {
  title: string;
  description: string;
};

export const navigation = [
  {
    path: "/",
    label: "Overview",
    title: "Overview",
    description: "Monitor gateway health, routing posture, and core platform signals.",
  },
  {
    path: "/api-keys",
    label: "API Keys",
    title: "API Keys",
    description: "Manage platform keys, scopes, and tenant access posture.",
  },
  {
    path: "/routes",
    label: "Routes",
    title: "Routes",
    description: "Inspect model mappings, provider resolution, and fallback behavior.",
  },
  {
    path: "/playground",
    label: "Playground",
    title: "Playground",
    description: "Validate requests against the gateway before production usage.",
  },
  {
    path: "/knowledge-base",
    label: "Knowledge Base",
    title: "Knowledge Base",
    description: "Review document ingestion, chunking, and RAG readiness.",
  },
  {
    path: "/audit",
    label: "Audit",
    title: "Audit",
    description: "Trace request history, provider resolution, and operational events.",
  },
];

export function AppLayout() {
  const matches = useMatches();
  const current = matches[matches.length - 1]?.handle as ConsoleRouteMeta | undefined;

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="sidebar__brand">AI Gateway Console</div>
        <nav className="sidebar__nav">
          {navigation.map((item) => (
            <NavLink
              key={item.path}
              end={item.path === "/"}
              to={item.path}
              className={({ isActive }) =>
                isActive ? "sidebar__link sidebar__link--active" : "sidebar__link"
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar__status">
          <span className="status-badge status-badge--neutral">MVP</span>
          <span className="status-badge status-badge--neutral">Bootstrap Mode</span>
          <span className="status-badge status-badge--healthy">Gateway Healthy</span>
        </div>
      </aside>
      <div className="shell-main">
        <header className="topbar">
          <div className="topbar__meta">
            <h1>{current?.title ?? "Overview"}</h1>
            <p>{current?.description ?? "Monitor gateway health, routing posture, and core platform signals."}</p>
          </div>
          <div className="topbar__badges">
            <span className="status-badge status-badge--healthy">Gateway Healthy</span>
            <span className="status-badge status-badge--neutral">Quota Guard Active</span>
          </div>
        </header>
        <main className="page-content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
```

```tsx
import { createBrowserRouter, createMemoryRouter } from "react-router-dom";

import { APIKeysPage } from "../pages/api-keys";
import { AuditPage } from "../pages/audit";
import { DashboardPage } from "../pages/dashboard";
import { KnowledgeBasePage } from "../pages/knowledge-base";
import { PlaygroundPage } from "../pages/playground";
import { RoutesPage } from "../pages/routes";
import { AppLayout, navigation, type ConsoleRouteMeta } from "./layout";

const routes = [
  {
    path: "/",
    element: <AppLayout />,
    children: [
      { index: true, element: <DashboardPage />, handle: navigation[0] satisfies ConsoleRouteMeta },
      { path: "api-keys", element: <APIKeysPage />, handle: navigation[1] satisfies ConsoleRouteMeta },
      { path: "routes", element: <RoutesPage />, handle: navigation[2] satisfies ConsoleRouteMeta },
      { path: "playground", element: <PlaygroundPage />, handle: navigation[3] satisfies ConsoleRouteMeta },
      { path: "knowledge-base", element: <KnowledgeBasePage />, handle: navigation[4] satisfies ConsoleRouteMeta },
      { path: "audit", element: <AuditPage />, handle: navigation[5] satisfies ConsoleRouteMeta },
    ],
  },
];

export function createAppRouter() {
  return createBrowserRouter(routes);
}

export function createTestRouter(initialEntries: string[] = ["/"]) {
  return createMemoryRouter(routes, { initialEntries });
}
```

- [ ] **Step 4: Re-run the route smoke test**

Run: `cd $PROJECT_ROOT/web && npm test -- --runInBand`
Expected: PASS

## Task 4: Final Shell Polish and Commit

**Files:**
- Modify: `web/src/styles.css`
- Modify: `web/src/test/router.test.tsx`
- Test: `web/src/test/router.test.tsx`

- [ ] **Step 1: Extend the smoke test to prove one secondary route renders correctly**

```tsx
import { render, screen } from "@testing-library/react";
import { RouterProvider } from "react-router-dom";

import { createTestRouter } from "../app/router";

test("renders dashboard route", async () => {
  render(<RouterProvider router={createTestRouter()} />);
  expect(await screen.findByText("Overview")).toBeInTheDocument();
});

test("renders routes page", async () => {
  render(<RouterProvider router={createTestRouter(["/routes"])} />);
  expect(await screen.findByText("Routes")).toBeInTheDocument();
  expect(await screen.findByText("Routing Policy")).toBeInTheDocument();
});
```

- [ ] **Step 2: Tighten the shell spacing for table-heavy pages if needed**

If the first pass feels too sparse or too cramped, keep the changes local to `styles.css`. Use this exact adjustment block if needed:

```css
.section-card p {
  margin: 0;
}

.page-grid > .section-card + .section-card {
  margin-top: 0;
}

.page-content .section-card p + p {
  margin-top: 8px;
}
```

- [ ] **Step 3: Run the web test suite again**

Run: `cd $PROJECT_ROOT/web && npm test -- --runInBand`
Expected: PASS with both router tests green

- [ ] **Step 4: Commit the Task 8 implementation**

```bash
git -C $PROJECT_ROOT add web
git -C $PROJECT_ROOT commit -m "feat: add console shell and primary routes"
```
