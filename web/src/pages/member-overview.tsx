import { useCallback, useState } from "react";

import { DetailList, ErrorSection, LoadingSection, StatCard } from "../components/console";
import { createAccountDeletionApplication, getMemberOverview } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

const numberFormatter = new Intl.NumberFormat("zh-CN");

function formatNumber(value: number | undefined) {
  return numberFormatter.format(value ?? 0);
}

export function MemberOverviewPage() {
  const loadOverview = useCallback(() => getMemberOverview(), []);
  const { data, loading, error } = useRemoteData(loadOverview);
  const [deletionReason, setDeletionReason] = useState("");
  const [deletionSubmitting, setDeletionSubmitting] = useState(false);
  const [deletionError, setDeletionError] = useState<string | null>(null);
  const [deletionSuccess, setDeletionSuccess] = useState<string | null>(null);

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

  async function handleDeletionSubmit() {
    const reason = deletionReason.trim();
    if (!reason) {
      setDeletionError("请输入注销原因。");
      return;
    }

    try {
      setDeletionSubmitting(true);
      setDeletionError(null);
      const result = await createAccountDeletionApplication({ reason });
      setDeletionSuccess(`注销申请已提交：${result.item.status}`);
      setDeletionReason("");
    } catch (currentError) {
      setDeletionError(
        currentError instanceof Error ? currentError.message : "注销申请提交失败，请稍后重试。",
      );
    } finally {
      setDeletionSubmitting(false);
    }
  }

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

      <section className="section-card danger-zone-card">
        <div className="section-card__header">
          <div>
            <h2>账户注销</h2>
            <p>提交后需要管理员审批；审批通过会停用账号并禁用你创建的 API Key。</p>
          </div>
        </div>
        <div className="form-grid">
          <label className="field-shell">
            注销原因
            <textarea
              rows={3}
              value={deletionReason}
              disabled={deletionSubmitting}
              placeholder="例如：项目结束，不再需要使用平台"
              onChange={(event) => setDeletionReason(event.target.value)}
            />
          </label>
          {deletionError ? <p className="form-error">{deletionError}</p> : null}
          {deletionSuccess ? <p className="form-success">{deletionSuccess}</p> : null}
          <div className="page-actions">
            <button
              type="button"
              className="button-shell button-shell--danger"
              disabled={deletionSubmitting}
              onClick={handleDeletionSubmit}
            >
              {deletionSubmitting ? "提交中..." : "提交注销申请"}
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}
