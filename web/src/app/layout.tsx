import { NavLink, Outlet, useMatches } from "react-router-dom";

import { getSystemStatus } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

export type ConsoleRouteMeta = {
  title: string;
  description: string;
};

export type ConsoleNavigationItem = ConsoleRouteMeta & {
  path: string;
  label: string;
};
const fallbackRouteMeta: ConsoleRouteMeta = {
  title: "控制台",
  description: "请选择左侧导航以查看对应页面。",
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

function getBadgeClassName(isHealthy: boolean) {
  return isHealthy ? "status-badge status-badge--healthy" : "status-badge status-badge--neutral";
}

export function AppLayout({ navigation }: { navigation: readonly ConsoleNavigationItem[] }) {
  const matches = useMatches();
  const { data: systemStatus, error: systemStatusError } = useRemoteData(getSystemStatus);
  const firstNavigationMeta = navigation[0] ? toRouteMeta(navigation[0]) : fallbackRouteMeta;
  const current =
    matches.reduce<ConsoleRouteMeta | undefined>(
      (matchedMeta, match) => (isConsoleRouteMeta(match.handle) ? match.handle : matchedMeta),
      undefined,
    ) ?? firstNavigationMeta;
  const statusPlaceholder = systemStatusError ? "状态获取失败" : "状态加载中";
  const gatewayHealth = systemStatus?.gateway_health ?? statusPlaceholder;
  const quotaProtection = systemStatus?.quota_protection ?? statusPlaceholder;
  const isGatewayHealthy = gatewayHealth === "健康";

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="sidebar__brand">AI 网关控制台</div>
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
      </aside>
      <div className="shell-main">
        <header className="topbar">
          <div className="topbar__meta">
            <h1>{current.title}</h1>
            <p>{current.description}</p>
          </div>
          <div className="topbar__badges">
            <span className={getBadgeClassName(isGatewayHealthy)}>{gatewayHealth}</span>
            <span className="status-badge status-badge--neutral">配额保护 {quotaProtection}</span>
          </div>
        </header>
        <main className="page-content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
