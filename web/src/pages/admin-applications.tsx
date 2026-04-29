import { useCallback, useEffect, useState } from "react";

import { ErrorSection, LoadingSection } from "../components/console";
import {
  type ApplicationItem,
  approveApplication,
  getApplications,
  rejectApplication,
} from "../lib/console-api";
import { useConsoleSession } from "../lib/session";
import { useRemoteData } from "../lib/use-remote-data";

function buildDefaultTenantID(item: ApplicationItem | null) {
  if (!item) {
    return "tenant_demo";
  }

  const base = item.company_name || item.email || item.id;
  const normalized = base
    .toLowerCase()
    .replace(/@.*$/, "")
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");

  return normalized ? `tenant_${normalized}` : `tenant_${item.id}`;
}

const defaultApprovalTokenLimit = 10_000_000;

type ApprovalResultState = {
  action: "approved" | "rejected";
  item: ApplicationItem;
  tenantID?: string;
};

export function AdminApplicationsPage() {
  const session = useConsoleSession();
  const loadApplications = useCallback(() => getApplications(), []);
  const { data, loading, error } = useRemoteData(loadApplications);
  const [items, setItems] = useState<ApplicationItem[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [tenantID, setTenantID] = useState("tenant_demo");
  const [tokenLimit, setTokenLimit] = useState(String(defaultApprovalTokenLimit));
  const [comment, setComment] = useState("通过控制台审批");
  const [submitting, setSubmitting] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionResult, setActionResult] = useState<ApprovalResultState | null>(null);

  useEffect(() => {
    if (!data) {
      return;
    }

    setItems(data.items);
    setSelectedID((current) => {
      if (current && data.items.some((item) => item.id === current)) {
        return current;
      }
      return data.items[0]?.id ?? null;
    });
  }, [data]);

  const selectedItem = items.find((item) => item.id === selectedID) ?? null;

  useEffect(() => {
    setTenantID(buildDefaultTenantID(selectedItem));
    setTokenLimit(String(defaultApprovalTokenLimit));
    setComment("通过控制台审批");
    setActionError(null);
  }, [selectedItem]);

  if (loading) {
    return <LoadingSection text="正在加载账号申请..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "账号申请加载失败。"} />;
  }

  async function handleApprove() {
    if (!selectedItem) {
      return;
    }

    const approvalItem = selectedItem;
    const approvalTenantID = tenantID.trim();
    const approvalTokenLimit = Number(tokenLimit.trim());
    const approvalComment = comment.trim() || "通过控制台审批";

    if (!Number.isFinite(approvalTokenLimit) || approvalTokenLimit <= 0) {
      setActionError("请输入大于 0 的 Token 上限。");
      return;
    }

    try {
      setSubmitting(true);
      setActionError(null);
      const result = await approveApplication(approvalItem.id, {
        actor_id: session.user_id,
        comment: approvalComment,
        tenant_id: approvalTenantID,
        token_limit: approvalTokenLimit,
      });
      setItems((current) =>
        current.map((item) => (item.id === approvalItem.id ? result.item : item)),
      );
      setActionResult({
        action: "approved",
        item: result.item,
        tenantID: approvalTenantID,
      });
    } catch (currentError) {
      setActionError(currentError instanceof Error ? currentError.message : "审批失败，请稍后重试。");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleReject() {
    if (!selectedItem) {
      return;
    }

    const rejectionItem = selectedItem;
    const rejectionComment = comment.trim() || "通过控制台审批";

    try {
      setSubmitting(true);
      setActionError(null);
      const result = await rejectApplication(rejectionItem.id, {
        actor_id: session.user_id,
        comment: rejectionComment,
      });
      setItems((current) =>
        current.map((item) => (item.id === rejectionItem.id ? result.item : item)),
      );
      setActionResult({
        action: "rejected",
        item: result.item,
      });
    } catch (currentError) {
      setActionError(currentError instanceof Error ? currentError.message : "审批失败，请稍后重试。");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="page-grid">
      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>申请列表</h2>
            <p>选择待处理申请后分配租户并完成审批。</p>
          </div>
          <p>{selectedItem ? `当前选中：${selectedItem.name}` : "当前没有待处理申请"}</p>
        </div>
        <table className="data-table">
          <thead>
            <tr>
              <th>选择</th>
              <th>申请人</th>
              <th>邮箱</th>
              <th>公司</th>
              <th>用途</th>
              <th>状态</th>
              <th>申请时间</th>
            </tr>
          </thead>
          <tbody>
            {items.length > 0 ? (
              items.map((item) => (
                <tr key={item.id} className={item.id === selectedID ? "table-row--selected" : ""}>
                  <td>
                    <button
                      type="button"
                      className="button-shell button-shell--table"
                      disabled={submitting}
                      onClick={() => setSelectedID(item.id)}
                    >
                      选择 {item.name}
                    </button>
                  </td>
                  <td>{item.name}</td>
                  <td>{item.email}</td>
                  <td>{item.company_name}</td>
                  <td>{item.use_case}</td>
                  <td>{item.status}</td>
                  <td>{item.created_at}</td>
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={7} className="table-empty-cell">
                  当前没有申请数据
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      <div className="two-column-grid">
        <section className="section-card">
          <h3>审批操作</h3>
          {selectedItem ? (
            <div className="form-grid">
              <label className="field-shell">
                租户 ID
                <input
                  value={tenantID}
                  disabled={submitting}
                  onChange={(event) => setTenantID(event.target.value)}
                />
              </label>
              <label className="field-shell">
                Token 上限
                <input
                  type="number"
                  min={1}
                  value={tokenLimit}
                  disabled={submitting}
                  onChange={(event) => setTokenLimit(event.target.value)}
                />
              </label>
              <label className="field-shell">
                审批备注
                <textarea
                  rows={3}
                  value={comment}
                  disabled={submitting}
                  onChange={(event) => setComment(event.target.value)}
                />
              </label>
              {actionError ? <p className="form-error">{actionError}</p> : null}
              <div className="page-actions">
                <button
                  type="button"
                  className="button-shell button-shell--primary"
                  disabled={submitting || selectedItem.status !== "pending"}
                  onClick={handleApprove}
                >
                  审批通过
                </button>
                <button
                  type="button"
                  className="button-shell"
                  disabled={submitting || selectedItem.status !== "pending"}
                  onClick={handleReject}
                >
                  拒绝审批
                </button>
                <button
                  type="button"
                  className="button-shell"
                  disabled={submitting}
                  onClick={() => {
                    setTenantID(buildDefaultTenantID(selectedItem));
                    setTokenLimit(String(defaultApprovalTokenLimit));
                    setComment("通过控制台审批");
                    setActionError(null);
                  }}
                >
                  重置
                </button>
              </div>
              {selectedItem.status === "approved" ? <p>该申请已经通过审批。</p> : null}
              {selectedItem.status === "rejected" ? <p>该申请已经拒绝。</p> : null}
            </div>
          ) : (
            <p>请选择一条申请后再进行审批。</p>
          )}
        </section>

        <section className="section-card">
          <h3>当前申请摘要</h3>
          {selectedItem ? (
            <div className="detail-list">
              <div className="detail-list__row">
                <dt>申请人</dt>
                <dd>{selectedItem.name}</dd>
              </div>
              <div className="detail-list__row">
                <dt>邮箱</dt>
                <dd>{selectedItem.email}</dd>
              </div>
              <div className="detail-list__row">
                <dt>建议租户</dt>
                <dd>{buildDefaultTenantID(selectedItem)}</dd>
              </div>
              <div className="detail-list__row">
                <dt>用途</dt>
                <dd>{selectedItem.use_case}</dd>
              </div>
            </div>
          ) : (
            <p>暂无选中申请。</p>
          )}
        </section>
      </div>

      {actionResult ? (
        <section className="section-card section-card--success">
          <h3>{actionResult.action === "approved" ? "审批已完成" : "审批已拒绝"}</h3>
          <p>申请人：{actionResult.item.name}</p>
          <p>状态：{actionResult.item.status}</p>
          {actionResult.tenantID ? <p>租户：{actionResult.tenantID}</p> : null}
        </section>
      ) : null}
    </div>
  );
}
