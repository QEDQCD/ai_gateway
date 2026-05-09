import { describe, expect, test } from "vitest";

import { formatRoutingReasonLabel, formatTaskClassLabel } from "./smart-routing";

describe("smart routing 文案格式化", () => {
  test("任务分类转为中文", () => {
    expect(formatTaskClassLabel("simple_qa")).toBe("简单问答");
    expect(formatTaskClassLabel("coding_complex")).toBe("复杂编码请求");
    expect(formatTaskClassLabel("embedding_simple")).toBe("向量检索请求");
  });

  test("路由原因转为中文", () => {
    expect(formatRoutingReasonLabel("model:direct")).toBe("直接指定模型");
    expect(formatRoutingReasonLabel("keyword:debug,pattern:code_fence")).toBe("命中关键词：调试；包含代码块");
    expect(formatRoutingReasonLabel("pattern:stack_trace,signal:soft_keyword_combo")).toBe(
      "包含报错堆栈；命中组合编码信号",
    );
  });
});
