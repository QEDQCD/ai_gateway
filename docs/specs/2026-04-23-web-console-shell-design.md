# Web Console Shell Design

## Context

This spec covers Task 8 of the AI Gateway MVP plan: building the React console shell and the primary routes for the web application.

The current repository already has:

- A Go gateway with health, auth, chat, embeddings, and RAG proxy paths
- A Python RAG service with query and ingest APIs
- A `web/` workspace that only contains a minimal `package.json`

The goal of this task is not to connect real data yet. The goal is to produce a console shell that already looks and feels like a real enterprise AI Gateway control plane, so that Task 9 can attach data flows onto stable page structures without reworking layout, routing, or page hierarchy.

## Product Direction

The user selected the following direction during brainstorming:

- Visual style: stable enterprise admin console
- Navigation model: fixed left sidebar plus a simple top bar
- Overview page: hybrid dashboard with business metrics on top and routing/alerts below
- Page maturity: close to a real product shell, but still static for now
- Content tone: mixed; titles are product-oriented while labels and modules remain engineering-oriented

This means the console should look operational and professional, not like a marketing site and not like a throwaway demo with only placeholder titles.

## Scope

This task includes:

- The web app bootstrap files
- The application layout shell
- The primary route tree
- Six routed pages:
  - Overview
  - API Keys
  - Routes
  - Playground
  - Knowledge Base
  - Audit
- A route smoke test

This task explicitly does not include:

- Real backend API integration
- Authentication for the web UI
- Complex interactions, forms, filters, or persistence
- Charts that require a data layer
- Task 9 data fetching logic

## Information Architecture

The route tree is:

- `/` -> `Overview`
- `/api-keys` -> `API Keys`
- `/routes` -> `Routes`
- `/playground` -> `Playground`
- `/knowledge-base` -> `Knowledge Base`
- `/audit` -> `Audit`

The route tree should be implemented in one place and used as the source of truth for navigation rendering.

## Layout

The application uses a two-part shell:

### Sidebar

The sidebar is fixed on the left and contains:

- Product label: `AI Gateway Console`
- Primary navigation links for all six pages
- A compact environment/status block near the bottom, such as:
  - `MVP`
  - `Bootstrap Mode`
  - `Gateway Healthy`

The sidebar should feel stable and operational. It is the structural anchor of the app.

### Top Bar

The top bar is lightweight and sits above the main content area. It contains:

- The current page title
- A one-line page description
- Two compact global state badges on the right, for example:
  - `Gateway Healthy`
  - `Quota Guard Active`

The top bar should not become a second navigation system. It exists to frame the current page and reinforce system status.

### Main Content

Each page uses a consistent content rhythm:

- Page header
- Primary content cards and tables
- Secondary context cards or side panels

The result should feel like a real control plane that is ready to receive live data.

## Visual System

The UI should follow an enterprise admin style:

- Background: light neutral gray or off-white
- Content cards: white
- Primary palette: navy-gray / slate / graphite
- Status colors:
  - healthy -> green
  - warning -> amber
  - error -> red
- Borders and shadows: restrained
- Typography: clean and readable, with clear hierarchy

The interface should avoid:

- flashy gradients or decorative marketing visuals
- playful styles that weaken the control-plane feel
- overly sparse “empty demo” layouts

The UI should still feel intentional. Even though it is a shell, it must look closer to a shipping SaaS admin than to a scaffold.

## Page Designs

### Overview

The Overview page is the most important shell page. It should communicate both business state and gateway capability.

Sections:

1. Top metric row with four stat cards:
   - `Requests 24h`
   - `Success Rate`
   - `Quota Usage`
   - `Active API Keys`
2. Middle left: `Route Health`
   - route rows such as requested model, resolved provider, latency, success rate
3. Middle right: `Top Models`
   - highlight popular or important model paths such as chat, embeddings, or RAG
4. Bottom left: `Recent Alerts`
   - recent operational issues, quota alerts, or degraded routes
5. Bottom right: `Audit Snapshot`
   - a short list of recent request or audit entries

This page should make the system look like an AI Gateway, not a generic analytics dashboard.

### API Keys

This page focuses on platform API key management.

Sections:

- Top action area with placeholder buttons:
  - `Create Key`
  - `Rotate Key`
  - `Disable Key`
- Primary table:
  - Name
  - Tenant
  - Status
  - Scope
  - Last Used
- Secondary info card:
  - explain platform API keys vs provider credentials
  - mention BYOK is reserved for future phases

### Routes

This is the page that most clearly expresses gateway and routing ability. It should be the strongest page after Overview.

Sections:

- Top status cards:
  - `Active Providers`
  - `Model Mappings`
  - `Fallback Policy`
- Primary table:
  - Requested Model
  - Resolved Provider
  - Credential
  - Latency
  - Status
- Secondary policy panel:
  - `Routing Policy`
  - `Bootstrap Mode`
  - `Model-first Resolution`

This page must clearly communicate that the product is performing API gateway routing, not just proxying raw calls.

### Playground

This page should look like a realistic request/response workbench.

Sections:

- Left pane:
  - model selector area
  - request body/editor placeholder
  - send action area
- Right pane:
  - response panel
  - latency
  - resolved provider
  - endpoint summary
- Footer/meta section:
  - Platform Key
  - Resolved Provider
  - Endpoint

No real submission logic is implemented in this task, but the layout should be obviously ready for Task 9.

### Knowledge Base

This page represents the RAG side of the platform.

Sections:

- Top stat cards:
  - `Documents`
  - `Chunks`
  - `Last Ingest`
- Primary table:
  - Knowledge Base
  - Documents
  - Status
  - Updated At
- Secondary side content:
  - `RAG Query Flow`
  - `Ingest Queue`

### Audit

This page emphasizes traceability and operational history.

Sections:

- Top filter bar placeholders:
  - Endpoint
  - Status
  - Tenant
- Primary table or timeline:
  - Time
  - Tenant
  - Endpoint
  - Status
  - Provider
  - Latency
- Secondary summary area:
  - `Error Summary`
  - `Quota Exceeded`

## Component Structure

Task 8 should keep reuse lightweight. It should not turn into a full design system.

Recommended shared pieces:

- `PageHeader`
- `StatCard`
- `SectionCard`
- `StatusBadge`
- `DataTableShell`

Pages remain responsible for composition and content layout. Shared components only cover repeated visual shells.

## Implementation Constraints

- Build the shell close to a real product, but keep data static
- Do not implement Task 9 API requests yet
- Keep files focused:
  - layout and routing in `app/`
  - page composition in `pages/`
  - test coverage in `src/test/`
- Preserve established structure if a framework choice requires supporting files, but do not overbuild tooling beyond what Task 8 needs

## Testing

The required test is a routing smoke test.

It must at minimum prove:

- the router can render the default route
- the Overview page renders stable text `Overview`

The test scope should stay intentionally small:

- no complicated interaction testing
- no network mocking
- no visual regression testing

The purpose of testing in this task is to confirm that the shell and route tree exist and render correctly.

## Success Criteria

Task 8 is complete when:

- the web app has a stable shell with sidebar and top bar
- all six primary routes exist
- each page renders a stable, realistic structure
- the Overview page looks like a gateway operations dashboard
- the Routes page clearly communicates routing and model resolution ability
- the route smoke test passes
- the result is visually credible as an enterprise AI Gateway console

## Risks And Guardrails

Main risks:

- building pages that are too empty, making the shell look fake
- overengineering a component system before data exists
- drifting into Task 9 by adding API plumbing too early
- producing a generic admin template that does not emphasize gateway behavior

Guardrails:

- keep shell fidelity high
- keep logic fidelity low
- make `Routes` and `Overview` the two most expressive pages
- reserve real data flow for Task 9
