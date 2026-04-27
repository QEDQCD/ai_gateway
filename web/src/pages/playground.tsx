import { useEffect, useState, type FormEvent } from "react";

import { DetailList, ErrorSection, LoadingSection } from "../components/console";
import {
  getPlayground,
  runPlayground,
  type PlaygroundRunResponse,
} from "../lib/console-api";
import { neutralizeLineLabel } from "../lib/platform-routing";
import { useRemoteData } from "../lib/use-remote-data";

const DEFAULT_PROMPT = "请总结最近一次发布。";

export function PlaygroundPage() {
  const { data, loading, error } = useRemoteData(getPlayground);
  const [model, setModel] = useState("");
  const [prompt, setPrompt] = useState(DEFAULT_PROMPT);
  const [lastRun, setLastRun] = useState<PlaygroundRunResponse | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!data) {
      return;
    }

    setLastRun(data.last_run ?? null);
    setModel((current) => current || data.available_models[0] || "");
  }, [data]);

  if (loading) {
    return <LoadingSection text="正在加载接口验证配置..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "调试场数据加载失败。"} />;
  }

  const selectedModel = model || data.available_models[0] || "";
  const currentRun = lastRun ?? data.last_run ?? null;
  const visibleRouteLabel = currentRun ? neutralizeLineLabel(currentRun.route_label) : "";

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    setSubmitError(null);

    try {
      const response = await runPlayground({ model: selectedModel, prompt });
      setLastRun(response);
    } catch (error) {
      setSubmitError(error instanceof Error ? error.message : "请求提交失败。");
    } finally {
      setSubmitting(false);
    }
  }

  function handleReset() {
    setPrompt(DEFAULT_PROMPT);
    setModel(data.available_models[0] || "");
    setSubmitError(null);
  }

  return (
    <div className="page-grid">
      <div className="two-column-grid">
        <section className="section-card">
          <h2>接口验证</h2>
          <form className="form-grid" onSubmit={handleSubmit}>
            <label className="field-shell">
              <span>模型</span>
              <select value={selectedModel} onChange={(event) => setModel(event.target.value)}>
                {data.available_models.map((item) => (
                  <option key={item} value={item}>
                    {item}
                  </option>
                ))}
              </select>
            </label>
            <label className="field-shell">
              <span>提示词</span>
              <textarea
                rows={8}
                value={prompt}
                onChange={(event) => setPrompt(event.target.value)}
              />
            </label>
            {submitError ? <p className="form-error">{submitError}</p> : null}
            <div className="page-actions">
              <button
                type="submit"
                className="button-shell button-shell--primary"
                disabled={submitting || !selectedModel}
              >
                {submitting ? "提交中..." : "提交请求"}
              </button>
              <button type="button" className="button-shell" onClick={handleReset}>
                重置表单
              </button>
            </div>
          </form>
        </section>
        <section className="section-card">
          <h3>最近一次执行结果</h3>
          <DetailList
            items={
              currentRun
                ? [
                    { label: "平台路由结果", value: visibleRouteLabel },
                    { label: "执行端点", value: currentRun.endpoint },
                    { label: "延迟", value: currentRun.latency },
                    { label: "状态", value: currentRun.status },
                    { label: "响应内容", value: currentRun.response },
                  ]
                : []
            }
          />
        </section>
      </div>
      <section className="section-card">
        <h3>执行元数据</h3>
        <DetailList
          items={
            currentRun
              ? [
                  { label: "平台密钥", value: currentRun.platform_key },
                  { label: "处理链路", value: visibleRouteLabel },
                  { label: "执行端点", value: currentRun.endpoint },
                ]
              : []
          }
        />
      </section>
    </div>
  );
}
