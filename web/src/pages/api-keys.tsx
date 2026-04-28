import { useCallback, useEffect, useId, useRef, useState } from "react";

import { ErrorSection, LoadingSection } from "../components/console";
import {
  type APIKeyItem,
  type APIKeyMutationResult,
  type APIKeySecretView,
  type APIKeyScope,
  copyAPIKeySecret,
  copyMemberAPIKeySecret,
  createAPIKey,
  createMemberAPIKey,
  deactivateAPIKey,
  deactivateMemberAPIKey,
  deleteAPIKey,
  getAPIKeys,
  getMemberAPIKeys,
  revealAPIKeySecret,
  revealMemberAPIKeySecret,
  rotateAPIKey,
  rotateMemberAPIKey,
} from "../lib/console-api";
import { useConsoleSession } from "../lib/session";
import { useRemoteData } from "../lib/use-remote-data";

type ActionMode = "create" | "rotate" | "deactivate" | "delete" | null;

const apiKeyScopeOptions: APIKeyScope[] = ["chat", "rag", "embeddings"];
const defaultScopes: APIKeyScope[] = ["chat"];

function maskAPIKey(rawKey: string) {
  if (rawKey.length <= 8) {
    return "••••••••";
  }

  return `${rawKey.slice(0, 4)}••••••••${rawKey.slice(-4)}`;
}

function buildSecretViewFromRawKey(item: APIKeyItem, rawKey: string): APIKeySecretView {
  return {
    api_key_id: item.id,
    masked_key: maskAPIKey(rawKey),
    revealable: true,
    legacy_unrecoverable: false,
    expires_at: item.expires_at,
  };
}

type CopyMethod = "clipboard" | "execCommand" | "manual";

async function copyTextWithFallback(value: string): Promise<CopyMethod> {
  if (
    typeof navigator !== "undefined" &&
    "clipboard" in navigator &&
    navigator.clipboard &&
    typeof navigator.clipboard.writeText === "function"
  ) {
    try {
      await navigator.clipboard.writeText(value);
      return "clipboard";
    } catch {
      // Some browsers expose the API but reject in non-secure or restricted contexts.
    }
  }

  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.readOnly = true;
  textarea.setAttribute("aria-hidden", "true");
  textarea.style.position = "fixed";
  textarea.style.top = "0";
  textarea.style.left = "-9999px";
  textarea.style.width = "1px";
  textarea.style.height = "1px";
  textarea.style.padding = "0";
  textarea.style.border = "0";
  textarea.style.outline = "none";
  textarea.style.boxShadow = "none";
  textarea.style.background = "transparent";
  document.body.appendChild(textarea);
  let copied = false;

  try {
    textarea.focus();
    textarea.select();
    textarea.setSelectionRange(0, textarea.value.length);

    const execCommand = (
      document as Document & { execCommand?: (command: string) => boolean }
    ).execCommand;
    if (typeof execCommand === "function") {
      try {
        copied = execCommand.call(document, "copy");
      } catch {
        copied = false;
      }
    }
  } finally {
    document.body.removeChild(textarea);
  }

  if (copied) {
    return "execCommand";
  }

  try {
    globalThis.prompt?.("浏览器自动复制不可用，请手动复制完整密钥：", value);
  } catch {
    // Ignore prompt restrictions and still surface manual copy guidance in UI.
  }
  return "manual";
}

function getSubmitLabel(actionMode: Exclude<ActionMode, null>, submitting: boolean) {
  if (!submitting) {
    return actionMode === "create"
      ? "确认创建"
      : actionMode === "rotate"
        ? "确认轮换"
        : actionMode === "deactivate"
          ? "确认停用"
          : "确认删除";
  }

  return actionMode === "create"
    ? "创建中..."
    : actionMode === "rotate"
      ? "轮换中..."
      : actionMode === "deactivate"
        ? "停用中..."
        : "删除中...";
}

function ScopePicker({
  label,
  buttonLabel,
  selectedScopes,
  onToggleScope,
}: {
  label: string;
  buttonLabel: string;
  selectedScopes: APIKeyScope[];
  onToggleScope: (scope: APIKeyScope) => void;
}) {
  const [open, setOpen] = useState(false);
  const menuID = useId();
  const summary = selectedScopes.length > 0 ? selectedScopes.join(" / ") : "未选择";

  return (
    <div className="field-shell">
      <span>{label}</span>
      <div className="scope-picker">
        <button
          type="button"
          className="button-shell button-shell--scope"
          aria-expanded={open}
          aria-controls={menuID}
          aria-label={buttonLabel}
          onClick={() => setOpen((current) => !current)}
        >
          {buttonLabel}
          <span className="scope-picker__summary">{summary}</span>
        </button>
        {open ? (
          <div id={menuID} className="scope-picker__menu">
            {apiKeyScopeOptions.map((scope) => (
              <label key={scope} className="scope-picker__option">
                <input
                  type="checkbox"
                  checked={selectedScopes.includes(scope)}
                  onChange={() => onToggleScope(scope)}
                />
                <span>{scope}</span>
              </label>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function APIKeysPage() {
  const session = useConsoleSession();
  const isAdmin = session.role === "admin";
  const loadAPIKeys = useCallback(
    () => (isAdmin ? getAPIKeys() : getMemberAPIKeys()),
    [isAdmin],
  );
  const { data, loading, error } = useRemoteData(loadAPIKeys);
  const [items, setItems] = useState<APIKeyItem[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [actionMode, setActionMode] = useState<ActionMode>(null);
  const [tenantID, setTenantID] = useState("");
  const [name, setName] = useState("");
  const [selectedScopes, setSelectedScopes] = useState<APIKeyScope[]>(defaultScopes);
  const [submitting, setSubmitting] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionResult, setActionResult] = useState<APIKeyMutationResult | null>(null);
  const [actionNotice, setActionNotice] = useState<string | null>(null);
  const [selectedSecret, setSelectedSecret] = useState<APIKeySecretView | null>(null);
  const [secretLoading, setSecretLoading] = useState(false);
  const [secretError, setSecretError] = useState<string | null>(null);
  const [selectedCopyNotice, setSelectedCopyNotice] = useState<string | null>(null);
  const [resultCopyNotice, setResultCopyNotice] = useState<string | null>(null);
  const actionResultRef = useRef<HTMLElement | null>(null);

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
    if (!actionNotice && !actionResult) {
      return;
    }

    const scrollIntoView = actionResultRef.current?.scrollIntoView;
    if (typeof scrollIntoView === "function") {
      scrollIntoView.call(actionResultRef.current, {
        behavior: "smooth",
        block: "start",
      });
    }
  }, [actionNotice, actionResult]);

  useEffect(() => {
    setSelectedSecret((current) => {
      if (!selectedID || current?.api_key_id !== selectedID) {
        return null;
      }
      return current;
    });
    setSecretError(null);
    setSelectedCopyNotice(null);
  }, [selectedID]);

  if (loading) {
    return <LoadingSection text="正在加载 API 密钥..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "API 密钥加载失败。"} />;
  }

  const selectedItem = items.find((item) => item.id === selectedID) ?? null;
  const selectedSecretForItem =
    selectedItem && selectedSecret?.api_key_id === selectedItem.id ? selectedSecret : null;

  function resetFeedback() {
    setActionError(null);
    setActionResult(null);
    setActionNotice(null);
    setSecretError(null);
    setSelectedCopyNotice(null);
    setResultCopyNotice(null);
  }

  function toggleScope(scope: APIKeyScope) {
    setSelectedScopes((current) => {
      if (current.includes(scope)) {
        return current.filter((item) => item !== scope);
      }
      return [...current, scope];
    });
  }

  function openCreate() {
    resetFeedback();
    setActionMode("create");
    setTenantID(selectedItem?.tenant ?? "");
    setName("");
    setSelectedScopes(defaultScopes);
  }

  function openRotate() {
    if (!selectedItem) {
      return;
    }

    resetFeedback();
    setActionMode("rotate");
    setName(selectedItem.name);
    setSelectedScopes(selectedItem.scopes);
  }

  function openDeactivate() {
    if (!selectedItem) {
      return;
    }

    resetFeedback();
    setActionMode("deactivate");
  }

  function openDelete() {
    if (!selectedItem) {
      return;
    }

    resetFeedback();
    setActionMode("delete");
  }

  function closeActionPanel() {
    setActionMode(null);
    setActionError(null);
  }

  async function handleCopyRawKey() {
    if (!actionResult?.raw_key) {
      return;
    }

    try {
      const copyMethod = await copyTextWithFallback(actionResult.raw_key);
      setSelectedCopyNotice(null);
      setResultCopyNotice(
        copyMethod === "manual"
          ? "浏览器自动复制不可用，请在弹窗中手动复制完整密钥。"
          : "完整密钥已复制到剪贴板。",
      );
    } catch {
      setResultCopyNotice("复制失败，请重试。");
    }
  }

  async function handleRevealSecret() {
    if (!selectedItem) {
      return;
    }

    try {
      setSecretLoading(true);
      setSecretError(null);
      setSelectedCopyNotice(null);
      const secret = isAdmin
        ? await revealAPIKeySecret(selectedItem.id)
        : await revealMemberAPIKeySecret(selectedItem.id);
      setSelectedSecret(secret);
      if (secret.legacy_unrecoverable) {
        setSecretError("该历史密钥不可回显，请执行轮换后再复制。");
      }
    } catch (currentError) {
      setSecretError(currentError instanceof Error ? currentError.message : "密钥加载失败。");
    } finally {
      setSecretLoading(false);
    }
  }

  async function handleCopySelectedSecret() {
    if (!selectedItem) {
      return;
    }

    try {
      setSecretLoading(true);
      setSecretError(null);
      setSelectedCopyNotice(null);
      setResultCopyNotice(null);
      const secret = isAdmin
        ? await copyAPIKeySecret(selectedItem.id)
        : await copyMemberAPIKeySecret(selectedItem.id);
      setSelectedSecret(secret);

      if (!secret.revealable || !secret.full_key) {
        setSelectedCopyNotice("该历史密钥不可回显，请先轮换后再复制。");
        return;
      }

      const copyMethod = await copyTextWithFallback(secret.full_key);
      setSelectedCopyNotice(
        copyMethod === "manual"
          ? "浏览器自动复制不可用，请在弹窗中手动复制完整密钥。"
          : "完整密钥已复制到剪贴板。",
      );
    } catch (currentError) {
      setSelectedCopyNotice(
        currentError instanceof Error ? currentError.message : "复制失败，请重试。",
      );
    } finally {
      setSecretLoading(false);
    }
  }

  async function handleConfirm() {
    try {
      setSubmitting(true);
      setActionError(null);
      setSelectedCopyNotice(null);
      setResultCopyNotice(null);

      if ((actionMode === "create" || actionMode === "rotate") && selectedScopes.length < 1) {
        setActionError("至少选择 1 项权限范围。");
        return;
      }

      if (actionMode === "create") {
        const result = isAdmin
          ? await createAPIKey({
              tenant_id: tenantID.trim(),
              name: name.trim(),
              scopes: selectedScopes,
            })
          : await createMemberAPIKey({
              name: name.trim(),
              scopes: selectedScopes,
            });
        setItems((current) => [result.item, ...current]);
        setSelectedID(result.item.id);
        setActionResult(result);
        setActionNotice("新建密钥已完成");
        if (result.raw_key) {
          setSelectedSecret(buildSecretViewFromRawKey(result.item, result.raw_key));
        }
        setActionMode(null);
        return;
      }

      if (actionMode === "rotate" && selectedItem) {
        const result = isAdmin
          ? await rotateAPIKey(selectedItem.id, {
              name: name.trim(),
              scopes: selectedScopes,
            })
          : await rotateMemberAPIKey(selectedItem.id, {
              name: name.trim(),
              scopes: selectedScopes,
            });
        setItems((current) => [
          result.item,
          ...current.map((item) =>
            item.id === selectedItem.id ? { ...item, status: "停用" } : item,
          ),
        ]);
        setSelectedID(result.item.id);
        setActionResult(result);
        setActionNotice("轮换操作已完成");
        if (result.raw_key) {
          setSelectedSecret(buildSecretViewFromRawKey(result.item, result.raw_key));
        }
        setActionMode(null);
        return;
      }

      if (actionMode === "deactivate" && selectedItem) {
        const result = isAdmin
          ? await deactivateAPIKey(selectedItem.id)
          : await deactivateMemberAPIKey(selectedItem.id);
        setItems((current) =>
          current.map((item) => (item.id === selectedItem.id ? result.item : item)),
        );
        setActionResult(result);
        setActionNotice("停用操作已完成");
        if (selectedSecret?.api_key_id === selectedItem.id) {
          setSelectedSecret((current) => (current ? { ...current, expires_at: result.item.expires_at } : null));
        }
        setActionMode(null);
        return;
      }

      if (isAdmin && actionMode === "delete" && selectedItem) {
        const result = await deleteAPIKey(selectedItem.id);
        setItems((current) => current.filter((item) => item.id !== selectedItem.id));
        setSelectedID((current) => {
          if (current !== selectedItem.id) {
            return current;
          }
          const remaining = items.filter((item) => item.id !== selectedItem.id);
          return remaining[0]?.id ?? null;
        });
        setActionResult(result);
        setActionNotice("删除操作已完成");
        setSelectedSecret(null);
        setActionMode(null);
      }
    } catch (currentError) {
      setActionError(currentError instanceof Error ? currentError.message : "操作失败，请稍后重试。");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="page-grid">
      <div className="page-actions">
        <button type="button" className="button-shell" onClick={openCreate}>
          新建密钥
        </button>
        <button
          type="button"
          className="button-shell"
          disabled={!selectedItem}
          onClick={openRotate}
        >
          轮换密钥
        </button>
        <button
          type="button"
          className="button-shell"
          disabled={!selectedItem}
          onClick={openDeactivate}
        >
          停用密钥
        </button>
        {isAdmin ? (
          <button
            type="button"
            className="button-shell button-shell--danger"
            disabled={!selectedItem}
            onClick={openDelete}
          >
            删除密钥
          </button>
        ) : null}
      </div>

      <section className="section-card">
        <div className="section-card__header">
          <h2>API 密钥列表</h2>
          <p>{selectedItem ? `当前选中：${selectedItem.name}` : "当前没有选中密钥"}</p>
        </div>
        <table className="data-table">
          <thead>
            <tr>
              <th>选择</th>
              <th>名称</th>
              <th>租户</th>
              <th>状态</th>
              <th>权限范围</th>
              <th>最近使用</th>
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
                      onClick={() => setSelectedID(item.id)}
                    >
                      选择 {item.name}
                    </button>
                  </td>
                  <td>{item.name}</td>
                  <td>{item.tenant}</td>
                  <td>{item.status}</td>
                  <td>{item.scopes.join(", ")}</td>
                  <td>{item.last_used_at}</td>
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={6} className="table-empty-cell">
                  暂无数据
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      <section aria-label="已选密钥详情" className="section-card api-key-detail-card">
        <div className="section-card__header">
          <div>
            <p className="api-key-detail-card__eyebrow">已选密钥</p>
            <h3>{selectedItem?.name ?? "未选择密钥"}</h3>
          </div>
          <p>{selectedItem ? `${selectedItem.tenant} / ${selectedItem.status}` : "请选择左侧密钥"}</p>
        </div>
        <div className="detail-list">
          <div className="detail-list__row">
            <dt>权限范围</dt>
            <dd>{selectedItem ? selectedItem.scopes.join(" / ") : "-"}</dd>
          </div>
          <div className="detail-list__row">
            <dt>到期时间</dt>
            <dd>{selectedItem?.expires_at ?? selectedSecretForItem?.expires_at ?? "-"}</dd>
          </div>
          <div className="detail-list__row">
            <dt>创建人</dt>
            <dd>{selectedItem?.created_by_user_id || selectedItem?.owner_user_id || "-"}</dd>
          </div>
          <div className="detail-list__row">
            <dt>密钥摘要</dt>
            <dd className="secret-text">
              {selectedSecretForItem?.full_key
                ? selectedSecretForItem.full_key
                : selectedSecretForItem?.masked_key ?? "点击显示完整密钥后加载"}
            </dd>
          </div>
        </div>
        <div className="page-actions">
          <button
            type="button"
            className="button-shell button-shell--primary"
            disabled={!selectedItem || secretLoading}
            onClick={handleRevealSecret}
          >
            {secretLoading ? "加载中..." : "显示完整密钥"}
          </button>
          <button
            type="button"
            className="button-shell"
            disabled={!selectedItem || secretLoading}
            onClick={handleCopySelectedSecret}
          >
            {secretLoading ? "复制中..." : "复制完整密钥"}
          </button>
        </div>
        {secretError ? <p className="form-error">{secretError}</p> : null}
        {selectedCopyNotice ? <p>{selectedCopyNotice}</p> : null}
      </section>

      {actionMode ? (
        <section className="section-card">
          <h3>
            {actionMode === "create"
              ? "新建密钥"
              : actionMode === "rotate"
                ? "轮换密钥"
                : actionMode === "deactivate"
                  ? "停用密钥"
                  : "删除密钥"}
          </h3>
          {actionMode === "create" ? (
            <div className="form-grid">
              {isAdmin ? (
                <label className="field-shell">
                  租户 ID
                  <input value={tenantID} onChange={(event) => setTenantID(event.target.value)} />
                </label>
              ) : null}
              <label className="field-shell">
                名称
                <input value={name} onChange={(event) => setName(event.target.value)} />
              </label>
              <ScopePicker
                label="权限范围"
                buttonLabel="选择权限范围"
                selectedScopes={selectedScopes}
                onToggleScope={toggleScope}
              />
            </div>
          ) : null}
          {actionMode === "rotate" ? (
            <div className="form-grid">
              <label className="field-shell">
                新名称
                <input value={name} onChange={(event) => setName(event.target.value)} />
              </label>
              <ScopePicker
                label="新权限范围"
                buttonLabel="选择新权限范围"
                selectedScopes={selectedScopes}
                onToggleScope={toggleScope}
              />
            </div>
          ) : null}
          {actionMode === "deactivate" && selectedItem ? (
            <p>确认停用 {selectedItem.name} 吗？停用后该密钥将立即失效。</p>
          ) : null}
          {actionMode === "delete" && selectedItem ? (
            <p>确认删除 {selectedItem.name} 吗？仅未被调用历史引用的密钥允许删除。</p>
          ) : null}
          {actionError ? <p className="form-error">{actionError}</p> : null}
          <div className="page-actions">
            <button
              type="button"
              className="button-shell button-shell--primary"
              disabled={submitting}
              onClick={handleConfirm}
            >
              {getSubmitLabel(actionMode, submitting)}
            </button>
            <button
              type="button"
              className="button-shell"
              disabled={submitting}
              onClick={closeActionPanel}
            >
              取消
            </button>
          </div>
        </section>
      ) : null}

      {actionResult || actionNotice ? (
        <section
          ref={actionResultRef}
          aria-label="最近操作结果"
          className="section-card section-card--success"
        >
          <h3>{actionNotice ?? "操作结果"}</h3>
          <p>名称：{actionResult?.item.name}</p>
          <p>状态：{actionResult?.item.status}</p>
          {actionResult?.raw_key ? (
            <div className="api-key-secret">
              <p className="secret-text">一次性密钥：{maskAPIKey(actionResult.raw_key)}</p>
              <button type="button" className="button-shell" onClick={handleCopyRawKey}>
                复制本次完整密钥
              </button>
            </div>
          ) : null}
          {resultCopyNotice ? <p>{resultCopyNotice}</p> : null}
        </section>
      ) : null}

      <section className="section-card">
        <h3>操作说明</h3>
        <p>新建和轮换后页面仅展示脱敏值，请立即复制完整密钥并妥善保存。</p>
      </section>
      <section className="section-card">
        <h3>{isAdmin ? "凭证模式" : "适用范围"}</h3>
        <p>{isAdmin ? data.credential_mode : "当前成员只能管理自己创建且属于当前租户的密钥。"}</p>
      </section>
    </div>
  );
}
