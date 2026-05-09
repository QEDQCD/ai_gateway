import { neutralizePlatformNarrative } from "./platform-routing";

const exactReasonLabels: Record<string, string> = {
  reasoning_only: "模型只返回推理过程，未返回最终答案",
  "no non-empty content token received": "模型已返回响应片段，但没有生成可交付的最终答案。",
  "route resolution failed": "平台未找到可用的模型路由，请联系管理员检查模型配置。",
};

export function formatUsageReason(value: string) {
  const normalized = value.trim();
  if (!normalized) {
    return "--";
  }

  const exact = exactReasonLabels[normalized];
  if (exact) {
    return exact;
  }

  const lowered = normalized.toLowerCase();
  if (lowered.includes("reasoning_only")) {
    return "模型只返回推理过程，未返回最终答案";
  }
  if (lowered.includes("no non-empty content token received")) {
    return "模型已返回响应片段，但没有生成可交付的最终答案。";
  }
  if (lowered.includes("route resolution failed")) {
    return "平台未找到可用的模型路由，请联系管理员检查模型配置。";
  }
  if (lowered.includes("context deadline exceeded") || lowered.includes("timeout")) {
    return "上游模型响应超时，请稍后重试。";
  }
  if (lowered.includes("429") && lowered.includes("limit")) {
    return "请求频率超过上游限制，请稍后重试。";
  }
  if (lowered.includes("401") || lowered.includes("403")) {
    return "上游模型鉴权失败，请联系管理员检查凭证配置。";
  }

  return neutralizePlatformNarrative(normalized);
}
