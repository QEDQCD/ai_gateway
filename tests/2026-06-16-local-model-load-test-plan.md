# 本地大模型压测计划（2026-06-16）

## 1. 背景与目标

2026-05-08 已完成针对云端上游模型（`qwen-flash`、`mimo-v2.5-pro`）的保守压测，验证了网关稳定性与 token 预算控制。

本轮场景切换为：**网关背后接入本地部署的大模型**（如 vLLM / Ollama / 自建推理服务），目标从“稳定性基线”升级为：

1. 探测本地模型的 **QPS 上限**（成功请求数 / 秒）
2. 探测本地模型的 **Token 吞吐量上限**（总 tokens / 秒）
3. 建立 **积分计费模型**：按每次调用的 token 消耗与模型单价换算积分，贵模型积分高、便宜模型积分低
4. 在积分 / Token 预算内自动停止，避免压测失控

## 2. 与上一轮压测的差异

| 维度 | 2026-05-08 云端压测 | 2026-06-16 本地模型压测 |
| --- | --- | --- |
| 上游 | DashScope / MIMO 公网 API | 本地推理服务 |
| 主要目标 | 稳定性、空 content 兼容 | QPS / 吞吐量上限 |
| 停止条件 | qwen/mimo token 预算 | 积分预算 + token 预算 + 失败率 |
| 并发策略 | 固定 1→2→4 轻压 | 阶梯增压 1→2→4→8→16→24 |
| 计费 | 仅统计 token | token + 积分（按模型单价） |

## 3. 压测范围

### 覆盖

- `POST /v1/chat/completions` 非流式短回复
- 多模型混合并发（快模型 + 强模型）
- QPS、Token/s、积分/s、P50/P95/P99 延迟
- 分模型积分累计与单次均积分
- **生产级指标**（脚本自动采集）：
  - SLA：成功率、P99 延迟、伪成功率（200 无 content）
  - 错误分类：401/429/5xx/超时/reasoning_only/empty_content
  - 路由一致性：请求 model vs 返回 model
  - TTFT（流式模式）
  - 基础设施采样：`docker stats`（gateway/postgres/redis 等）
  - 用量对账：脚本累计 vs 网关 `/admin/usage/overview`（可选）
  - 推荐生产并发：峰值可接受阶段的 70%
  - 稳态 soak：`AGW_SOAK=1` 追加 30 分钟阶段

### 不覆盖

- 长上下文、流式长输出（除非显式 `AGW_STREAM=true`）
- embedding / RAG / 多租户隔离极限（需独立 tenant/key 压测）
- GPU 显存细粒度监控（需 nvidia-smi / Prometheus 补充）

## 4. 积分模型

积分口径与网关 `ComputeUsageCosts` 保持一致，单位为 **微元 / 百万 Token**：

```
input_cost  = round(input_tokens_uncached × input_price  / 1e6)
output_cost = round(output_tokens         × output_price / 1e6)
cached_cost = round(cached_tokens         × cached_price / 1e6)
total_cost  = input_cost + output_cost + cached_cost
积分        = total_cost / POINTS_DIVISOR
```

默认 `POINTS_DIVISOR = 10000`，即 **1 积分 ≈ 0.01 元**。

### 模型单价示例（可在脚本中调整）

| 模型 | 档位 | 输入 (微元/M) | 输出 (微元/M) | 缓存 (微元/M) |
| --- | --- | ---: | ---: | ---: |
| `local-qwen-7b` | cheap | 500,000 | 2,000,000 | 100,000 |
| `local-qwen-14b` | medium | 1,000,000 | 8,000,000 | 250,000 |
| `local-qwen-72b` | expensive | 4,000,000 | 32,000,000 | 1,000,000 |

同一输入输出 token 数下，`local-qwen-72b` 单次积分显著高于 `local-qwen-7b`。

## 5. 压测方法

### 5.1 前置条件

1. 网关已启动：`docker compose ... up -d`
2. 本地模型已在网关 provider 中挂载，model id 与脚本 `LOAD_TEST_MODELS` 一致
3. 平台 API Key 可用：`~/.ai_gateway_secrets/gateway_seed_platform_api_key`
4. 修改 `tests/run_local_model_load_test.py` 中的模型名与单价表

### 5.2 并发阶梯

| 阶段 | 并发 | 时长 | 目的 |
| --- | ---: | ---: | --- |
| baseline_c1 | 1 | 15s | 单路 RT 基线 |
| ramp_c2 | 2 | 15s | 低并发 |
| ramp_c4 | 4 | 20s | 中低并发 |
| ramp_c8 | 8 | 20s | 中并发 |
| ramp_c16 | 16 | 20s | 高并发 |
| ramp_c24 | 24 | 20s | 冲击上限 |

混合比例默认：快模型 2 : 强模型 1。

### 5.3 请求参数

```json
{
  "model": "<local-model-id>",
  "messages": [{"role": "user", "content": "你好，请用一句话回答。"}],
  "max_tokens": 32
}
```

### 5.4 停止条件

满足任一条件立即停止：

1. 累计积分 ≥ `AGW_POINTS_BUDGET`（默认 50000）
2. 累计 token ≥ `AGW_TOKEN_BUDGET`（默认 500000）
3. 连续 2 个阶段失败率 > 10%
4. 本地 GPU/推理服务 OOM 或网关 5xx 持续

## 6. 观测指标

### 6.1 业务与 SLA

1. **QPS**、**Token/s**、**积分/s**
2. **P50 / P95 / P99 延迟**、**TTFT**（流式）
3. **成功率 / 伪成功率**、**路由一致性**

### 6.2 错误与治理

4. **错误分类**：401/429/5xx/timeout/empty_content/reasoning_only
5. **用量对账**（`AGW_USAGE_VERIFY=1`）
6. **推荐生产并发**、**日积分估算**

### 6.3 基础设施

7. **docker stats 采样**（gateway/postgres/redis）
8. **healthz 压测前后对比**

详见报告 `tests/2026-06-16-local-model-pressure-test-summary.md` 第 11 节指标清单。

## 7. 执行命令

```bash
# 完整压测
tests/run_local_model_load_test.sh

# 快速冒烟（5s × 2 阶段，仅 qwen-flash）
AGW_QUICK=1 tests/run_local_model_load_test.sh

# 生产级：开启 usage 对账 + 30 分钟 soak
AGW_USAGE_VERIFY=1 AGW_SOAK=1 \
  GATEWAY_SERVICE_AUTH_USERNAME=example-console-user \
  GATEWAY_SERVICE_AUTH_PASSWORD=... \
  GATEWAY_CONSOLE_ADMIN_PASSWORD=... \
  tests/run_local_model_load_test.sh

# 单元验证（积分计算）
cd tests && python3 test_local_model_load_test.py
```

结果输出至 `tests/output/local-model-<timestamp>/`：

- `results.jsonl`：逐请求明细
- `summary.json`：结构化汇总
- `summary.md`：可读摘要

## 8. 通过标准

1. 至少完成 baseline + ramp_c2 两个阶段
2. 快模型阶段成功率 ≥ 95%
3. P99 延迟 ≤ `AGW_P99_SLA_THRESHOLD`（默认 8s）
4. 伪成功率 = 0（或已知兼容问题已文档化）
5. 能明确给出峰值 QPS、Token/s、积分/s
6. 积分统计与网关 usage 偏差 ≤ 15%（开启对账时）
7. 未突破积分 / token 预算
8. 压测后 gateway healthz 仍为 ok

## 9. 风险点

| 风险 | 说明 | 应对 |
| --- | --- | --- |
| 本地 GPU 饱和 | 并发升高后延迟陡增、超时 | 降低 `LOAD_STAGES` 上限 |
| 模型名不一致 | 脚本 model id 与网关路由不匹配 | 压测前改 `LOAD_TEST_MODELS` |
| OOM | 72B 模型多并发 | 强模型权重设为 1，快模型优先 |
| 积分口径偏差 | 脚本单价与网关 env 不一致 | 对齐 `MODEL_PRICING` 与 `.env.local` |

## 10. 产出物

- 计划：本文档
- 脚本：`tests/run_local_model_load_test.py`、`tests/run_local_model_load_test.sh`
- 报告：`tests/2026-06-16-local-model-pressure-test-summary.md`
