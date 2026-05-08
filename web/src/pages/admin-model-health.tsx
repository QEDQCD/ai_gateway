import { useCallback, useState } from "react";

import { DataTable, ErrorSection, LoadingSection, StatCard } from "../components/console";
import { getModelHealth } from "../lib/console-api";
import { neutralizeLineLabel } from "../lib/platform-routing";
import { useRemoteData } from "../lib/use-remote-data";

const wallWindows = [
  { key: "6h", label: "最近 6 小时" },
  { key: "24h", label: "最近 24 小时" },
  { key: "7d", label: "最近 7 天" },
] as const;

const wallLegend = [
  { label: "健康", tone: "success" as const },
  { label: "告警", tone: "warning" as const },
  { label: "降级", tone: "warning" as const },
  { label: "空窗", tone: "neutral" as const },
];

type WallWindow = (typeof wallWindows)[number]["key"];

function averageLatency(values: number[]) {
  if (values.length === 0) {
    return "0 ms";
  }

  const total = values.reduce((sum, value) => sum + value, 0);
  return `${Math.round(total / values.length)} ms`;
}

function toneForHealthStatus(status: string) {
  if (status === "健康") {
    return "success";
  }
  if (status === "告警") {
    return "warning";
  }
  if (status === "降级") {
    return "warning";
  }
  return "neutral";
}

export function AdminModelHealthPage() {
  const [wallWindow, setWallWindow] = useState<WallWindow>("24h");
  const loadModelHealth = useCallback(() => getModelHealth(wallWindow), [wallWindow]);
  const { data, loading, error } = useRemoteData(loadModelHealth, [loadModelHealth]);

  if (loading) {
    return <LoadingSection text="正在加载模型健康状态..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "模型健康数据加载失败。"} />;
  }

  const healthyItems = data.items.filter((item) => item.health_status === "healthy");
  const nonHealthyItems = data.items.filter((item) => item.health_status !== "healthy");
  const latencySamples = data.items.map((item) => item.latency_ms).filter((value) => value > 0);

  return (
    <div className="page-grid">
      <div className="stats-grid">
        <StatCard label="模型总数" value={String(data.items.length)} />
        <StatCard label="健康模型" value={String(healthyItems.length)} />
        <StatCard label="异常模型" value={String(nonHealthyItems.length)} />
        <StatCard label="平均延迟" value={averageLatency(latencySamples)} />
      </div>

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>模型健康墙</h2>
            <p>按模型观察 {data.wall.window_label || "最近 24 小时"} 的健康状态与探活延迟。</p>
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
        {data.wall.lanes.length > 0 ? (
          <div className="usage-wall__board">
            <div className="usage-wall__header">
              <div>模型 / 平台线路</div>
              <div className="usage-wall__bucket-row">
                {data.wall.buckets.map((bucket) => (
                  <span key={bucket}>{bucket}</span>
                ))}
              </div>
            </div>
            {data.wall.lanes.map((lane) => (
              <div key={`${lane.model}-${lane.route_label}`} className="usage-wall__lane">
                <div className="usage-wall__meta">
                  <strong>{lane.model}</strong>
                  <span>{lane.provider || neutralizeLineLabel(lane.route_label)}</span>
                  <span>通过率 {lane.success_rate}</span>
                  <span>平均延迟 {lane.average_latency}</span>
                </div>
                <div className="usage-wall__bucket-row">
                  {lane.cells.map((cell) => (
                    <article
                      key={`${lane.model}-${lane.route_label}-${cell.bucket_label}`}
                      className={`usage-wall__cell usage-wall__cell--${toneForHealthStatus(cell.status)}`}
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
          <p>当前时间范围内暂无健康检查历史数据。</p>
        )}
      </section>

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>健康摘要</h2>
            <p>基于专用 model-health 接口生成的最小只读健康视图。</p>
          </div>
        </div>
        <DataTable
          columns={["状态", "数量", "说明"]}
          rows={[
            ["healthy", String(healthyItems.length), "健康检查通过或当前状态正常"],
            ["non-healthy", String(nonHealthyItems.length), "包含 warning、degraded 或空状态"],
          ]}
        />
      </section>

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>模型健康列表</h2>
            <p>按模型展示当前健康状态、线路与延迟。</p>
          </div>
        </div>
        <DataTable
          columns={["请求模型", "线路", "健康状态", "延迟", "首 Token", "最近检查", "错误", "模式"]}
          rows={data.items.map((item) => [
            item.requested_model,
            item.route_label,
            item.health_status || "-",
            item.latency_ms > 0 ? `${item.latency_ms} ms` : "-",
            item.first_token_latency_ms > 0 ? `${item.first_token_latency_ms} ms` : "-",
            item.last_health_checked_at || "-",
            item.last_health_error || "-",
            item.request_mode || "-",
          ])}
        />
      </section>
    </div>
  );
}
