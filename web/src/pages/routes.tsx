import {
  DataTable,
  ErrorSection,
  LoadingSection,
  StatCard,
  SummarySection,
} from "../components/console";
import { getRoutes } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

export function RoutesPage() {
  const { data, loading, error } = useRemoteData(getRoutes);

  if (loading) {
    return <LoadingSection text="正在加载路由策略..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "路由数据加载失败。"} />;
  }

  return (
    <div className="page-grid">
      <div className="stats-grid">
        {data.stats.map((item) => (
          <StatCard key={item.label} label={item.label} value={item.value} />
        ))}
      </div>
      <section className="section-card">
        <h2>路由明细</h2>
        <DataTable
          columns={["请求模型", "解析供应商", "凭证", "延迟", "状态"]}
          rows={data.items.map((item) => [
            item.requested_model,
            item.resolved_provider,
            item.credential,
            item.latency,
            item.status,
          ])}
        />
      </section>
      <SummarySection title="路由策略说明" items={data.policy_summary} />
    </div>
  );
}
