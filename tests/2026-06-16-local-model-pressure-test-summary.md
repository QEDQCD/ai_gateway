# 本地大模型压测报告（2026-06-16）

## 1. 文档说明

本文档基于 `tests/2026-05-08-*` 云端压测经验，针对 **网关背后接入本地部署大模型** 的新场景，给出压测方法、积分模型、脚本使用方式与结果解读模板。

关联文件：

| 类型 | 路径 |
| --- | --- |
| 压测计划 | `tests/2026-06-16-local-model-load-test-plan.md` |
| 压测脚本 | `tests/run_local_model_load_test.py` |
| 启动入口 | `tests/run_local_model_load_test.sh` |
| 单元验证 | `tests/test_local_model_load_test.py` |
| 历史参考 | `tests/2026-05-08-pressure-test-summary.md` |

## 2. 场景定义

```
用户 → AI Gateway → 本地推理服务（vLLM/Ollama/自建）
                      ├─ local-qwen-7b   （cheap，低积分）
                      ├─ local-qwen-14b  （medium）
                      └─ local-qwen-72b  （expensive，高积分）
```

与 2026-05-08 云端压测相比，本轮关注：

- **QPS 上限**：成功请求数 / 秒，随并发阶梯上升直至失败率 > 10%
- **Token 吞吐量上限**：总 tokens / 秒，反映本地 GPU/推理引擎吞吐
- **积分消耗速率**：按模型单价将 token 换算为积分，用于模拟租户配额消耗

## 3. 积分模型

### 3.1 计算公式

与网关 `gateway/internal/service/token_pricing.go` 中 `ComputeUsageCosts` 口径一致：

```
uncached_input = max(prompt_tokens - cached_tokens, 0)

input_cost  = round(uncached_input × input_price_per_million  / 1,000,000)
output_cost = round(completion_tokens × output_price_per_million / 1,000,000)
cached_cost = round(cached_tokens     × cached_price_per_million / 1,000,000)

total_cost_microyuan = input_cost + output_cost + cached_cost
积分 = total_cost_microyuan / 10000    # 默认 1 积分 ≈ 0.01 元
```

### 3.2 模型单价与积分示例

假设单次调用：`prompt_tokens=20`，`completion_tokens=16`，`cached_tokens=0`

| 模型 | 档位 | 输入价 (微元/M) | 输出价 (微元/M) | 单次积分 | 相对倍率 |
| --- | --- | ---: | ---: | ---: | ---: |
| `local-qwen-7b` | cheap | 500,000 | 2,000,000 | **0.042** | 1.0× |
| `local-qwen-14b` | medium | 1,000,000 | 8,000,000 | **0.148** | 3.5× |
| `local-qwen-72b` | expensive | 4,000,000 | 32,000,000 | **0.592** | 14.1× |

同一 token 消耗下，72B 模型单次积分约为 7B 的 **14 倍**，符合“贵模型积分高、便宜模型积分低”的设计目标。

### 3.3 积分在压测中的用途

| 指标 | 含义 |
| --- | --- |
| 单次积分 | 模拟用户一次调用的配额扣减 |
| 积分/s | 系统在高并发下的“计费压力” |
| 累计积分 | 对照 `AGW_POINTS_BUDGET` 自动停止 |
| 分模型总积分 | 对比快慢模型混合负载下的成本结构 |

## 4. 压测方法摘要

### 4.1 并发阶梯

| 阶段 | 并发 | 时长 | 目标 |
| --- | ---: | ---: | --- |
| baseline_c1 | 1 | 15s | RT 基线 |
| ramp_c2 | 2 | 15s | 低并发 QPS |
| ramp_c4 | 4 | 20s | 中低并发 |
| ramp_c8 | 8 | 20s | 中并发 |
| ramp_c16 | 16 | 20s | 高并发 |
| ramp_c24 | 24 | 20s | 冲击上限 |

停止增压条件：连续 2 阶段失败率 > 10%，或触及积分/token 预算。

### 4.2 混合比例

默认快模型:强模型 = **2:1**（可在脚本 `LOAD_TEST_MODELS` 中调整权重）。

### 4.3 执行前检查清单

- [ ] 本地推理服务已启动，GPU 显存充足
- [ ] 网关 provider 已挂载本地 model id，与脚本一致
- [ ] 平台 API Key 有效（非 401）
- [ ] `MODEL_PRICING` 单价已与业务定价对齐
- [ ] `AGW_POINTS_BUDGET` / `AGW_TOKEN_BUDGET` 已设置

### 4.4 执行命令

```bash
# 完整压测
tests/run_local_model_load_test.sh

# 快速冒烟（2 阶段 × 5s）
AGW_QUICK=1 tests/run_local_model_load_test.sh

# 积分计算单元验证
cd tests && python3 test_local_model_load_test.py
```

## 5. 结果解读模板

压测完成后，在 `tests/output/local-model-<timestamp>/` 查看：

- `results.jsonl` — 逐请求明细（含 tokens、积分、延迟）
- `summary.json` — 结构化汇总
- `summary.md` — 可读摘要

## 11. 生产环境指标清单（脚本已支持）

| 类别 | 指标 | 脚本字段 / 输出 |
| --- | --- | --- |
| SLA | 成功率、P50/P95/P99、TTFT | `overall.success_rate`, `p99_latency_s`, `p95_ttft_s` |
| 质量 | 伪成功、路由不一致 | `overall.pseudo_success`, `routing_mismatch` |
| 错误 | 401/429/5xx/超时/空 content | `production.errors_by_category` |
| 容量 | 峰值 QPS、Token/s、积分/s | `overall.peak_*` |
| 治理 | 推荐生产并发、日积分估算 | `recommended_production_concurrency` |
| 对账 | 脚本 vs 网关 usage | `production.usage_reconciliation` |
| 基础设施 | 容器 CPU/内存峰值 | `production.infra.by_service` |
| 健康 | 压测前后 healthz | `production.health_before/after` |

### 5.1 阶段结果表（填写模板）

| 阶段 | 并发 | 请求 | 成功 | 失败率 | QPS | Token/s | 积分/s | P50 | P95 | P99 | SLA |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| baseline_c1 | 1 | | | | | | | | | | |
| ramp_c2 | 2 | | | | | | | | | | |
| ramp_c4 | 4 | | | | | | | | | | |
| ramp_c8 | 8 | | | | | | | | | | |
| ramp_c16 | 16 | | | | | | | | | | |
| ramp_c24 | 24 | | | | | | | | | | |
| soak_steady | 8 | | | | | | | | | | |

### 5.2 错误分类表（填写模板）

| 错误类型 | 次数 | 说明 |
| --- | ---: | --- |
| auth_401 | | 鉴权失败 |
| rate_limit_429 | | 限流 |
| server_5xx | | 网关/上游异常 |
| timeout | | 请求超时 |
| empty_content | | 200 但无 content |
| reasoning_only | | 仅 reasoning 无最终答案 |
| network_error | | 网络/connect 错误 |

### 5.3 分模型积分表（填写模板）

| 模型 | 档位 | 请求 | 成功 | 总 Token | 总积分 | 单次均积分 | 平均延迟 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| local-qwen-7b | cheap | | | | | | |
| local-qwen-72b | expensive | | | | | | |

### 5.4 基础设施峰值表（填写模板）

| 服务 | CPU 峰值 % | 内存峰值 MB | 是否先于模型成为瓶颈 |
| --- | ---: | ---: | --- |
| compose-gateway-1 | | | |
| compose-postgres-1 | | | |
| compose-redis-1 | | | |
| 本地推理服务 | | | 需 nvidia-smi 补充 |

### 5.5 用量对账表（填写模板）

| 项 | 脚本累计 | 网关增量 | 偏差率 | 结论 |
| --- | ---: | ---: | ---: | --- |
| Token | | | | |
| 积分 | | | | |

### 5.6 上限判定规则

| 指标 | 判定方式 |
| --- | --- |
| **QPS 上限** | 最后一个失败率 ≤ 10% 的阶段的成功 QPS；或峰值 QPS |
| **Token 吞吐上限** | 同上阶段的 tokens/s |
| **推荐生产并发** | 上限阶段的 70% 并发（留 headroom） |
| **积分消耗上限** | 峰值积分/s × 86400 ≈ 日配额参考 |

## 6. 与历史云端压测的对照

2026-05-08 云端压测（`qwen-flash` + `mimo-v2.5-pro`）关键结论：

| 模型 | 最高稳定并发 | 峰值 QPS（估算） | 主要瓶颈 |
| --- | --- | --- | --- |
| qwen-flash | 24 并发 | ~106 req/s（2699/25s） | 基本稳定 |
| mimo-v2.5-pro | 12 并发 | ~6 req/s | reasoning 空 content |

本地模型压测预期差异：

| 维度 | 云端 API | 本地部署 |
| --- | --- | --- |
| QPS 上限 | 受上游 rate limit | 受 GPU 算力 / batch |
| 延迟 | 网络 + 上游 |  mainly 推理时间 |
| 成本 | 真实 API 费用 | 积分模拟 + 电费/算力 |
| 失败模式 | 429/5xx/空 content | OOM/超时/队列满 |

## 7. 冒烟验证记录

2026-06-18 在开发环境执行 `AGW_QUICK=1` 冒烟：

| 项 | 结果 |
| --- | --- |
| 脚本 | 正常启动，结果写入 `tests/output/local-model-20260618110643/` |
| 单元测试 | `test_local_model_load_test.py` 通过 |
| 请求 | 309 次全部 HTTP 401 |
| 原因 | 平台 API Key 无效或已轮换 |
| QPS / 积分 | 无法得出有效结论 |

**结论**：脚本与积分逻辑已验证；需在本地模型与有效 API Key 就绪后重新执行完整压测。

## 8. 预期结论格式（完整压测后填写）

### 8.1 本地快模型（local-qwen-7b）

- QPS 上限：___ req/s（阶段 ___）
- Token 吞吐：___ tokens/s
- 单次均积分：___
- 结论：___

### 8.2 本地强模型（local-qwen-72b）

- QPS 上限：___ req/s（阶段 ___）
- Token 吞吐：___ tokens/s
- 单次均积分：___（预期为 7B 的 10×+）
- 结论：___

### 8.3 混合负载建议

- 推荐生产并发：快模型 ___ 路 + 强模型 ___ 路
- 租户日积分配额参考：峰值 ___ 积分/s × 86400 ≈ ___ 积分/天
- 是否建议进入更高并发回归：是 / 否

## 9. 建议下一步

1. 在网关 admin 控制台挂载本地 provider，确认 model id
2. 更新 `tests/run_local_model_load_test.py` 中 `LOAD_TEST_MODELS` 与 `MODEL_PRICING`
3. 轮换有效平台 API Key 后执行完整压测
4. 将 `summary.json` 数据填入本文第 5、8 节模板
5. 若 QPS 受 GPU 限制，考虑 batching / 多卡部署后再回归

## 10. 附录：积分计算代码片段

脚本核心逻辑（与网关一致）：

```python
def compute_points(model, prompt_tokens, completion_tokens, cached_tokens):
    price = MODEL_PRICING[model]
    uncached = max(prompt_tokens - cached_tokens, 0)
    input_cost = round_microyuan_cost(uncached, price["input_microyuan_per_million"])
    output_cost = round_microyuan_cost(completion_tokens, price["output_microyuan_per_million"])
    cached_cost = round_microyuan_cost(cached_tokens, price["cached_microyuan_per_million"])
    total_cost = input_cost + output_cost + cached_cost
    return total_cost / 10000  # 积分
```

单元验证已确认：同 token 下 expensive 模型积分 > cheap 模型；cached token 越多积分越低。
