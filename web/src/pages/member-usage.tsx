import { useCallback, useState } from "react";

import { DataTable, ErrorSection, LoadingSection, SourcePill, StatCard, StatusPill } from "../components/console";
import { getMemberUsageOverview, getMemberUsageRequests } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

const PAGE_SIZE = 20;

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

function formatValue(value: string) {
  return value || "--";
}

function formatTokenCost(tokens: string, cost: string) {
  return `${formatValue(tokens)} / ${formatValue(cost)}`;
}

export function MemberUsagePage() {
  const [offset, setOffset] = useState(0);
  const loadOverview = useCallback(() => getMemberUsageOverview(), []);
  const loadRequests = useCallback(
    () =>
      getMemberUsageRequests({
        limit: PAGE_SIZE,
        offset,
      }),
    [offset],
  );
  const overview = useRemoteData(loadOverview);
  const requests = useRemoteData(loadRequests);
  const error = overview.error ?? requests.error;

  if (error) {
    return <ErrorSection message={error} />;
  }

  if (!overview.data || !requests.data) {
    return <LoadingSection text="正在加载成员调用观测..." />;
  }

  const currentPage = Math.floor(requests.data.offset / requests.data.limit) + 1;
  const totalPages = Math.max(1, Math.ceil(requests.data.total / requests.data.limit));
  const hasPreviousPage = requests.data.offset > 0;
  const hasNextPage = requests.data.offset + requests.data.limit < requests.data.total;

  return (
    <div className="page-grid page-grid--usage">
      <section className="section-card">
        <h2>租户调用总览</h2>
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

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>最近请求</h2>
            <p>只展示当前租户的请求明细。</p>
          </div>
          <div className="usage-pagination">
            <p>
              共 {requests.data.total} 条，当前第 {currentPage} / {totalPages} 页
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
          columns={[
            "请求 ID",
            "租户",
            "端点",
            "模型",
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
            item.tenant,
            item.endpoint,
            item.model,
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
      </section>
    </div>
  );
}
