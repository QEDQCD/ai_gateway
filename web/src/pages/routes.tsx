import {
  DataTable,
  ErrorSection,
  LoadingSection,
  StatCard,
  SummarySection,
} from "../components/console";
import { getRoutes } from "../lib/console-api";
import {
  neutralizeCredentialLabel,
  neutralizeLineLabel,
  neutralizePlatformNarrative,
  neutralizeRouteMetricLabel,
} from "../lib/platform-routing";
import { useRemoteData } from "../lib/use-remote-data";

function buildRouteRows(
  items: Array<{
    requested_model: string;
    route_label: string;
    credential: string;
    latency: string;
    status: string;
  }>,
) {
  return items.map((item) => [
    item.requested_model,
    neutralizeLineLabel(item.route_label),
    neutralizeCredentialLabel(item.credential),
    item.latency,
    item.status,
  ]);
}

export function RoutesPage() {
  const { data, loading, error } = useRemoteData(getRoutes);

  if (loading) {
    return <LoadingSection text="正在加载平台路由..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "路由数据加载失败。"} />;
  }

  const qwenItems = data.items.filter((item) => item.provider_group === "qwen");
  const mimoItems = data.items.filter((item) => item.provider_group === "mimo");

  return (
    <div className="page-grid">
      <div className="stats-grid">
        {data.stats.map((item) => (
          <StatCard
            key={item.label}
            label={neutralizeRouteMetricLabel(item.label)}
            value={item.value}
          />
        ))}
      </div>
      {qwenItems.length > 0 ? (
        <section className="section-card">
          <h2>Qwen 路由观测</h2>
          <DataTable
            columns={["请求模型", "平台路由结果", "平台凭证", "延迟", "状态"]}
            rows={buildRouteRows(qwenItems)}
          />
        </section>
      ) : null}
      {mimoItems.length > 0 ? (
        <section className="section-card">
          <h2>MIMO 路由观测</h2>
          <DataTable
            columns={["请求模型", "平台路由结果", "平台凭证", "延迟", "状态"]}
            rows={buildRouteRows(mimoItems)}
          />
        </section>
      ) : null}
      <SummarySection
        title="处理链路说明"
        items={data.policy_summary.map((item) => neutralizePlatformNarrative(item))}
      />
    </div>
  );
}
