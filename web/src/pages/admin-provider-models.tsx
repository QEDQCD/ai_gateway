import { useEffect, useRef, useState } from "react";

import { ProviderModelCreateForm } from "../components/provider-model-create-form";
import { DataTable, ErrorSection, LoadingSection, StatCard } from "../components/console";
import { getProviderModels, type ProviderModelsPageData } from "../lib/console-api";
import { neutralizeLineLabel } from "../lib/platform-routing";
import { useRemoteData } from "../lib/use-remote-data";

function formatValue(value: string | number | undefined) {
  if (value == null) {
    return "-";
  }

  if (typeof value === "string") {
    return value || "-";
  }

  return String(value);
}

const emptyData: ProviderModelsPageData = {
  providers: [],
  models: [],
};

export function AdminProviderModelsPage() {
  const [refreshKey, setRefreshKey] = useState(0);
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const modalRef = useRef<HTMLElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  const { data, loading, error } = useRemoteData(() => getProviderModels(), [refreshKey]);
  const pageData = data ?? emptyData;

  useEffect(() => {
    if (!createModalOpen) {
      return;
    }

    closeButtonRef.current?.focus();

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setCreateModalOpen(false);
        return;
      }

      if (event.key !== "Tab") {
        return;
      }

      const modalElement = modalRef.current;
      if (!modalElement) {
        return;
      }

      const focusableElements = Array.from(
        modalElement.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
        ),
      ).filter((item) => !item.hasAttribute("disabled"));

      if (focusableElements.length === 0) {
        return;
      }

      const firstElement = focusableElements[0];
      const lastElement = focusableElements[focusableElements.length - 1];
      const activeElement = document.activeElement as HTMLElement | null;

      if (event.shiftKey) {
        if (activeElement == null || activeElement === firstElement || !modalElement.contains(activeElement)) {
          event.preventDefault();
          lastElement.focus();
        }
        return;
      }

      if (activeElement == null || activeElement === lastElement || !modalElement.contains(activeElement)) {
        event.preventDefault();
        firstElement.focus();
      }
    };

    document.addEventListener("keydown", handleKeyDown);

    return () => {
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [createModalOpen]);

  function handleModelCreated() {
    setCreateModalOpen(false);
    setRefreshKey((value) => value + 1);
  }

  if (loading) {
    return <LoadingSection text="正在加载后台模型..." />;
  }

  if (error) {
    return <ErrorSection message={error} />;
  }

  const activeProviders = pageData.providers.filter((item) => item.status === "active").length;
  const healthyModels = pageData.models.filter((item) => item.health_status === "healthy").length;

  return (
    <div className="page-grid">
      <div className="stats-grid">
        <StatCard label="Provider 数量" value={String(pageData.providers.length)} />
        <StatCard label="启用 Provider" value={String(activeProviders)} />
        <StatCard label="模型数量" value={String(pageData.models.length)} />
        <StatCard label="健康模型" value={String(healthyModels)} />
      </div>

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>Provider 列表</h2>
            <p>当前可用的 provider 凭证。</p>
          </div>
          <button
            type="button"
            className="button-shell button-shell--primary"
            onClick={() => setCreateModalOpen(true)}
          >
            新建模型
          </button>
        </div>
        <DataTable
          columns={["ID", "Provider", "显示名", "凭证模式", "密钥引用", "状态"]}
          rows={pageData.providers.map((item) => [
            item.id,
            item.provider,
            item.display_name,
            item.credential_mode,
            formatValue(item.secret_ref),
            item.status,
          ])}
        />
      </section>

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>模型挂载</h2>
            <p>聊天模型到 provider 凭证的映射关系。</p>
          </div>
        </div>
        <DataTable
          columns={["请求模型", "Provider", "凭证 ID", "线路", "模式", "健康状态", "延迟"]}
          rows={pageData.models.map((item) => [
            item.requested_model,
            item.provider,
            item.provider_credential_id,
            neutralizeLineLabel(item.route_label),
            item.request_mode,
            item.health_status,
            item.latency_ms > 0 ? `${item.latency_ms} ms` : "-",
          ])}
        />
      </section>

      {createModalOpen ? (
        <div className="modal-backdrop" role="presentation">
          <section
            ref={modalRef}
            className="modal-card modal-card--wide provider-model-create-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="provider-model-create-modal-title"
          >
            <div className="modal-card__header">
              <div>
                <span className="modal-card__eyebrow">后台模型</span>
                <h3 id="provider-model-create-modal-title">新建模型</h3>
                <p>先创建上游供应商凭证，再创建聊天模型挂载关系。</p>
              </div>
              <button
                ref={closeButtonRef}
                type="button"
                className="button-shell"
                onClick={() => setCreateModalOpen(false)}
              >
                关闭
              </button>
            </div>
            <ProviderModelCreateForm
              embedded
              providers={pageData.providers}
              onModelCreated={handleModelCreated}
            />
          </section>
        </div>
      ) : null}
    </div>
  );
}
