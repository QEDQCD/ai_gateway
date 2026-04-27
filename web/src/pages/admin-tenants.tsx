import { useCallback } from "react";

import { DataTable, ErrorSection, LoadingSection, StatCard } from "../components/console";
import { getAPIKeys, getOverview, getUsageOverview } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

type TenantSummary = {
  tenant: string;
  keyCount: number;
  activeKeyCount: number;
  scopes: string;
  sampleKeyName: string;
};

function buildTenantSummaries(items: Awaited<ReturnType<typeof getAPIKeys>>["items"]): TenantSummary[] {
  const map = new Map<string, TenantSummary>();

  items.forEach((item) => {
    const current = map.get(item.tenant) ?? {
      tenant: item.tenant,
      keyCount: 0,
      activeKeyCount: 0,
      scopes: "",
      sampleKeyName: item.name,
    };
    const scopes = new Set(
      `${current.scopes},${item.scopes.join(",")}`
        .split(",")
        .map((value) => value.trim())
        .filter(Boolean),
    );

    current.keyCount += 1;
    current.activeKeyCount += item.status === "启用" ? 1 : 0;
    current.scopes = Array.from(scopes).join(", ");
    current.sampleKeyName = item.name;
    map.set(item.tenant, current);
  });

  return Array.from(map.values()).sort((left, right) => left.tenant.localeCompare(right.tenant));
}

function findOverviewStat(
  stats: Awaited<ReturnType<typeof getOverview>>["stats"],
  label: string,
  fallback: string,
) {
  return stats.find((item) => item.label === label)?.value ?? fallback;
}

export function AdminTenantsPage() {
  const loadTenants = useCallback(
    async () => {
      const [overview, apiKeys, usageOverview] = await Promise.all([
        getOverview(),
        getAPIKeys(),
        getUsageOverview(),
      ]);

      return {
        overview,
        apiKeys,
        usageOverview,
        tenantSummaries: buildTenantSummaries(apiKeys.items),
      };
    },
    [],
  );
  const { data, loading, error } = useRemoteData(loadTenants);

  if (loading) {
    return <LoadingSection text="正在加载租户治理视图..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "租户治理视图加载失败。"} />;
  }

  return (
    <div className="page-grid">
      <div className="stats-grid">
        <StatCard label="已发放租户" value={String(data.tenantSummaries.length)} />
        <StatCard
          label="启用密钥"
          value={String(data.apiKeys.items.filter((item) => item.status === "启用").length)}
        />
        <StatCard label="总调用数" value={String(data.usageOverview.total_requests)} />
        <StatCard label="成功率" value={data.usageOverview.success_rate} />
      </div>

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>租户治理列表</h2>
            <p>基于当前密钥清单聚合出最小租户治理视图。</p>
          </div>
          <p>共 {data.tenantSummaries.length} 个租户</p>
        </div>
        <DataTable
          columns={["租户 ID", "密钥数", "启用中", "示例密钥", "权限范围"]}
          rows={data.tenantSummaries.map((item) => [
            item.tenant,
            String(item.keyCount),
            String(item.activeKeyCount),
            item.sampleKeyName,
            item.scopes || "-",
          ])}
        />
      </section>

      <div className="two-column-grid">
        <section className="section-card">
          <h3>平台调用概况</h3>
          <div className="detail-list">
            <div className="detail-list__row">
              <dt>总 Token</dt>
              <dd>{data.usageOverview.total_tokens}</dd>
            </div>
            <div className="detail-list__row">
              <dt>平均延迟</dt>
              <dd>{data.usageOverview.average_latency}</dd>
            </div>
            <div className="detail-list__row">
              <dt>估算占比</dt>
              <dd>{data.usageOverview.estimated_share}</dd>
            </div>
          </div>
        </section>

        <section className="section-card">
          <h3>平台总览摘录</h3>
          <div className="detail-list">
            <div className="detail-list__row">
              <dt>24 小时请求量</dt>
              <dd>{findOverviewStat(data.overview.stats, "24 小时请求量", "-")}</dd>
            </div>
            <div className="detail-list__row">
              <dt>配额使用率</dt>
              <dd>{findOverviewStat(data.overview.stats, "配额使用率", "-")}</dd>
            </div>
            <div className="detail-list__row">
              <dt>活跃 API 密钥</dt>
              <dd>{findOverviewStat(data.overview.stats, "活跃 API 密钥", "-")}</dd>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
