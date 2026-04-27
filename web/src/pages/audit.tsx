import { useCallback } from "react";

import { DataTable, ErrorSection, LoadingSection, StatCard } from "../components/console";
import { getAudit, getMemberAuditEvents } from "../lib/console-api";
import { neutralizeLineLabel, neutralizePlatformNarrative } from "../lib/platform-routing";
import { useConsoleSession } from "../lib/session";
import { useRemoteData } from "../lib/use-remote-data";

export function AuditPage() {
  const session = useConsoleSession();
  const isAdmin = session.role === "admin";
  const loadAudit = useCallback(
    () => (isAdmin ? getAudit() : getMemberAuditEvents()),
    [isAdmin],
  );
  const { data, loading, error } = useRemoteData(loadAudit);

  if (loading) {
    return <LoadingSection text="正在加载审计日志..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "审计数据加载失败。"} />;
  }

  if (!isAdmin) {
    return (
      <div className="page-grid">
        <section className="section-card">
          <h2>成员审计事件</h2>
          <DataTable
            columns={["时间", "事件类型", "目标类型", "目标 ID", "详情"]}
            rows={data.items.map((item) => [
              item.time,
              item.event_type,
              item.target_type,
              item.target_id,
              item.detail,
            ])}
          />
        </section>
      </div>
    );
  }

  const metrics = data.metrics ?? [];
  const items = data.items ?? [];
  const events = data.events ?? [];
  const summaries = data.summaries ?? [];

  return (
    <div className="page-grid">
      <div className="stats-grid stats-grid--four">
        {metrics.map((metric) => (
          <StatCard key={metric.label} label={metric.label} value={metric.value} />
        ))}
      </div>
      <section className="section-card">
        <h2>审计明细</h2>
        <DataTable
          columns={["时间", "租户", "端点", "请求模型", "上游模型", "状态", "平台线路", "延迟", "计量来源"]}
          rows={items.map((item) => [
            item.time,
            item.tenant,
            item.endpoint,
            item.request_model,
            item.upstream_model,
            item.status,
            neutralizeLineLabel(item.route_label),
            item.latency,
            item.usage_source,
          ])}
        />
      </section>
      <div className="two-column-grid">
        <section className="section-card">
          <h2>最近事件流</h2>
          {events.length > 0 ? (
            <ul className="event-timeline">
              {events.map((event) => (
                <li key={`${event.time}-${event.type}`}>
                  <strong>{event.time}</strong>
                  <p>{neutralizePlatformNarrative(event.status)}</p>
                  <p>{neutralizePlatformNarrative(event.detail)}</p>
                </li>
              ))}
            </ul>
          ) : (
            <p>暂无事件</p>
          )}
        </section>
        <div className="page-grid">
          {summaries.map((summary) => (
            <section key={summary.title} className="section-card">
              <h3>{summary.title}</h3>
              <p>{neutralizePlatformNarrative(summary.content)}</p>
            </section>
          ))}
        </div>
      </div>
    </div>
  );
}
