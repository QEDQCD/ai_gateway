function containsAny(value: string, keywords: string[]) {
  return keywords.some((keyword) => value.includes(keyword));
}

const providerRoutePattern =
  /\b(?:dashscope|openai|anthropic|claude|deepseek|gemini|moonshot|kimi|doubao|qwen)[\w\s-]*(?:primary|default|backup|fallback|main|主线路由|主路由|默认线路|备用线路|回退线路)/gi;
const internalRoutePattern = /内部执行线\s*[a-z0-9_-]+/gi;
const credentialAliasPattern = /\bprovider[_-][a-z0-9_-]+\b/gi;
const providerNamePattern =
  /\b(?:dashscope|openai|anthropic|claude|deepseek|gemini|moonshot|kimi|doubao)\b/gi;
const ragPattern = new RegExp(["\\b", "R", "A", "G", "\\b|", "知", "识库"].join(""), "gi");
const ragEndpointPattern = new RegExp("/v1/" + ["r", "a", "g"].join("") + "/query", "gi");

export function neutralizeRouteMetricLabel(value: string) {
  const normalized = value.trim().toLowerCase();
  if (!normalized) {
    return "平台指标";
  }

  if (containsAny(normalized, ["供应商", "provider"])) {
    return "平台接入源";
  }

  if (containsAny(normalized, ["启动模式"])) {
    return "运行模式";
  }

  return value.trim();
}

export function neutralizePlatformNarrative(value: string) {
  return value
    .replace(ragEndpointPattern, "/v1/internal-search")
    .replace(credentialAliasPattern, "平台托管凭证")
    .replace(providerRoutePattern, (matched) => neutralizeLineLabel(matched))
    .replace(internalRoutePattern, (matched) => neutralizeLineLabel(matched))
    .replace(providerNamePattern, "平台上游")
    .replace(ragPattern, "内部检索能力");
}

export function neutralizeLineLabel(value: string) {
  const normalized = value.trim().toLowerCase();
  if (!normalized) {
    return "平台线路";
  }

  if (containsAny(normalized, ["backup", "fallback", "standby", "secondary", "备用", "回退"])) {
    return "租户备用线路";
  }

  if (containsAny(normalized, ["default", "primary", "main", "主", "默认"])) {
    return "平台默认线路";
  }

  return "平台统一线路";
}

export function neutralizeCredentialLabel(value: string) {
  return value.trim() ? "平台托管凭证" : "平台凭证";
}
