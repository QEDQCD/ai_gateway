import {
  ErrorSection,
  LoadingSection,
  StatCard,
  TableSection,
} from "../components/console";
import { getOverview } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

export function DashboardPage() {
  const { data, loading, error } = useRemoteData(getOverview);

  if (loading) {
    return <LoadingSection text="正在加载总览数据..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "总览数据加载失败。"} />;
  }

  return (
    <div className="page-grid">
      <div className="stats-grid">
        {data.stats.map((item) => (
          <StatCard key={item.label} label={item.label} value={item.value} />
        ))}
      </div>
      <div className="two-column-grid">
        <TableSection
          title="路由健康"
          columns={["请求模型", "解析供应商", "延迟", "状态"]}
          rows={data.route_health.map((row) => row.columns)}
        />
        <TableSection
          title="热门模型"
          columns={["模型", "请求量", "占比", "模式"]}
          rows={data.top_models.map((row) => row.columns)}
        />
      </div>
      <div className="two-column-grid">
        <TableSection
          title="最近告警"
          columns={["时间", "类型", "范围"]}
          rows={data.recent_alerts.map((row) => row.columns)}
        />
        <TableSection
          title="审计快照"
          columns={["租户", "端点", "状态"]}
          rows={data.audit_snapshot.map((row) => row.columns)}
        />
      </div>
    </div>
  );
}
