import { useCallback, useState } from "react";

import {
  DataTable,
  ErrorSection,
  LoadingSection,
  MetricSeriesSection,
  SourcePill,
  StatCard,
  StatusPill,
} from "../components/console";
import {
  getUsageFailures,
  getUsageLatencyWall,
  getUsageOverview,
  getUsageRequests,
  getUsageTrends,
} from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

const PAGE_SIZE = 20;
const wallWindows = [
  { key: "6h", label: "最近 6 小时" },
  { key: "24h", label: "最近 24 小时" },
  { key: "7d", label: "最近 7 天" },
] as const;
const wallLegend = [
  { label: "健康", tone: "success" as const },
  { label: "失败", tone: "danger" as const },
  { label: "空窗", tone: "neutral" as const },
];

type WallWindow = (typeof wallWindows)[number]["key"];

function toneForStatus(status: string): "success" | "warning" | "danger" | "neutral" {
  if (status === "成功") {
    return "success";
  }
  if (status === "限流") {
    return "warning";
  }
  if (status.includes("失败") || status.includes("异常")) {
    return "danger";
  }
  return "neutral";
}

function toCount(value: string) {
  const matched = value.match(/\d+/);
  return matched ? Number(matched[0]) : 0;
}

function latencyTone(status: string) {
  if (status === "失败") {
    return "danger";
  }
  if (status === "健康") {
    return "success";
  }
  return "neutral";
}

export function UsagePage() {
  const [offset, setOffset] = useState(0);
  const [wallWindow, setWallWindow] = useState<WallWindow>("24h");

  const loadRequests = useCallback(
    () =>
      getUsageRequests({
        limit: PAGE_SIZE,
        offset,
      }),
    [offset],
  );
  const loadLatencyWall = useCallback(() => getUsageLatencyWall(wallWindow), [wallWindow]);

  const overview = useRemoteData(getUsageOverview);
  const trends = useRemoteData(getUsageTrends);
  const latencyWall = useRemoteData(loadLatencyWall);
  const failures = useRemoteData(getUsageFailures);
  const requests = useRemoteData(loadRequests);

  const error =
    overview.error ??
    trends.error ??
    latencyWall.error ??
    failures.error ??
    requests.error;

  if (error) {
    return <ErrorSection message={error} />;
  }

  if (!overview.data || !trends.data || !latencyWall.data || !failures.data || !requests.data) {
    return <LoadingSection text="正在加载调用观测数据..." />;
  }

  const currentPage = Math.floor(requests.data.offset / requests.data.limit) + 1;
  const totalPages = Math.max(1, Math.ceil(requests.data.total / requests.data.limit));
  const hasPreviousPage = requests.data.offset > 0;
  const hasNextPage = requests.data.offset + requests.data.limit < requests.data.total;
  const strongestFailure = Math.max(1, ...failures.data.breakdown.map((item) => toCount(item.value)));
  const visibleWallLabel =
    wallWindows.find((item) => item.key === wallWindow)?.label ?? "最近 24 小时";

  return (
    <div className="page-grid page-grid--usage">
      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>实时运行视图</h2>
            <p>请求量、Token、成功率与估算占比全部来自 usage 接口。</p>
          </div>
        </div>
        <div className="stats-grid stats-grid--five">
          <StatCard label="总调用数" value={String(overview.data.total_requests)} />
          <StatCard label="成功率" value={overview.data.success_rate} />
          <StatCard label="总 Token" value={overview.data.total_tokens} />
          <StatCard label="平均延迟" value={overview.data.average_latency} />
          <StatCard label="估算占比" value={overview.data.estimated_share} />
        </div>
      </section>

      <section className="section-card usage-wall">
        <div className="section-card__header">
          <div>
            <h2>模型延时健康墙</h2>
            <p>按模型观察 {latencyWall.data.window_label || visibleWallLabel} 的响应延时与失败分布。</p>
          </div>
          <div className="usage-wall__toolbar">
            {wallWindows.map((item) => (
              <button
                key={item.key}
                type="button"
                className={`button-shell ${wallWindow === item.key ? "button-shell--primary" : ""}`}
                onClick={() => setWallWindow(item.key)}
              >
                {item.label}
              </button>
            ))}
          </div>
        </div>
        <div className="usage-wall__legend">
          {wallLegend.map((item) => (
            <span key={item.label} className={`status-pill status-pill--${item.tone}`}>
              {item.label}
            </span>
          ))}
        </div>
        {latencyWall.data.lanes.length > 0 ? (
          <div className="usage-wall__board">
            <div className="usage-wall__header">
              <div>模型 / 供应商</div>
              <div className="usage-wall__bucket-row">
                {latencyWall.data.buckets.map((bucket) => (
                  <span key={bucket}>{bucket}</span>
                ))}
              </div>
            </div>
            {latencyWall.data.lanes.map((lane) => (
              <div key={`${lane.model}-${lane.provider}`} className="usage-wall__lane">
                <div className="usage-wall__meta">
                  <strong>{lane.model}</strong>
                  <span>{lane.provider}</span>
                  <span>成功率 {lane.success_rate}</span>
                  <span>平均延迟 {lane.average_latency}</span>
                </div>
                <div className="usage-wall__bucket-row">
                  {lane.cells.map((cell) => (
                    <article
                      key={`${lane.model}-${cell.bucket_label}`}
                      className={`usage-wall__cell usage-wall__cell--${latencyTone(cell.status)}`}
                    >
                      <strong>{cell.latency}</strong>
                      <span>{cell.status}</span>
                      <small>{cell.requests}</small>
                    </article>
                  ))}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p>当前时间范围内暂无真实延时数据。</p>
        )}
      </section>

      <section className="section-card">
        <h2>趋势概览</h2>
        <div className="three-column-grid">
          <MetricSeriesSection title="调用次数趋势" points={trends.data.requests} />
          <MetricSeriesSection title="Token 趋势" points={trends.data.tokens} />
          <MetricSeriesSection title="成功率趋势" points={trends.data.success} />
        </div>
      </section>

      <div className="two-column-grid">
        <section className="section-card">
          <h2>失败分类强弱条</h2>
          <ul className="meter-list">
            {failures.data.breakdown.map((item) => (
              <li key={item.label} className="meter-list__item">
                <div className="meter-list__meta">
                  <span>{item.label}</span>
                  <strong>{item.value}</strong>
                </div>
                <div className="meter-list__track">
                  <span
                    className="meter-list__fill"
                    style={{ width: `${(toCount(item.value) / strongestFailure) * 100}%` }}
                  />
                </div>
              </li>
            ))}
          </ul>
        </section>
        <section className="section-card">
          <h2>异常事件流</h2>
          {failures.data.recent_events.length > 0 ? (
            <ul className="event-timeline">
              {failures.data.recent_events.map((event) => (
                <li key={event}>{event}</li>
              ))}
            </ul>
          ) : (
            <p>暂无异常事件</p>
          )}
        </section>
      </div>

      <section className="section-card">
        <div className="section-card__header">
          <h2>调用明细</h2>
          <div className="usage-pagination">
            <p>
              共 {requests.data.total} 条，当前第 {currentPage} / {totalPages} 页，展示{" "}
              {requests.data.items.length} 条
            </p>
            <div className="page-actions">
              <button
                className="button-shell"
                disabled={!hasPreviousPage || requests.loading}
                onClick={() => setOffset((currentOffset) => Math.max(0, currentOffset - PAGE_SIZE))}
                type="button"
              >
                上一页
              </button>
              <button
                className="button-shell"
                disabled={!hasNextPage || requests.loading}
                onClick={() => setOffset((currentOffset) => currentOffset + PAGE_SIZE)}
                type="button"
              >
                下一页
              </button>
            </div>
          </div>
        </div>
        <DataTable
          columns={["请求 ID", "租户", "端点", "模型", "状态", "总 Token", "延迟", "计量来源"]}
          rows={requests.data.items.map((item) => [
            item.request_id,
            item.tenant,
            item.endpoint,
            item.model,
            <StatusPill
              key={`${item.request_id}-status`}
              label={item.status}
              tone={toneForStatus(item.status)}
            />,
            item.total_tokens,
            item.latency,
            <SourcePill key={`${item.request_id}-source`} label={item.usage_source} />,
          ])}
        />
      </section>
    </div>
  );
}
