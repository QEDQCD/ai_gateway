import { useEffect, useState } from "react";

import { ErrorSection, LoadingSection } from "./console";
import {
  createProvider,
  createProviderModel,
  getProviderModels,
  type ProviderModelsPageData,
} from "../lib/console-api";

const emptyData: ProviderModelsPageData = {
  providers: [],
  models: [],
};

function isNonChatProvider(providerItem: ProviderModelsPageData["providers"][number]) {
  const supportedModels = providerItem.supported_models ?? [];
  return supportedModels.length > 0 && !supportedModels.some((item) => item !== "rag-query");
}

type ProviderModelCreateFormProps = {
  onModelCreated?: () => void;
  embedded?: boolean;
  providers?: ProviderModelsPageData["providers"];
};

export function ProviderModelCreateForm({
  onModelCreated,
  embedded = false,
  providers,
}: ProviderModelCreateFormProps) {
  const [refreshKey, setRefreshKey] = useState(0);
  const [providerItems, setProviderItems] = useState<ProviderModelsPageData["providers"]>(providers ?? []);
  const [loading, setLoading] = useState(providers == null);
  const [error, setError] = useState<string | null>(null);
  const [provider, setProvider] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [credentialMode, setCredentialMode] = useState("secret_ref");
  const [secretRef, setSecretRef] = useState("");
  const [apiKey, setAPIKey] = useState("");
  const [requestedModel, setRequestedModel] = useState("");
  const [providerCredentialID, setProviderCredentialID] = useState("");
  const [requestMode, setRequestMode] = useState("聊天");
  const [healthcheckEnabled, setHealthcheckEnabled] = useState(false);
  const [submittingProvider, setSubmittingProvider] = useState(false);
  const [submittingModel, setSubmittingModel] = useState(false);
  const [actionError, setActionError] = useState("");
  const [actionMessage, setActionMessage] = useState("");

  useEffect(() => {
    if (providers) {
      setProviderItems(providers);
      setLoading(false);
      setError(null);
      return;
    }

    let active = true;
    setLoading(true);
    setError(null);

    void getProviderModels()
      .then((next) => {
        if (!active) {
          return;
        }
        setProviderItems(next.providers ?? emptyData.providers);
      })
      .catch((currentError) => {
        if (!active) {
          return;
        }
        setError(currentError instanceof Error ? currentError.message : "加载失败，请稍后重试。");
      })
      .finally(() => {
        if (active) {
          setLoading(false);
        }
      });

    return () => {
      active = false;
    };
  }, [providers, refreshKey]);

  const usesSecretRef = credentialMode === "secret_ref";
  const chatCompatibleProviders = providerItems.filter((item) => !isNonChatProvider(item));
  const rootClassName = embedded
    ? "provider-model-create-form provider-model-create-form--embedded"
    : "page-grid provider-model-create-page provider-model-create-form";

  async function handleCreateProvider() {
    setSubmittingProvider(true);
    setActionError("");
    setActionMessage("");

    try {
      const result = await createProvider({
        provider,
        display_name: displayName,
        base_url: baseURL,
        credential_mode: credentialMode,
        secret_ref: usesSecretRef ? secretRef : "",
        api_key: usesSecretRef ? "" : apiKey,
      });
      setProviderItems((items) => {
        const next = items.filter((item) => item.id !== result.item.id);
        next.push(result.item);
        return next;
      });
      setProviderCredentialID(result.item.id);
      setActionMessage(`上游供应商已创建：${result.item.display_name}`);
      if (!providers) {
        setRefreshKey((value) => value + 1);
      }
    } catch (currentError) {
      setActionError(currentError instanceof Error ? currentError.message : "创建上游供应商失败。");
    } finally {
      setSubmittingProvider(false);
    }
  }

  async function handleCreateModel() {
    setSubmittingModel(true);
    setActionError("");
    setActionMessage("");

    try {
      const result = await createProviderModel({
        requested_model: requestedModel,
        provider_credential_id: providerCredentialID,
        request_mode: requestMode,
        healthcheck_enabled: healthcheckEnabled,
      });
      setActionMessage(`模型已创建：${result.item.requested_model}`);
      if (!providers) {
        setRefreshKey((value) => value + 1);
      }
      onModelCreated?.();
    } catch (currentError) {
      setActionError(currentError instanceof Error ? currentError.message : "创建模型失败。");
    } finally {
      setSubmittingModel(false);
    }
  }

  if (loading) {
    return <LoadingSection text="正在加载模型创建表单..." />;
  }

  if (error) {
    return <ErrorSection message={error} />;
  }

  return (
    <div className={rootClassName}>
      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>创建上游供应商</h2>
            <p>先创建上游供应商凭证，再把聊天模型绑定到该上游。</p>
          </div>
        </div>
        <div className="form-grid">
          <label className="field-shell">
            <span>供应商标识</span>
            <input value={provider} onChange={(event) => setProvider(event.target.value)} />
          </label>
          <label className="field-shell">
            <span>上游名称</span>
            <input value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
          </label>
          <label className="field-shell">
            <span>Base URL</span>
            <input value={baseURL} onChange={(event) => setBaseURL(event.target.value)} />
          </label>
          <label className="field-shell">
            <span>凭证模式</span>
            <select value={credentialMode} onChange={(event) => setCredentialMode(event.target.value)}>
              <option value="secret_ref">密钥引用（secret_ref）</option>
              <option value="encrypted">加密存储（encrypted）</option>
            </select>
          </label>
          {usesSecretRef ? (
            <label className="field-shell">
              <span>密钥引用</span>
              <span className="field-shell__hint">填写环境变量名，供服务启动时读取真实密钥。</span>
              <input value={secretRef} onChange={(event) => setSecretRef(event.target.value)} />
            </label>
          ) : null}
          {usesSecretRef ? null : (
            <label className="field-shell">
              <span>API Key</span>
              <input
                type="password"
                autoComplete="new-password"
                value={apiKey}
                onChange={(event) => setAPIKey(event.target.value)}
              />
            </label>
          )}
        </div>
        <div className="page-actions">
          <button
            type="button"
            className="button-shell button-shell--primary"
            disabled={submittingProvider}
            onClick={() => void handleCreateProvider()}
          >
            创建上游供应商
          </button>
        </div>
      </section>

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>创建模型</h2>
            <p>选择聊天模型可绑定的上游供应商凭证，创建后可立即发起一次健康检查。</p>
          </div>
        </div>
        <div className="form-grid">
          <label className="field-shell">
            <span>请求模型</span>
            <input value={requestedModel} onChange={(event) => setRequestedModel(event.target.value)} />
          </label>
          <label className="field-shell">
            <span>上游供应商凭证</span>
            <select
              value={providerCredentialID}
              onChange={(event) => setProviderCredentialID(event.target.value)}
            >
              <option value="">请选择</option>
              {chatCompatibleProviders.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.display_name} ({item.id})
                </option>
              ))}
            </select>
          </label>
          <label className="field-shell">
            <span>请求模式</span>
            <select value={requestMode} onChange={(event) => setRequestMode(event.target.value)}>
              <option value="聊天">聊天</option>
              <option value="推理">推理</option>
            </select>
          </label>
          <label className="field-shell field-shell--checkbox">
            <input
              className="field-shell__checkbox"
              type="checkbox"
              checked={healthcheckEnabled}
              onChange={(event) => setHealthcheckEnabled(event.target.checked)}
            />
            <span>创建后立即健康检查</span>
          </label>
        </div>
        <div className="page-actions">
          <button
            type="button"
            className="button-shell button-shell--primary"
            disabled={submittingModel}
            onClick={() => void handleCreateModel()}
          >
            创建模型
          </button>
        </div>
        {actionError ? <p>{actionError}</p> : null}
        {actionMessage ? <p>{actionMessage}</p> : null}
      </section>
    </div>
  );
}
