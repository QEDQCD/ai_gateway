import { NavLink, Outlet, useMatches } from "react-router-dom";

export type ConsoleRouteMeta = {
  title: string;
  description: string;
};

export type ConsoleNavigationItem = ConsoleRouteMeta & {
  path: string;
  label: string;
};

function isConsoleRouteMeta(handle: unknown): handle is ConsoleRouteMeta {
  if (!handle || typeof handle !== "object") {
    return false;
  }

  return typeof handle.title === "string" && typeof handle.description === "string";
}

function toRouteMeta(navigationItem: ConsoleNavigationItem): ConsoleRouteMeta {
  return {
    title: navigationItem.title,
    description: navigationItem.description,
  };
}

export function AppLayout({ navigation }: { navigation: readonly ConsoleNavigationItem[] }) {
  const matches = useMatches();
  const current =
    matches.reduce<ConsoleRouteMeta | undefined>(
      (matchedMeta, match) => (isConsoleRouteMeta(match.handle) ? match.handle : matchedMeta),
      undefined,
    ) ?? toRouteMeta(navigation[0]);

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
