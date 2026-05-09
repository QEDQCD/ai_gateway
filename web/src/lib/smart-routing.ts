const taskClassLabels: Record<string, string> = {
  coding_complex: "复杂编码请求",
  simple_qa: "简单问答",
  embedding_simple: "向量检索请求",
  explicit_model: "显式指定模型",
};

const modelTierLabels: Record<string, string> = {
  "gateway-chat-reasoning": "强模型档位",
  "gateway-chat-fast": "快模型档位",
  "gateway-public": "公共网关入口",
};

const routingPatternLabels: Record<string, string> = {
  code_fence: "包含代码块",
  stack_trace: "包含报错堆栈",
  direct_artifact_request: "明确要求产出代码或技术产物",
};

const routingSignalLabels: Record<string, string> = {
  long_prompt: "提示词较长",
  soft_keyword_combo: "命中组合编码信号",
};

const routingKeywordLabels: Record<string, string> = {
  debug: "调试",
  "报错": "报错",
  "异常": "异常",
  "写代码": "写代码",
  "单元测试": "单元测试",
  "架构设计": "架构设计",
  "实现": "实现",
  "重构": "重构",
};

export function formatTaskClassLabel(value: string) {
  if (!value) {
    return "--";
  }
  return taskClassLabels[value] ?? value;
}

export function formatTargetModelTierLabel(value: string) {
  if (!value) {
    return "--";
  }
  return modelTierLabels[value] ?? value;
}

export function formatRoutingReasonLabel(value: string) {
  if (!value) {
    return "--";
  }

  const explicitModelMatch = value.match(/^explicit_model:(.+)$/);
  if (explicitModelMatch) {
    return `直接指定模型：${explicitModelMatch[1]}`;
  }

  const translatedParts = value
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean)
    .map((part) => {
      const [prefix, rawDetail] = part.split(":", 2);
      const detail = rawDetail?.trim() ?? "";
      switch (prefix) {
        case "keyword":
          return detail ? `命中关键词：${routingKeywordLabels[detail] ?? detail}` : "命中关键词";
        case "pattern":
          return routingPatternLabels[detail] ?? (detail ? `命中特征：${detail}` : "命中特征");
        case "signal":
          return routingSignalLabels[detail] ?? (detail ? `命中信号：${detail}` : "命中信号");
        case "model":
          if (detail === "direct") {
            return "直接指定模型";
          }
          return detail ? `模型策略：${detail}` : "模型策略";
        default:
          return part;
      }
    });

  return translatedParts.length > 0 ? translatedParts.join("；") : value;
}
