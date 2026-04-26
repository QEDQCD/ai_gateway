import {
  DataTable,
  ErrorSection,
  LoadingSection,
  StatCard,
  SummarySection,
} from "../components/console";
import { getKnowledgeBases } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

export function KnowledgeBasePage() {
  const { data, loading, error } = useRemoteData(getKnowledgeBases);

  if (loading) {
    return <LoadingSection text="正在加载知识库..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "知识库数据加载失败。"} />;
  }

  return (
    <div className="page-grid">
      <div className="stats-grid">
        {data.stats.map((item) => (
          <StatCard key={item.label} label={item.label} value={item.value} />
        ))}
      </div>
      <section className="section-card">
        <h2>知识库列表</h2>
        <DataTable
          columns={["知识库", "文档数", "状态", "更新时间"]}
          rows={data.items.map((item) => [
            item.name,
            item.documents,
            item.status,
            item.updated_at,
          ])}
        />
      </section>
      <div className="two-column-grid">
        <SummarySection title="检索流程" items={data.flow_summary} />
        <SummarySection title="导入队列" items={data.queue_summary} />
      </div>
    </div>
  );
}
