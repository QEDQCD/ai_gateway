import { useCallback, useState } from "react";

import {
  DataTable,
  DetailList,
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
  getUsageRequestDetail,
  getUsageRequests,
  getUsageTrends,
  type UsageRequestDetail,
} from "../lib/console-api";
import { neutralizeLineLabel, neutralizePlatformNarrative } from "../lib/platform-routing";
import {
  formatRoutingReasonLabel,
  formatTargetModelTierLabel,
  formatTaskClassLabel,
} from "../lib/smart-routing";
import { useRemoteData } from "../lib/use-remote-data";

const PAGE_SIZE = 20;
const wallWindows = [
  { key: "6h", label: "最近 6 小时" },
  { key: "24h", label: "最近 24 小时" },
  { key: "7d", label: "最近 7 天" },
] as const;
const wallLegend = [
  { label: "健康", tone: "success" as const },
  { label: "降级", tone: "warning" as const },
  { label: "失败", tone: "danger" as const },
  { label: "空窗", tone: "neutral" as const },
];
const requestStatusOptions = [
  { value: "", label: "全部状态" },
  { value: "success", label: "成功" },
  { value: "rate_limited", label: "限流" },
  { value: "failed", label: "失败" },
  { value: "timeout", label: "超时" },
  { value: "auth_failed", label: "鉴权失败" },
  { value: "upstream_error", label: "上游错误" },
] as const;

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
  if (status === "降级") {
    return "warning";
  }
  if (status === "健康") {
    return "success";
  }
  return "neutral";
}

function formatValue(value: string) {
  return value || "--";
}

function formatTokenCost(tokens: string, cost: string) {
  return `${formatValue(tokens)} / ${formatValue(cost)}`;
}

function formatTenantLabel(tenantName: string, tenantID: string) {
  if (!tenantName && !tenantID) {
    return "--";
  }
  if (!tenantName || tenantName === tenantID) {
    return tenantID || tenantName;
  }
  return `${tenantName} · ${tenantID}`;
}

function formatFailureStatusCode(statusCode: number) {
  return statusCode > 0 ? `（${statusCode}）` : "";
}

function shouldShowLatencyLane(model: string) {
  const normalizedModel = model.trim().toLowerCase();
  return !normalizedModel.startsWith("text-embedding");
}

export function UsagePage() {
  const [offset, setOffset] = useState(0);
  const [wallWindow, setWallWindow] = useState<WallWindow>("7d");
  const [tenantFilter, setTenantFilter] = useState("");
  const [resolvedModelFilter, setResolvedModelFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [selectedRequestID, setSelectedRequestID] = useState("");
  const [requestDetail, setRequestDetail] = useState<UsageRequestDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);

  const loadRequests = useCallback(
    () =>
      getUsageRequests({
        limit: PAGE_SIZE,
        offset,
        window: wallWindow,
        tenant_id: tenantFilter || undefined,
        resolved_model: resolvedModelFilter || undefined,
        status: statusFilter || undefined,
      }),
    [offset, resolvedModelFilter, statusFilter, tenantFilter, wallWindow],
  );
  const loadLatencyWall = useCallback(() => getUsageLatencyWall(wallWindow), [wallWindow]);
  const loadOverview = useCallback(() => getUsageOverview(wallWindow), [wallWindow]);
  const loadTrends = useCallback(() => getUsageTrends(wallWindow), [wallWindow]);
  const loadFailures = useCallback(() => getUsageFailures(wallWindow), [wallWindow]);

  const overview = useRemoteData(loadOverview);
  const trends = useRemoteData(loadTrends);
  const latencyWall = useRemoteData(loadLatencyWall);
  const failures = useRemoteData(loadFailures);
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
    wallWindows.find((item) => item.key === wallWindow)?.label ?? "最近 7 天";
  const visibleLatencyLanes = latencyWall.data.lanes.filter((lane) => shouldShowLatencyLane(lane.model));
  const tenantOptions = Array.from(
    requests.data.items.reduce((options, item) => {
      if (item.tenant_id) {
        options.set(item.tenant_id, formatTenantLabel(item.tenant_name, item.tenant_id));
      }
      return options;
    }, new Map<string, string>()).entries(),
  ).map(([value, label]) => ({ value, label }));
  const resolvedModelOptions = requests.data.resolved_model_options;

  async function openRequestDetail(requestID: string) {
    if (!requestID) {
      return;
    }
    setSelectedRequestID(requestID);
    setDetailLoading(true);
    setDetailError(null);
    try {
      const detail = await getUsageRequestDetail(requestID);
      setRequestDetail(detail);
    } catch (error) {
      setRequestDetail(null);
      setDetailError(error instanceof Error ? error.message : "请求详情加载失败。");
    } finally {
      setDetailLoading(false);
    }
  }

  function closeRequestDetail() {
    setSelectedRequestID("");
    setRequestDetail(null);
    setDetailLoading(false);
    setDetailError(null);
  }

  return (
    <div className="page-grid page-grid--usage">
      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>实时运行视图</h2>
            <p>请求量、Token、费用、成功率与估算占比全部来自 usage 接口，当前范围：{visibleWallLabel}。</p>
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
        <div className="stats-grid">
          <StatCard label="总调用数" value={String(overview.data.total_requests)} />
          <StatCard label="成功率" value={overview.data.success_rate} />
          <StatCard label="平均延迟" value={overview.data.average_latency} />
          <StatCard label="估算占比" value={overview.data.estimated_share} />
          <StatCard
            label="输入 Token / 费用"
            value={formatTokenCost(overview.data.input_tokens, overview.data.input_cost)}
          />
          <StatCard
            label="输出 Token / 费用"
            value={formatTokenCost(overview.data.output_tokens, overview.data.output_cost)}
          />
          <StatCard
            label="缓存 Token / 费用"
            value={formatTokenCost(overview.data.cached_tokens, overview.data.cached_cost)}
          />
          <StatCard label="总 Token" value={formatValue(overview.data.total_tokens)} />
          <StatCard label="总费用" value={formatValue(overview.data.total_cost)} />
        </div>
      </section>

      {overview.data.pricing_models.length > 0 ? (
        <section className="section-card">
          <div className="section-card__header">
            <div>
              <h2>价格口径</h2>
              <p>展示当前窗口内出现过的模型计费单价。</p>
            </div>
          </div>
          <DataTable
            columns={["模型", "输入单价", "输出单价", "缓存单价"]}
            rows={overview.data.pricing_models.map((item) => [
              item.model,
              formatValue(item.input_price),
              formatValue(item.output_price),
              formatValue(item.cached_price),
            ])}
          />
        </section>
      ) : null}

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
        {visibleLatencyLanes.length > 0 ? (
          <div className="usage-wall__board">
            <div className="usage-wall__header">
              <div>模型 / 平台线路</div>
              <div className="usage-wall__bucket-row">
                {latencyWall.data.buckets.map((bucket) => (
                  <span key={bucket}>{bucket}</span>
                ))}
              </div>
            </div>
            {visibleLatencyLanes.map((lane) => (
              <div
                key={`${lane.model}-${lane.provider || lane.route_label}-${lane.source || "真实调用"}`}
                className="usage-wall__lane"
              >
                <div className="usage-wall__meta">
                  <strong>{lane.model}</strong>
                  <span>{lane.provider || neutralizeLineLabel(lane.route_label)}</span>
                  <span>{lane.source || "真实调用"}</span>
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
        <div className="stats-grid">
          <MetricSeriesSection title="调用次数趋势" points={trends.data.requests} />
          <MetricSeriesSection title="Token 趋势" points={trends.data.tokens} />
          <MetricSeriesSection title="成功率趋势" points={trends.data.success} />
          <MetricSeriesSection title="费用趋势" points={trends.data.costs} />
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
          {failures.data.recent_event_items.length > 0 ? (
            <ul className="event-timeline">
              {failures.data.recent_event_items.map((event) => (
                <li key={`${event.time}-${event.tenant_id}-${event.resolved_model}-${event.reason}`}>
                  <strong>
                    {event.time} · {event.category}
                    {formatFailureStatusCode(event.status_code)}
                  </strong>
                  <p>{formatTenantLabel(event.tenant_name, event.tenant_id)}</p>
                  <p>实际模型：{formatValue(event.resolved_model || event.request_model)}</p>
                  <p>供应商：{formatValue(event.provider)}</p>
                  <p>{neutralizePlatformNarrative(event.reason)}</p>
                </li>
              ))}
            </ul>
          ) : failures.data.recent_events.length > 0 ? (
            <ul className="event-timeline">
              {failures.data.recent_events.map((event) => (
                <li key={event}>{neutralizePlatformNarrative(event)}</li>
              ))}
            </ul>
          ) : (
            <p>暂无异常事件</p>
          )}
        </section>
      </div>

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>调用明细</h2>
            <p>支持按租户、实际模型、状态筛选，点击任意一行可查看脱敏后的输入输出摘要。</p>
          </div>
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
        <div className="filter-bar usage-request-filters">
          <label className="field-shell">
            <span>租户筛选</span>
            <select
              aria-label="租户筛选"
              value={tenantFilter}
              onChange={(event) => {
                setOffset(0);
                setTenantFilter(event.target.value);
              }}
            >
              <option value="">全部租户</option>
              {tenantOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <label className="field-shell">
            <span>实际模型筛选</span>
            <select
              aria-label="实际模型筛选"
              value={resolvedModelFilter}
              onChange={(event) => {
                setOffset(0);
                setResolvedModelFilter(event.target.value);
              }}
            >
              <option value="">全部实际模型</option>
              {resolvedModelOptions.map((option) => (
                <option key={option} value={option}>
                  {option}
                </option>
              ))}
            </select>
          </label>
          <label className="field-shell">
            <span>状态筛选</span>
            <select
              aria-label="状态筛选"
              value={statusFilter}
              onChange={(event) => {
                setOffset(0);
                setStatusFilter(event.target.value);
              }}
            >
              {requestStatusOptions.map((option) => (
                <option key={option.value || "all"} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="data-table-scroll">
          <DataTable
            tableClassName="data-table--wide data-table--clickable"
            onRowClick={(rowIndex) => void openRequestDetail(requests.data.items[rowIndex]?.request_id ?? "")}
            rowClassName={(rowIndex) =>
              requests.data.items[rowIndex]?.request_id === selectedRequestID ? "table-row--selected" : ""
            }
            columns={[
              "请求 ID",
              "租户",
              "端点",
              "模型",
              "实际模型",
              "任务分类",
              "目标档位",
              "路由原因",
              "状态",
              "输入 Token",
              "输出 Token",
              "缓存 Token",
              "总 Token",
              "输入费用",
              "输出费用",
              "缓存费用",
              "总费用",
              "输入单价",
              "输出单价",
              "缓存单价",
              "延迟",
              "计量来源",
            ]}
            rows={requests.data.items.map((item) => [
              item.request_id,
              formatTenantLabel(item.tenant_name, item.tenant_id || item.tenant),
              item.endpoint,
              item.model,
              formatValue(item.resolved_model),
              formatTaskClassLabel(item.task_class),
              formatTargetModelTierLabel(item.target_model_tier),
              formatRoutingReasonLabel(item.routing_reason),
              <StatusPill
                key={`${item.request_id}-status`}
                label={item.status}
                tone={toneForStatus(item.status)}
              />,
              formatValue(item.input_tokens),
              formatValue(item.output_tokens),
              formatValue(item.cached_tokens),
              formatValue(item.total_tokens),
              formatValue(item.input_cost),
              formatValue(item.output_cost),
              formatValue(item.cached_cost),
              formatValue(item.total_cost),
              formatValue(item.input_price),
              formatValue(item.output_price),
              formatValue(item.cached_price),
              item.latency,
              <SourcePill key={`${item.request_id}-source`} label={item.usage_source} />,
            ])}
          />
        </div>
      </section>

      {selectedRequestID ? (
        <div className="modal-backdrop" onClick={closeRequestDetail}>
          <section
            aria-labelledby="usage-request-detail-title"
            aria-modal="true"
            className="modal-card modal-card--wide"
            role="dialog"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="modal-card__header">
              <div>
                <span className="modal-card__eyebrow">REQUEST DETAIL</span>
                <h3 id="usage-request-detail-title">请求详情</h3>
                <p>{selectedRequestID}</p>
              </div>
              <button className="button-shell" type="button" onClick={closeRequestDetail}>
                关闭
              </button>
            </div>
            {detailLoading ? <p>正在加载请求详情...</p> : null}
            {detailError ? <p className="form-error">{detailError}</p> : null}
            {requestDetail ? (
              <div className="form-grid">
                <DetailList
                  items={[
                    { label: "租户", value: requestDetail.tenant_name || requestDetail.tenant_id || "--" },
                    { label: "租户 ID", value: requestDetail.tenant_id || "--" },
                    { label: "端点", value: requestDetail.endpoint },
                    { label: "请求模型", value: requestDetail.model },
                    { label: "实际模型", value: requestDetail.resolved_model },
                    { label: "任务分类", value: formatTaskClassLabel(requestDetail.task_class) },
                    { label: "目标档位", value: formatTargetModelTierLabel(requestDetail.target_model_tier) },
                    { label: "路由原因", value: formatRoutingReasonLabel(requestDetail.routing_reason) },
                    { label: "状态", value: requestDetail.status },
                    { label: "计量来源", value: requestDetail.usage_source },
                    { label: "总 Token", value: requestDetail.total_tokens },
                    { label: "总费用", value: requestDetail.total_cost },
                    { label: "延迟", value: requestDetail.latency },
                    { label: "首字延迟", value: `${requestDetail.first_token_latency_ms} ms` },
                  ]}
                />
                <section className="section-card">
                  <h3>脱敏后的输入摘要</h3>
                  <pre className="usage-detail__excerpt">{requestDetail.prompt_excerpt || "--"}</pre>
                </section>
                <section className="section-card">
                  <h3>脱敏后的输出摘要</h3>
                  <pre className="usage-detail__excerpt">{requestDetail.response_excerpt || "--"}</pre>
                </section>
                <section className="section-card">
                  <h3>失败原因</h3>
                  <DetailList
                    items={[
                      { label: "错误代码", value: requestDetail.error_code || "--" },
                      { label: "错误信息", value: requestDetail.error_message || "--" },
                    ]}
                  />
                  {requestDetail.failure_events.length > 0 ? (
                    <ul className="event-timeline usage-detail__events">
                      {requestDetail.failure_events.map((event) => (
                        <li key={`${event.time}-${event.category}-${event.reason}-${event.status_code}`}>
                          <strong>
                            {event.time} · {event.category}
                            {formatFailureStatusCode(event.status_code)}
                          </strong>
                          <p>{event.reason}</p>
                        </li>
                      ))}
                    </ul>
                  ) : null}
                </section>
              </div>
            ) : null}
          </section>
        </div>
      ) : null}
    </div>
  );
}
