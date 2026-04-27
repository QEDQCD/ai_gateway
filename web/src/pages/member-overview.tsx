import { useCallback } from "react";

import { DetailList, ErrorSection, LoadingSection, StatCard } from "../components/console";
import { getMemberOverview } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

export function MemberOverviewPage() {
  const loadOverview = useCallback(() => getMemberOverview(), []);
  const { data, loading, error } = useRemoteData(loadOverview);

  if (loading) {
    return <LoadingSection text="正在加载成员总览..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "成员总览加载失败。"} />;
  }

  return (
    <div className="page-grid">
      <div className="stats-grid">
        <StatCard label="所属租户" value={data.tenant_name} />
        <StatCard label="租户 ID" value={data.tenant_id} />
        <StatCard label="活跃密钥数" value={String(data.active_api_keys)} />
        <StatCard label="控制台身份" value="成员" />
      </div>

      <div className="two-column-grid">
        <section className="section-card">
          <h2>租户信息</h2>
          <DetailList
            items={[
              { label: "租户名称", value: data.tenant_name },
              { label: "租户 ID", value: data.tenant_id },
              { label: "可用密钥", value: `${data.active_api_keys} 个` },
            ]}
          />
        </section>
        <section className="section-card">
          <h2>使用说明</h2>
          <p>成员控制台仅展示当前租户的数据和由当前成员创建的密钥。</p>
        </section>
      </div>
    </div>
  );
}
