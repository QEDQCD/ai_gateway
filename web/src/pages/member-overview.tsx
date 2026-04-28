import { useCallback } from "react";

import { DetailList, ErrorSection, LoadingSection, StatCard } from "../components/console";
import { getMemberOverview } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

const numberFormatter = new Intl.NumberFormat("zh-CN");

function formatNumber(value: number | undefined) {
  return numberFormatter.format(value ?? 0);
}

export function MemberOverviewPage() {
  const loadOverview = useCallback(() => getMemberOverview(), []);
  const { data, loading, error } = useRemoteData(loadOverview);

  if (loading) {
    return <LoadingSection text="正在加载成员总览..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "成员总览加载失败。"} />;
  }

  const quota = data.quota ?? {
    configured: false,
    request_limit: 0,
    requests_used: 0,
    requests_remaining: 0,
    token_limit: 0,
    tokens_used: 0,
    tokens_remaining: 0,
    resets_at: "",
  };

  return (
    <div className="page-grid">
      <div className="stats-grid">
        <StatCard label="所属租户" value={data.tenant_name} />
        <StatCard label="租户 ID" value={data.tenant_id} />
        <StatCard label="活跃密钥数" value={String(data.active_api_keys)} />
        <StatCard label="控制台身份" value="成员" />
      </div>

      {quota.configured ? (
        <section className="quota-summary-grid">
          <article className="quota-card">
            <span>本月请求额度</span>
            <strong>
              {formatNumber(quota.requests_used)} / {formatNumber(quota.request_limit)}
            </strong>
            <p>
              剩余 {formatNumber(quota.requests_remaining)}
              {quota.resets_at ? `，重置时间 ${quota.resets_at}` : ""}
            </p>
          </article>
          <article className="quota-card">
            <span>本月 Token 额度</span>
            <strong>
              {formatNumber(quota.tokens_used)} / {formatNumber(quota.token_limit)}
            </strong>
            <p>剩余 {formatNumber(quota.tokens_remaining)}</p>
          </article>
        </section>
      ) : null}

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
