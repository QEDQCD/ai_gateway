import { useCallback, useEffect, useState } from "react";

import { ErrorSection, LoadingSection } from "../components/console";
import {
  type AccountDeletionApplicationItem,
  type ApplicationItem,
  approveAccountDeletionApplication,
  approveApplication,
  getAccountDeletionApplications,
  getApplications,
  getRoutes,
  rejectAccountDeletionApplication,
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
const fallbackApprovalModels = ["qwen-flash", "mimo-v2.5-pro"];

function formatApprovalModelName(model: string) {
  if (model === "qwen-flash") {
    return "Qwen Flash";
  }
  if (model === "mimo-v2.5-pro") {
    return "MIMO Pro";
  }
  return model;
}

function describeApprovalModel(model: string) {
  if (model === "qwen-flash") {
    return "通义千问快速模型，适合通用问答、低成本高频调用。";
  }
  if (model === "mimo-v2.5-pro") {
    return "小米 MIMO 推理模型，适合代码、复杂分析与深度问答。";
  }
  return "来自路由配置的自定义模型。";
}

type ApprovalResultState = {
  action: "approved" | "rejected";
  item: ApplicationItem;
  tenantID?: string;
};

type DeletionResultState = {
  action: "approved" | "rejected";
  item: AccountDeletionApplicationItem;
};

export function AdminApplicationsPage() {
  const session = useConsoleSession();
  const loadApplications = useCallback(() => getApplications(), []);
  const loadDeletionApplications = useCallback(() => getAccountDeletionApplications(), []);
  const loadRoutes = useCallback(() => getRoutes(), []);
  const { data, loading, error } = useRemoteData(loadApplications);
  const { data: deletionData } = useRemoteData(loadDeletionApplications);
  const { data: routesData } = useRemoteData(loadRoutes);
  const [items, setItems] = useState<ApplicationItem[]>([]);
  const [deletionItems, setDeletionItems] = useState<AccountDeletionApplicationItem[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [selectedDeletionID, setSelectedDeletionID] = useState<string | null>(null);
  const [approvalModalOpen, setApprovalModalOpen] = useState(false);
  const [deletionModalOpen, setDeletionModalOpen] = useState(false);
  const [tenantID, setTenantID] = useState("tenant_demo");
  const [tokenLimit, setTokenLimit] = useState(String(defaultApprovalTokenLimit));
  const [allowedModels, setAllowedModels] = useState<string[]>(fallbackApprovalModels);
  const [comment, setComment] = useState("通过控制台审批");
  const [submitting, setSubmitting] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionResult, setActionResult] = useState<ApprovalResultState | null>(null);
  const [deletionComment, setDeletionComment] = useState("同意注销申请");
  const [deletionSubmitting, setDeletionSubmitting] = useState(false);
  const [deletionError, setDeletionError] = useState<string | null>(null);
  const [deletionResult, setDeletionResult] = useState<DeletionResultState | null>(null);

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

  useEffect(() => {
    if (!deletionData) {
      return;
    }

    setDeletionItems(deletionData.items);
    setSelectedDeletionID((current) => {
      if (current && deletionData.items.some((item) => item.id === current)) {
        return current;
      }
      return deletionData.items[0]?.id ?? null;
    });
  }, [deletionData]);

  const selectedItem = items.find((item) => item.id === selectedID) ?? null;
  const selectedDeletionItem =
    deletionItems.find((item) => item.id === selectedDeletionID) ?? null;
  const availableModels = Array.from(
    new Set(
      (routesData?.items ?? [])
        .map((item) => item.requested_model.trim())
        .filter(Boolean)
        .concat(fallbackApprovalModels),
    ),
  );

  function toggleAllowedModel(model: string) {
    setAllowedModels((current) =>
      current.includes(model) ? current.filter((item) => item !== model) : [...current, model],
    );
  }

  function selectApplication(id: string) {
    setSelectedID(id);
    setApprovalModalOpen(true);
  }

  function selectDeletionApplication(id: string) {
    setSelectedDeletionID(id);
    setDeletionModalOpen(true);
  }

  useEffect(() => {
    setTenantID(buildDefaultTenantID(selectedItem));
    setTokenLimit(String(defaultApprovalTokenLimit));
    setAllowedModels(availableModels);
    setComment("通过控制台审批");
    setActionError(null);
    setActionResult(null);
  }, [selectedID, routesData]);

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
    if (allowedModels.length === 0) {
      setActionError("请至少选择一个可使用模型。");
      return;
    }

    try {
      setSubmitting(true);
      setActionError(null);
      setActionResult(null);
      const result = await approveApplication(approvalItem.id, {
        actor_id: session.user_id,
        comment: approvalComment,
        tenant_id: approvalTenantID,
        token_limit: approvalTokenLimit,
        allowed_models: allowedModels,
      });
      setItems((current) =>
        current.map((item) => (item.id === approvalItem.id ? result.item : item)),
      );
      setActionResult({
        action: "approved",
        item: result.item,
        tenantID: approvalTenantID,
      });
      setApprovalModalOpen(false);
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
      setActionResult(null);
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
      setApprovalModalOpen(false);
    } catch (currentError) {
      setActionError(currentError instanceof Error ? currentError.message : "审批失败，请稍后重试。");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleDeletionReview(action: "approved" | "rejected") {
    if (!selectedDeletionItem) {
      return;
    }

    const reviewItem = selectedDeletionItem;
    const reviewComment =
      deletionComment.trim() || (action === "approved" ? "同意注销申请" : "拒绝注销申请");

    try {
      setDeletionSubmitting(true);
      setDeletionError(null);
      setDeletionResult(null);
      const result =
        action === "approved"
          ? await approveAccountDeletionApplication(reviewItem.id, {
              actor_id: session.user_id,
              comment: reviewComment,
            })
          : await rejectAccountDeletionApplication(reviewItem.id, {
              actor_id: session.user_id,
              comment: reviewComment,
            });
      setDeletionItems((current) =>
        current.map((item) => (item.id === reviewItem.id ? result.item : item)),
      );
      setDeletionResult({ action, item: result.item });
      setDeletionModalOpen(false);
    } catch (currentError) {
      setDeletionError(
        currentError instanceof Error ? currentError.message : "注销申请审批失败，请稍后重试。",
      );
    } finally {
      setDeletionSubmitting(false);
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
                      onClick={() => selectApplication(item.id)}
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

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>注销申请</h2>
            <p>审批通过后会停用用户、禁用该用户创建的 API Key，并保留审计记录。</p>
          </div>
          <p>{selectedDeletionItem ? `当前选中：${selectedDeletionItem.user_name}` : "暂无注销申请"}</p>
        </div>
        <table className="data-table">
          <thead>
            <tr>
              <th>选择</th>
              <th>用户</th>
              <th>邮箱</th>
              <th>租户</th>
              <th>原因</th>
              <th>状态</th>
              <th>申请时间</th>
            </tr>
          </thead>
          <tbody>
            {deletionItems.length > 0 ? (
              deletionItems.map((item) => (
                <tr
                  key={item.id}
                  className={item.id === selectedDeletionID ? "table-row--selected" : ""}
                >
                  <td>
                    <button
                      type="button"
                      className="button-shell button-shell--table"
                      disabled={deletionSubmitting}
                      onClick={() => selectDeletionApplication(item.id)}
                    >
                      选择 {item.user_name}
                    </button>
                  </td>
                  <td>{item.user_name}</td>
                  <td>{item.user_email}</td>
                  <td>{item.tenant_id}</td>
                  <td>{item.reason}</td>
                  <td>{item.status}</td>
                  <td>{item.created_at}</td>
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={7} className="table-empty-cell">
                  当前没有注销申请
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      <div className="two-column-grid">
        <section className="section-card">
          <h3>当前账号申请</h3>
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
            <p>选择账号申请后会在弹窗中完成审批。</p>
          )}
        </section>

        <section className="section-card">
          <h3>当前注销申请</h3>
          {selectedDeletionItem ? (
            <div className="detail-list">
              <div className="detail-list__row">
                <dt>申请用户</dt>
                <dd>{selectedDeletionItem.user_name}</dd>
              </div>
              <div className="detail-list__row">
                <dt>租户</dt>
                <dd>{selectedDeletionItem.tenant_id}</dd>
              </div>
              <div className="detail-list__row">
                <dt>注销原因</dt>
                <dd>{selectedDeletionItem.reason}</dd>
              </div>
              <div className="detail-list__row">
                <dt>已清理 Key</dt>
                <dd>{selectedDeletionItem.disabled_api_keys} 个</dd>
              </div>
            </div>
          ) : (
            <p>选择注销申请后会在弹窗中完成审批。</p>
          )}
        </section>
      </div>

      {approvalModalOpen && selectedItem ? (
        <div className="modal-backdrop" role="presentation">
          <section
            className="modal-card modal-card--wide"
            role="dialog"
            aria-modal="true"
            aria-labelledby="approval-modal-title"
          >
            <div className="modal-card__header">
              <div>
                <span className="modal-card__eyebrow">账号申请</span>
                <h3 id="approval-modal-title">审批操作</h3>
                <p>为 {selectedItem.name} 分配租户、额度和可使用模型范围。</p>
              </div>
              <button
                type="button"
                className="button-shell"
                disabled={submitting}
                onClick={() => setApprovalModalOpen(false)}
              >
                关闭
              </button>
            </div>
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
              <div className="field-shell">
                <div className="model-scope-picker__header">
                  <span>可使用模型范围</span>
                  <span className="model-scope-picker__count">
                    已选 {allowedModels.length}/{availableModels.length}
                  </span>
                </div>
                <div className="model-scope-picker" role="group" aria-label="可使用模型范围">
                  {availableModels.map((model) => {
                    const checked = allowedModels.includes(model);

                    return (
                      <label
                        key={model}
                        className={`model-scope-card${
                          checked ? " model-scope-card--selected" : ""
                        }${submitting ? " model-scope-card--disabled" : ""}`}
                      >
                        <input
                          type="checkbox"
                          value={model}
                          checked={checked}
                          disabled={submitting}
                          onChange={() => toggleAllowedModel(model)}
                        />
                        <span className="model-scope-card__content">
                          <span className="model-scope-card__title">
                            {formatApprovalModelName(model)}
                          </span>
                          <span className="model-scope-card__id">{model}</span>
                          <span className="model-scope-card__meta">
                            {describeApprovalModel(model)}
                          </span>
                        </span>
                      </label>
                    );
                  })}
                </div>
                <small className="model-scope-picker__hint">
                  直接勾选一个或多个模型；审批后租户只能调用选中的模型。
                </small>
              </div>
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
                    setAllowedModels(availableModels);
                    setComment("通过控制台审批");
                    setActionError(null);
                    setActionResult(null);
                  }}
                >
                  重置
                </button>
              </div>
              {selectedItem.status === "approved" ? <p>该申请已经通过审批。</p> : null}
              {selectedItem.status === "rejected" ? <p>该申请已经拒绝。</p> : null}
            </div>
          </section>
        </div>
      ) : null}

      {deletionModalOpen && selectedDeletionItem ? (
        <div className="modal-backdrop" role="presentation">
          <section
            className="modal-card"
            role="dialog"
            aria-modal="true"
            aria-labelledby="deletion-modal-title"
          >
            <div className="modal-card__header">
              <div>
                <span className="modal-card__eyebrow">注销申请</span>
                <h3 id="deletion-modal-title">注销审批</h3>
                <p>审批通过后会停用用户并禁用该用户创建的 API Key。</p>
              </div>
              <button
                type="button"
                className="button-shell"
                disabled={deletionSubmitting}
                onClick={() => setDeletionModalOpen(false)}
              >
                关闭
              </button>
            </div>
            <div className="form-grid">
              <div className="detail-list">
                <div className="detail-list__row">
                  <dt>申请用户</dt>
                  <dd>{selectedDeletionItem.user_name}</dd>
                </div>
                <div className="detail-list__row">
                  <dt>租户</dt>
                  <dd>{selectedDeletionItem.tenant_id}</dd>
                </div>
                <div className="detail-list__row">
                  <dt>注销原因</dt>
                  <dd>{selectedDeletionItem.reason}</dd>
                </div>
                <div className="detail-list__row">
                  <dt>已清理 Key</dt>
                  <dd>{selectedDeletionItem.disabled_api_keys} 个</dd>
                </div>
              </div>
              <label className="field-shell">
                审批备注
                <textarea
                  rows={3}
                  value={deletionComment}
                  disabled={deletionSubmitting}
                  onChange={(event) => setDeletionComment(event.target.value)}
                />
              </label>
              {deletionError ? <p className="form-error">{deletionError}</p> : null}
              <div className="page-actions">
                <button
                  type="button"
                  className="button-shell button-shell--danger"
                  disabled={deletionSubmitting || selectedDeletionItem.status !== "pending"}
                  onClick={() => handleDeletionReview("approved")}
                >
                  审批注销
                </button>
                <button
                  type="button"
                  className="button-shell"
                  disabled={deletionSubmitting || selectedDeletionItem.status !== "pending"}
                  onClick={() => handleDeletionReview("rejected")}
                >
                  拒绝注销
                </button>
              </div>
            </div>
          </section>
        </div>
      ) : null}

      {actionResult ? (
        <section className="section-card section-card--success">
          <h3>{actionResult.action === "approved" ? "审批已完成" : "审批已拒绝"}</h3>
          <p>申请人：{actionResult.item.name}</p>
          <p>状态：{actionResult.item.status}</p>
          {actionResult.tenantID ? <p>租户：{actionResult.tenantID}</p> : null}
        </section>
      ) : null}

      {deletionResult ? (
        <section className="section-card section-card--success">
          <h3>{deletionResult.action === "approved" ? "注销审批已通过" : "注销审批已拒绝"}</h3>
          <p>用户：{deletionResult.item.user_name}</p>
          <p>状态：{deletionResult.item.status}</p>
          <p>已清理 API Key：{deletionResult.item.disabled_api_keys} 个</p>
        </section>
      ) : null}
    </div>
  );
}
