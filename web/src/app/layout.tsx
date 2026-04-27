import { NavLink, Outlet, useMatches } from "react-router-dom";

import { getSystemStatus } from "../lib/console-api";
import type { ConsoleSession } from "../lib/session";
import { useConsoleSession } from "../lib/session";
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

export function AppLayout({
  navigation,
  session,
}: {
  navigation: readonly ConsoleNavigationItem[];
  session?: ConsoleSession;
}) {
  const matches = useMatches();
  const resolvedSession = session ?? useConsoleSession();
  const isAdminConsole = resolvedSession.role === "admin";
  const { data: systemStatus, error: systemStatusError } = useRemoteData(
    () => (isAdminConsole ? getSystemStatus() : Promise.resolve(null)),
    [isAdminConsole],
  );
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
        <div className="sidebar__brand">
          {resolvedSession.role === "admin" ? "AI 接入平台" : "租户控制台"}
        </div>
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
          {isAdminConsole ? (
            <div className="topbar__badges">
              <span className={getBadgeClassName(isGatewayHealthy)}>{gatewayHealth}</span>
              <span className="status-badge status-badge--neutral">配额保护 {quotaProtection}</span>
            </div>
          ) : null}
        </header>
        <main className="page-content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
