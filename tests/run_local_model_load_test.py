#!/usr/bin/env python3
"""本地大模型压测脚本：探测 QPS / Token 吞吐量上限，并按模型计价生成积分。

生产环境扩展指标：
- SLA：P50/P95/P99 延迟、TTFT、成功率、伪成功率（200 但无 content）
- 错误分类：401/429/5xx/超时/reasoning_only/empty_content
- 路由一致性：请求 model vs 返回 model
- 基础设施采样：docker stats（gateway/postgres/redis 等）
- 用量对账：脚本累计 vs 网关 admin usage API（可选）
"""

from __future__ import annotations

import base64
import json
import math
import os
import statistics
import subprocess
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

# ===== 固定配置（按需修改） =====
GATEWAY_URL = os.environ.get("AGW_URL", "http://127.0.0.1:32658/v1/chat/completions")
GATEWAY_BASE_URL = os.environ.get("AGW_BASE_URL", GATEWAY_URL.rsplit("/v1/", 1)[0])
API_KEY_FILE = Path(os.environ.get("AGW_API_KEY_FILE", "/root/.ai_gateway_secrets/gateway_seed_platform_api_key"))
REQUEST_TIMEOUT_S = int(os.environ.get("AGW_REQUEST_TIMEOUT", "120"))
PROMPT = os.environ.get("AGW_LOAD_TEST_PROMPT", "你好，请用一句话回答。")
MAX_TOKENS = int(os.environ.get("AGW_SHORT_MAX_TOKENS", "32"))
STREAM = os.environ.get("AGW_STREAM", "false").lower() == "true"
DRY_RUN = os.environ.get("AGW_DRY_RUN", "0") == "1"
LOAD_TEST_MARKER = os.environ.get("AGW_LOAD_TEST_MARKER", "local-model-load-test")

# 生产观测开关
ENABLE_INFRA_SAMPLE = os.environ.get("AGW_INFRA_SAMPLE", "1") == "1"
INFRA_SAMPLE_INTERVAL_S = int(os.environ.get("AGW_INFRA_SAMPLE_INTERVAL", "5"))
INFRA_SERVICES = os.environ.get(
    "AGW_INFRA_SERVICES",
    "compose-gateway-1,compose-postgres-1,compose-redis-1,compose-rabbitmq-1,compose-internal-search-1",
).split(",")

ENABLE_USAGE_VERIFY = os.environ.get("AGW_USAGE_VERIFY", "0") == "1"
GATEWAY_SERVICE_AUTH_USERNAME = os.environ.get("GATEWAY_SERVICE_AUTH_USERNAME", "")
GATEWAY_SERVICE_AUTH_PASSWORD = os.environ.get("GATEWAY_SERVICE_AUTH_PASSWORD", "")
GATEWAY_ADMIN_EMAIL = os.environ.get("GATEWAY_ADMIN_EMAIL", "admin@example.com")
GATEWAY_ADMIN_PASSWORD = os.environ.get("GATEWAY_CONSOLE_ADMIN_PASSWORD", "")

PRODUCTION_HEADROOM_RATIO = float(os.environ.get("AGW_PRODUCTION_HEADROOM", "0.7"))
P99_SLA_THRESHOLD_S = float(os.environ.get("AGW_P99_SLA_THRESHOLD", "8.0"))

# 积分换算：10000 微元 = 1 积分（即 1 积分 ≈ 0.01 元）
POINTS_DIVISOR = int(os.environ.get("AGW_POINTS_DIVISOR", "10000"))

# 模型积分单价（微元 / 百万 Token），贵的模型积分更高
MODEL_PRICING: dict[str, dict[str, int]] = {
    "local-qwen-7b": {
        "label": "本地快模型（7B）",
        "tier": "cheap",
        "input_microyuan_per_million": 500_000,
        "output_microyuan_per_million": 2_000_000,
        "cached_microyuan_per_million": 100_000,
    },
    "local-qwen-14b": {
        "label": "本地均衡模型（14B）",
        "tier": "medium",
        "input_microyuan_per_million": 1_000_000,
        "output_microyuan_per_million": 8_000_000,
        "cached_microyuan_per_million": 250_000,
    },
    "local-qwen-72b": {
        "label": "本地强模型（72B）",
        "tier": "expensive",
        "input_microyuan_per_million": 4_000_000,
        "output_microyuan_per_million": 32_000_000,
        "cached_microyuan_per_million": 1_000_000,
    },
    # 兼容当前网关已挂载模型名；本地部署时请改成实际 model id
    "qwen-flash": {
        "label": "快模型档位",
        "tier": "medium",
        "input_microyuan_per_million": 2_000_000,
        "output_microyuan_per_million": 20_000_000,
        "cached_microyuan_per_million": 500_000,
    },
    "mimo-v2.5-pro": {
        "label": "强模型档位",
        "tier": "expensive",
        "input_microyuan_per_million": 4_000_000,
        "output_microyuan_per_million": 32_000_000,
        "cached_microyuan_per_million": 1_000_000,
    },
}

DEFAULT_PRICING = {
    "label": "默认",
    "tier": "medium",
    "input_microyuan_per_million": 2_000_000,
    "output_microyuan_per_million": 20_000_000,
    "cached_microyuan_per_million": 500_000,
}

# 压测模型列表：(model_name, 并发权重)
LOAD_TEST_MODELS: list[tuple[str, int]] = [
    ("local-qwen-7b", 2),
    ("local-qwen-72b", 1),
]

# 并发阶梯：(阶段名, 每模型并发数, 持续秒数)
LOAD_STAGES: list[tuple[str, int, int]] = [
    ("baseline_c1", 1, 15),
    ("ramp_c2", 2, 15),
    ("ramp_c4", 4, 20),
    ("ramp_c8", 8, 20),
    ("ramp_c16", 16, 20),
    ("ramp_c24", 24, 20),
]

# 停止条件
MAX_FAILURE_RATE = 0.10
MAX_CONSECUTIVE_BAD_STAGES = 2
POINTS_BUDGET = int(os.environ.get("AGW_POINTS_BUDGET", "50000"))
TOKEN_BUDGET = int(os.environ.get("AGW_TOKEN_BUDGET", "500000"))

if os.environ.get("AGW_QUICK", "0") == "1":
    LOAD_TEST_MODELS = [("qwen-flash", 1)]
    LOAD_STAGES = [
        ("baseline_c1", 1, 5),
        ("ramp_c2", 2, 5),
    ]
    POINTS_BUDGET = 1000
    TOKEN_BUDGET = 10000

if os.environ.get("AGW_SOAK", "0") == "1":
    LOAD_STAGES = LOAD_STAGES + [("soak_steady", 8, 1800)]

ROOT_DIR = Path(__file__).resolve().parent.parent
OUTPUT_DIR = ROOT_DIR / "tests" / "output"


@dataclass
class RequestResult:
    phase: str
    model: str
    tier: str
    success: bool
    http_code: int
    latency_s: float
    ttft_s: float
    prompt_tokens: int
    completion_tokens: int
    cached_tokens: int
    total_tokens: int
    points: float
    outcome: str
    error_category: str
    returned_model: str = ""
    routing_mismatch: bool = False
    is_pseudo_success: bool = False
    error_message: str = ""
    created_at: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat())


@dataclass
class StageStats:
    phase: str
    concurrency: int
    duration_s: float
    requests: int = 0
    success: int = 0
    failures: int = 0
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0
    total_points: float = 0.0
    latencies: list[float] = field(default_factory=list)

    @property
    def failure_rate(self) -> float:
        return self.failures / self.requests if self.requests else 0.0

    @property
    def qps(self) -> float:
        return self.success / self.duration_s if self.duration_s else 0.0

    @property
    def tokens_per_sec(self) -> float:
        return self.total_tokens / self.duration_s if self.duration_s else 0.0

    @property
    def points_per_sec(self) -> float:
        return self.total_points / self.duration_s if self.duration_s else 0.0

    def latency_percentile(self, p: float) -> float:
        if not self.latencies:
            return 0.0
        ordered = sorted(self.latencies)
        idx = min(len(ordered) - 1, max(0, math.ceil(p / 100 * len(ordered)) - 1))
        return ordered[idx]


@dataclass
class InfraSample:
    captured_at: str
    service: str
    cpu_percent: float
    memory_usage_mb: float
    memory_limit_mb: float
    net_io: str
    block_io: str


def classify_error(http_code: int, outcome: str, error_message: str, timed_out: bool) -> str:
    if timed_out:
        return "timeout"
    if outcome == "reasoning_only":
        return "reasoning_only"
    if outcome == "empty_or_error" and http_code == 200:
        return "empty_content"
    if http_code == 401:
        return "auth_401"
    if http_code == 403:
        return "auth_403"
    if http_code == 429:
        return "rate_limit_429"
    if 500 <= http_code < 600:
        return "server_5xx"
    if http_code == 0 and error_message:
        lower = error_message.lower()
        if "timed out" in lower or "timeout" in lower:
            return "timeout"
        return "network_error"
    if outcome == "dry_run":
        return "dry_run"
    return "other_error"


def is_pseudo_success(http_code: int, outcome: str) -> bool:
    return http_code == 200 and outcome in {"reasoning_only", "empty_or_error"}


def routing_mismatch(requested: str, returned: str) -> bool:
    if not returned:
        return False
    return requested.strip() != returned.strip()


def percentile(values: list[float], p: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    idx = min(len(ordered) - 1, max(0, math.ceil(p / 100 * len(ordered)) - 1))
    return ordered[idx]


def load_api_key() -> str:
    if env_key := os.environ.get("AGW_API_KEY", "").strip():
        return env_key
    if not API_KEY_FILE.is_file():
        raise FileNotFoundError(f"找不到 API Key 文件：{API_KEY_FILE}")
    return API_KEY_FILE.read_text(encoding="utf-8").strip()


def pricing_for(model: str) -> dict[str, Any]:
    return MODEL_PRICING.get(model, DEFAULT_PRICING)


def round_microyuan_cost(tokens: int, price_per_million: int) -> int:
    if tokens <= 0 or price_per_million <= 0:
        return 0
    return (tokens * price_per_million + 500_000) // 1_000_000


def compute_points(model: str, prompt_tokens: int, completion_tokens: int, cached_tokens: int) -> tuple[float, dict[str, int]]:
    price = pricing_for(model)
    input_tokens = max(prompt_tokens, 0)
    cached = max(min(cached_tokens, input_tokens), 0)
    uncached_input = max(input_tokens - cached, 0)

    input_cost = round_microyuan_cost(uncached_input, price["input_microyuan_per_million"])
    output_cost = round_microyuan_cost(completion_tokens, price["output_microyuan_per_million"])
    cached_cost = round_microyuan_cost(cached, price["cached_microyuan_per_million"])
    total_cost = input_cost + output_cost + cached_cost
    points = total_cost / POINTS_DIVISOR
    return points, {
        "input_cost_microyuan": input_cost,
        "output_cost_microyuan": output_cost,
        "cached_cost_microyuan": cached_cost,
        "total_cost_microyuan": total_cost,
    }


def parse_non_stream(body: str) -> dict[str, Any]:
    data = json.loads(body)
    usage = data.get("usage") or {}
    message = ((data.get("choices") or [{}])[0].get("message") or {})
    content = message.get("content") or ""
    reasoning = message.get("reasoning_content") or ""
    return {
        "returned_model": data.get("model") or "",
        "prompt_tokens": int(usage.get("prompt_tokens") or 0),
        "completion_tokens": int(usage.get("completion_tokens") or 0),
        "total_tokens": int(usage.get("total_tokens") or 0),
        "cached_tokens": int(usage.get("cached_tokens") or 0),
        "content": content,
        "reasoning": reasoning,
        "finish_reason": ((data.get("choices") or [{}])[0].get("finish_reason") or ""),
        "ttft_s": 0.0,
    }


def parse_stream(body: str) -> dict[str, Any]:
    prompt_tokens = 0
    completion_tokens = 0
    total_tokens = 0
    cached_tokens = 0
    content_parts: list[str] = []
    reasoning_parts: list[str] = []
    returned_model = ""
    finish_reason = ""
    ttft_s = 0.0
    stream_started = time.perf_counter()
    first_token_seen = False

    for raw_line in body.splitlines():
        if not raw_line.startswith("data:"):
            continue
        chunk_text = raw_line[5:].strip()
        if chunk_text == "[DONE]":
            continue
        try:
            chunk = json.loads(chunk_text)
        except json.JSONDecodeError:
            continue
        if chunk.get("model"):
            returned_model = chunk["model"]
        usage = chunk.get("usage") or {}
        if usage.get("prompt_tokens") is not None:
            prompt_tokens = int(usage["prompt_tokens"])
        if usage.get("completion_tokens") is not None:
            completion_tokens = int(usage["completion_tokens"])
        if usage.get("total_tokens") is not None:
            total_tokens = int(usage["total_tokens"])
        if usage.get("cached_tokens") is not None:
            cached_tokens = int(usage["cached_tokens"])
        choice = (chunk.get("choices") or [{}])[0]
        delta = choice.get("delta") or {}
        chunk_content = delta.get("content") or ""
        chunk_reasoning = (
            delta.get("reasoning_content")
            or choice.get("reasoning_content")
            or chunk.get("reasoning_content")
            or ""
        )
        if not first_token_seen and (chunk_content or chunk_reasoning):
            ttft_s = time.perf_counter() - stream_started
            first_token_seen = True
        content_parts.append(chunk_content)
        reasoning_parts.append(chunk_reasoning)
        if choice.get("finish_reason"):
            finish_reason = choice["finish_reason"]

    return {
        "returned_model": returned_model,
        "prompt_tokens": prompt_tokens,
        "completion_tokens": completion_tokens,
        "total_tokens": total_tokens,
        "cached_tokens": cached_tokens,
        "content": "".join(content_parts),
        "reasoning": "".join(reasoning_parts),
        "finish_reason": finish_reason,
        "ttft_s": ttft_s,
    }


def call_gateway(phase: str, model: str, api_key: str) -> RequestResult:
    price = pricing_for(model)
    if DRY_RUN:
        return RequestResult(
            phase=phase,
            model=model,
            tier=price["tier"],
            success=False,
            http_code=0,
            latency_s=0.0,
            ttft_s=0.0,
            prompt_tokens=0,
            completion_tokens=0,
            cached_tokens=0,
            total_tokens=0,
            points=0.0,
            outcome="dry_run",
            error_category="dry_run",
        )

    payload = {
        "model": model,
        "messages": [{"role": "user", "content": PROMPT}],
        "max_tokens": MAX_TOKENS,
    }
    if STREAM:
        payload["stream"] = True

    req = urllib.request.Request(
        GATEWAY_URL,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
            "X-Load-Test": LOAD_TEST_MARKER,
        },
        method="POST",
    )

    started = time.perf_counter()
    http_code = 0
    error_message = ""
    timed_out = False
    parsed: dict[str, Any] = {
        "prompt_tokens": 0,
        "completion_tokens": 0,
        "total_tokens": 0,
        "cached_tokens": 0,
        "content": "",
        "reasoning": "",
        "returned_model": "",
        "ttft_s": 0.0,
    }

    try:
        with urllib.request.urlopen(req, timeout=REQUEST_TIMEOUT_S) as resp:
            http_code = resp.getcode()
            body = resp.read().decode("utf-8", errors="replace")
            parsed = parse_stream(body) if STREAM else parse_non_stream(body)
    except urllib.error.HTTPError as exc:
        http_code = exc.code
        error_message = exc.read().decode("utf-8", errors="replace")[:500]
    except TimeoutError:
        timed_out = True
        error_message = "request timed out"
    except Exception as exc:  # noqa: BLE001 - 压测脚本需要捕获全部请求错误
        error_message = str(exc)
        timed_out = "timed out" in error_message.lower()

    latency_s = time.perf_counter() - started
    content = parsed.get("content") or ""
    reasoning = parsed.get("reasoning") or ""
    returned_model = parsed.get("returned_model") or ""

    if http_code == 200 and content:
        outcome = "content"
        success = True
    elif http_code == 200 and reasoning:
        outcome = "reasoning_only"
        success = False
    else:
        outcome = "empty_or_error"
        success = False

    error_category = classify_error(http_code, outcome, error_message, timed_out)
    pseudo_success = is_pseudo_success(http_code, outcome)
    mismatch = routing_mismatch(model, returned_model)

    points, _ = compute_points(
        model,
        int(parsed.get("prompt_tokens") or 0),
        int(parsed.get("completion_tokens") or 0),
        int(parsed.get("cached_tokens") or 0),
    )

    return RequestResult(
        phase=phase,
        model=model,
        tier=price["tier"],
        success=success,
        http_code=http_code,
        latency_s=latency_s,
        ttft_s=float(parsed.get("ttft_s") or 0.0),
        prompt_tokens=int(parsed.get("prompt_tokens") or 0),
        completion_tokens=int(parsed.get("completion_tokens") or 0),
        cached_tokens=int(parsed.get("cached_tokens") or 0),
        total_tokens=int(parsed.get("total_tokens") or 0),
        points=points,
        outcome=outcome,
        error_category=error_category,
        returned_model=returned_model,
        routing_mismatch=mismatch,
        is_pseudo_success=pseudo_success,
        error_message=error_message,
    )


class BudgetGuard:
    def __init__(self) -> None:
        self.lock = threading.Lock()
        self.total_tokens = 0
        self.total_points = 0.0
        self.stop_reason = ""

    def add(self, result: RequestResult) -> None:
        with self.lock:
            self.total_tokens += result.total_tokens
            self.total_points += result.points
            if not self.stop_reason and self.total_tokens >= TOKEN_BUDGET:
                self.stop_reason = "token_budget_exceeded"
            if not self.stop_reason and self.total_points >= POINTS_BUDGET:
                self.stop_reason = "points_budget_exceeded"

    def should_stop(self) -> bool:
        with self.lock:
            return bool(self.stop_reason)


def run_stage(
    phase: str,
    per_model_concurrency: int,
    duration_s: int,
    api_key: str,
    budget: BudgetGuard,
    collected: list[RequestResult],
) -> StageStats:
    stats = StageStats(phase=phase, concurrency=per_model_concurrency, duration_s=duration_s)
    deadline = time.time() + duration_s
    lock = threading.Lock()

    def worker(model: str) -> None:
        while time.time() < deadline and not budget.should_stop():
            result = call_gateway(phase, model, api_key)
            with lock:
                collected.append(result)
                stats.requests += 1
                if result.success:
                    stats.success += 1
                    stats.latencies.append(result.latency_s)
                else:
                    stats.failures += 1
                stats.prompt_tokens += result.prompt_tokens
                stats.completion_tokens += result.completion_tokens
                stats.total_tokens += result.total_tokens
                stats.total_points += result.points
            budget.add(result)

    workers: list[str] = []
    total_weight = sum(weight for _, weight in LOAD_TEST_MODELS)
    for model, weight in LOAD_TEST_MODELS:
        share = max(1, round(per_model_concurrency * weight / total_weight))
        if per_model_concurrency == 1 and len(LOAD_TEST_MODELS) == 1:
            share = 1
        workers.extend([model] * share)

    with ThreadPoolExecutor(max_workers=max(1, len(workers))) as pool:
        futures = [pool.submit(worker, model) for model in workers]
        for future in as_completed(futures):
            future.result()

    return stats


class InfrastructureSampler:
    def __init__(self, run_dir: Path) -> None:
        self.run_dir = run_dir
        self.samples: list[InfraSample] = []
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None
        self.output_file = run_dir / "infra_samples.jsonl"

    def _parse_memory(self, value: str) -> float:
        value = (value or "").strip()
        if not value or value == "--":
            return 0.0
        token = value.split("/")[0].strip().split()[0]
        multiplier = 1.0
        if token.endswith("GiB"):
            multiplier = 1024
            token = token[:-3]
        elif token.endswith("MiB"):
            multiplier = 1.0
            token = token[:-3]
        elif token.endswith("KiB"):
            multiplier = 1 / 1024
            token = token[:-3]
        elif token.endswith("B"):
            multiplier = 1 / (1024 * 1024)
            token = token[:-1]
        elif token.endswith("Gi"):
            multiplier = 1024
            token = token[:-2]
        elif token.endswith("Mi"):
            multiplier = 1.0
            token = token[:-2]
        elif token.endswith("Ki"):
            multiplier = 1 / 1024
            token = token[:-2]
        elif " " in value.split("/")[0]:
            parts = value.split("/")[0].strip().split()
            token = parts[0]
            unit = parts[1]
            if unit.startswith("Gi"):
                multiplier = 1024
            elif unit.startswith("Mi"):
                multiplier = 1.0
            elif unit.startswith("Ki"):
                multiplier = 1 / 1024
            else:
                multiplier = 1 / (1024 * 1024)
        return float(token) * multiplier

    def _sample_once(self) -> None:
        for service in INFRA_SERVICES:
            service = service.strip()
            if not service:
                continue
            try:
                proc = subprocess.run(
                    [
                        "docker",
                        "stats",
                        service,
                        "--no-stream",
                        "--format",
                        "{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}",
                    ],
                    capture_output=True,
                    text=True,
                    timeout=10,
                    check=False,
                )
            except (OSError, subprocess.SubprocessError):
                continue
            line = (proc.stdout or "").strip()
            if not line:
                continue
            parts = line.split("\t")
            if len(parts) < 4:
                continue
            mem_usage = parts[2]
            mem_parts = mem_usage.split("/")
            sample = InfraSample(
                captured_at=datetime.now(timezone.utc).isoformat(),
                service=parts[0],
                cpu_percent=float(parts[1].replace("%", "") or 0),
                memory_usage_mb=self._parse_memory(mem_parts[0] if mem_parts else ""),
                memory_limit_mb=self._parse_memory(mem_parts[1] if len(mem_parts) > 1 else ""),
                net_io=parts[4] if len(parts) > 4 else "",
                block_io=parts[5] if len(parts) > 5 else "",
            )
            self.samples.append(sample)
            with self.output_file.open("a", encoding="utf-8") as fh:
                fh.write(json.dumps(asdict(sample), ensure_ascii=False) + "\n")

    def _loop(self) -> None:
        while not self._stop.is_set():
            self._sample_once()
            self._stop.wait(INFRA_SAMPLE_INTERVAL_S)

    def start(self) -> None:
        if not ENABLE_INFRA_SAMPLE:
            return
        self.output_file.write_text("", encoding="utf-8")
        self._thread = threading.Thread(target=self._loop, daemon=True)
        self._thread.start()

    def stop(self) -> None:
        if self._thread:
            self._stop.set()
            self._thread.join(timeout=INFRA_SAMPLE_INTERVAL_S + 5)

    def summarize(self) -> dict[str, Any]:
        if not self.samples:
            return {"enabled": ENABLE_INFRA_SAMPLE, "samples": 0, "by_service": {}}
        by_service: dict[str, list[InfraSample]] = {}
        for sample in self.samples:
            by_service.setdefault(sample.service, []).append(sample)
        summary: dict[str, Any] = {"enabled": True, "samples": len(self.samples), "by_service": {}}
        for service, items in by_service.items():
            summary["by_service"][service] = {
                "max_cpu_percent": round(max(x.cpu_percent for x in items), 2),
                "avg_cpu_percent": round(statistics.mean(x.cpu_percent for x in items), 2),
                "max_memory_mb": round(max(x.memory_usage_mb for x in items), 2),
                "avg_memory_mb": round(statistics.mean(x.memory_usage_mb for x in items), 2),
            }
        return summary


def gateway_admin_request(path: str, session_token: str) -> dict[str, Any]:
    if not GATEWAY_SERVICE_AUTH_USERNAME or not GATEWAY_SERVICE_AUTH_PASSWORD:
        return {}
    auth = base64.b64encode(
        f"{GATEWAY_SERVICE_AUTH_USERNAME}:{GATEWAY_SERVICE_AUTH_PASSWORD}".encode()
    ).decode()
    req = urllib.request.Request(
        f"{GATEWAY_BASE_URL}{path}",
        headers={
            "Authorization": f"Basic {auth}",
            "X-Console-Session": session_token,
        },
        method="GET",
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode("utf-8"))


def login_console_session() -> str:
    if not GATEWAY_ADMIN_PASSWORD:
        return ""
    auth = base64.b64encode(
        f"{GATEWAY_SERVICE_AUTH_USERNAME}:{GATEWAY_SERVICE_AUTH_PASSWORD}".encode()
    ).decode()
    payload = json.dumps({"email": GATEWAY_ADMIN_EMAIL, "password": GATEWAY_ADMIN_PASSWORD}).encode()
    req = urllib.request.Request(
        f"{GATEWAY_BASE_URL}/console/session/login",
        data=payload,
        headers={
            "Authorization": f"Basic {auth}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        data = json.loads(resp.read().decode("utf-8"))
        return data.get("token") or ""


def fetch_usage_overview(session_token: str) -> dict[str, Any]:
    if not session_token:
        return {}
    try:
        return gateway_admin_request("/admin/usage/overview", session_token)
    except (urllib.error.URLError, json.JSONDecodeError, TimeoutError):
        return {}


def reconcile_usage(
    before: dict[str, Any],
    after: dict[str, Any],
    script_tokens: int,
    script_points: float,
) -> dict[str, Any]:
    def to_int(value: Any) -> int:
        try:
            return int(str(value).replace(",", ""))
        except (TypeError, ValueError):
            return 0

    before_tokens = to_int(before.get("input_tokens")) + to_int(before.get("output_tokens"))
    after_tokens = to_int(after.get("input_tokens")) + to_int(after.get("output_tokens"))
    delta_tokens = max(after_tokens - before_tokens, 0)

    before_cost = to_int(before.get("total_cost"))
    after_cost = to_int(after.get("total_cost"))
    delta_cost_microyuan = max(after_cost - before_cost, 0)
    gateway_points = delta_cost_microyuan / POINTS_DIVISOR

    token_diff_ratio = abs(delta_tokens - script_tokens) / script_tokens if script_tokens else 0.0
    points_diff_ratio = abs(gateway_points - script_points) / script_points if script_points else 0.0

    return {
        "enabled": bool(before and after),
        "gateway_delta_tokens": delta_tokens,
        "script_total_tokens": script_tokens,
        "token_diff_ratio": round(token_diff_ratio, 4),
        "gateway_delta_points": round(gateway_points, 4),
        "script_total_points": round(script_points, 4),
        "points_diff_ratio": round(points_diff_ratio, 4),
        "consistent": token_diff_ratio <= 0.15 and points_diff_ratio <= 0.15,
    }


def check_gateway_health() -> dict[str, Any]:
    try:
        with urllib.request.urlopen(f"{GATEWAY_BASE_URL}/healthz", timeout=5) as resp:
            body = json.loads(resp.read().decode("utf-8"))
            return {"ok": resp.getcode() == 200, "body": body}
    except (urllib.error.URLError, json.JSONDecodeError, TimeoutError) as exc:
        return {"ok": False, "error": str(exc)}


def summarize_results(
    results: list[RequestResult],
    stages: list[StageStats],
    budget: BudgetGuard,
    run_dir: Path,
    infra_summary: dict[str, Any],
    usage_reconciliation: dict[str, Any],
    health_before: dict[str, Any],
    health_after: dict[str, Any],
) -> dict[str, Any]:
    success_results = [r for r in results if r.success]
    latencies = [r.latency_s for r in success_results]
    ttfts = [r.ttft_s for r in success_results if r.ttft_s > 0]

    error_counts: dict[str, int] = {}
    for item in results:
        error_counts[item.error_category] = error_counts.get(item.error_category, 0) + 1

    pseudo_success_count = sum(1 for r in results if r.is_pseudo_success)
    routing_mismatch_count = sum(1 for r in results if r.routing_mismatch)

    by_model: dict[str, list[RequestResult]] = {}
    for item in results:
        by_model.setdefault(item.model, []).append(item)

    model_summary = {}
    for model, items in by_model.items():
        ok = [x for x in items if x.success]
        ok_latencies = [x.latency_s for x in ok]
        model_summary[model] = {
            "tier": pricing_for(model)["tier"],
            "label": pricing_for(model)["label"],
            "requests": len(items),
            "success": len(ok),
            "failures": len(items) - len(ok),
            "pseudo_success": sum(1 for x in items if x.is_pseudo_success),
            "routing_mismatch": sum(1 for x in items if x.routing_mismatch),
            "total_tokens": sum(x.total_tokens for x in items),
            "total_points": round(sum(x.points for x in items), 4),
            "avg_points_per_request": round(sum(x.points for x in ok) / len(ok), 4) if ok else 0.0,
            "avg_latency_s": round(statistics.mean(ok_latencies), 4) if ok_latencies else 0.0,
            "p95_latency_s": round(percentile(ok_latencies, 95), 4),
            "error_categories": {
                cat: sum(1 for x in items if x.error_category == cat) for cat in sorted({x.error_category for x in items})
            },
        }

    stage_summary = []
    peak_qps = 0.0
    peak_tps = 0.0
    peak_stage = ""
    recommended_stage = ""
    for stage in stages:
        stage_ok = stage.failure_rate <= MAX_FAILURE_RATE and stage.latency_percentile(99) <= P99_SLA_THRESHOLD_S
        entry = {
            "phase": stage.phase,
            "concurrency": stage.concurrency,
            "duration_s": stage.duration_s,
            "requests": stage.requests,
            "success": stage.success,
            "failures": stage.failures,
            "failure_rate": round(stage.failure_rate, 4),
            "qps": round(stage.qps, 4),
            "tokens_per_sec": round(stage.tokens_per_sec, 4),
            "points_per_sec": round(stage.points_per_sec, 4),
            "total_points": round(stage.total_points, 4),
            "p50_latency_s": round(stage.latency_percentile(50), 4),
            "p95_latency_s": round(stage.latency_percentile(95), 4),
            "p99_latency_s": round(stage.latency_percentile(99), 4),
            "sla_pass": stage_ok,
        }
        stage_summary.append(entry)
        if stage.qps > peak_qps and stage_ok:
            peak_qps = stage.qps
            peak_tps = stage.tokens_per_sec
            peak_stage = stage.phase
            recommended_stage = stage.phase

    recommended_concurrency = 0
    for stage in stages:
        if stage.phase == recommended_stage:
            recommended_concurrency = max(1, int(stage.concurrency * PRODUCTION_HEADROOM_RATIO))
            break

    total_points = round(sum(r.points for r in results), 4)
    peak_points_per_sec = max((s.points_per_sec for s in stages), default=0.0)

    return {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "scenario": "local-model-throughput-production",
        "gateway_url": GATEWAY_URL,
        "stream": STREAM,
        "points_divisor": POINTS_DIVISOR,
        "budgets": {
            "points_limit": POINTS_BUDGET,
            "token_limit": TOKEN_BUDGET,
            "stop_reason": budget.stop_reason,
        },
        "overall": {
            "requests": len(results),
            "success": len(success_results),
            "failures": len(results) - len(success_results),
            "success_rate": round(len(success_results) / len(results), 4) if results else 0.0,
            "pseudo_success": pseudo_success_count,
            "pseudo_success_rate": round(pseudo_success_count / len(results), 4) if results else 0.0,
            "routing_mismatch": routing_mismatch_count,
            "total_tokens": sum(r.total_tokens for r in results),
            "total_points": total_points,
            "avg_points_per_success": round(sum(r.points for r in success_results) / len(success_results), 4)
            if success_results
            else 0.0,
            "peak_qps": round(peak_qps, 4),
            "peak_tokens_per_sec": round(peak_tps, 4),
            "peak_points_per_sec": round(peak_points_per_sec, 4),
            "peak_stage": peak_stage,
            "recommended_production_concurrency": recommended_concurrency,
            "estimated_daily_points_at_peak": round(peak_points_per_sec * 86400, 2),
            "p50_latency_s": round(percentile(latencies, 50), 4),
            "p95_latency_s": round(percentile(latencies, 95), 4),
            "p99_latency_s": round(percentile(latencies, 99), 4),
            "p50_ttft_s": round(percentile(ttfts, 50), 4) if ttfts else 0.0,
            "p95_ttft_s": round(percentile(ttfts, 95), 4) if ttfts else 0.0,
        },
        "production": {
            "sla": {
                "p99_threshold_s": P99_SLA_THRESHOLD_S,
                "p99_pass": percentile(latencies, 99) <= P99_SLA_THRESHOLD_S if latencies else False,
                "success_rate_pass": (len(success_results) / len(results) >= 0.95) if results else False,
                "pseudo_success_acceptable": pseudo_success_count == 0,
            },
            "errors_by_category": error_counts,
            "infra": infra_summary,
            "usage_reconciliation": usage_reconciliation,
            "health_before": health_before,
            "health_after": health_after,
        },
        "pricing_table": MODEL_PRICING,
        "by_model": model_summary,
        "by_stage": stage_summary,
        "run_dir": str(run_dir),
    }


def write_markdown_summary(summary: dict[str, Any], path: Path) -> None:
    overall = summary["overall"]
    production = summary.get("production", {})
    lines = [
        "# 本地大模型压测结果摘要",
        "",
        f"- 生成时间：{summary['generated_at']}",
        f"- 场景：{summary['scenario']}",
        f"- 网关：{summary['gateway_url']}",
        f"- 结果目录：`{summary['run_dir']}`",
        "",
        "## 总体",
        "",
        f"- 请求总数：{overall['requests']}",
        f"- 成功数：{overall['success']}（成功率 {overall.get('success_rate', 0):.2%}）",
        f"- 失败数：{overall['failures']}",
        f"- 伪成功（200 无 content）：{overall.get('pseudo_success', 0)}",
        f"- 路由不一致：{overall.get('routing_mismatch', 0)}",
        f"- 总 Token：{overall['total_tokens']}",
        f"- 总积分：{overall['total_points']}",
        f"- 峰值 QPS：{overall['peak_qps']}（阶段 {overall['peak_stage']}）",
        f"- 峰值 Token 吞吐：{overall['peak_tokens_per_sec']} tokens/s",
        f"- 峰值积分吞吐：{overall.get('peak_points_per_sec', 0)} 积分/s",
        f"- 推荐生产并发：{overall.get('recommended_production_concurrency', 0)}",
        f"- 峰值日积分估算：{overall.get('estimated_daily_points_at_peak', 0)}",
        f"- P50/P95/P99 延迟：{overall.get('p50_latency_s', 0)}s / {overall.get('p95_latency_s', 0)}s / {overall.get('p99_latency_s', 0)}s",
        "",
        "## 生产 SLA",
        "",
    ]

    sla = production.get("sla", {})
    lines.extend([
        f"- P99 SLA（≤ {sla.get('p99_threshold_s', 0)}s）：{'通过' if sla.get('p99_pass') else '未通过'}",
        f"- 成功率 ≥ 95%：{'通过' if sla.get('success_rate_pass') else '未通过'}",
        f"- 无伪成功：{'通过' if sla.get('pseudo_success_acceptable') else '未通过'}",
        "",
        "## 错误分类",
        "",
    ])
    for cat, count in sorted((production.get("errors_by_category") or {}).items()):
        lines.append(f"- {cat}：{count}")

    lines.extend([
        "",
        "## 分阶段 QPS / 吞吐量",
        "",
        "| 阶段 | 并发 | 成功 QPS | Token/s | 积分/s | 失败率 | P95 | P99 | SLA |",
        "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |",
    ])

    for stage in summary["by_stage"]:
        lines.append(
            f"| {stage['phase']} | {stage['concurrency']} | {stage['qps']} | {stage['tokens_per_sec']} | "
            f"{stage['points_per_sec']} | {stage['failure_rate']:.2%} | {stage['p95_latency_s']}s | "
            f"{stage.get('p99_latency_s', 0)}s | {'✓' if stage.get('sla_pass') else '✗'} |"
        )

    lines.extend(["", "## 分模型积分", ""])
    for model, stats in summary["by_model"].items():
        lines.append(
            f"- **{model}**（{stats['label']} / {stats['tier']}）："
            f"请求 {stats['requests']}，成功 {stats['success']}，伪成功 {stats.get('pseudo_success', 0)}，"
            f"总 Token {stats['total_tokens']}，总积分 {stats['total_points']}，"
            f"P95 {stats.get('p95_latency_s', 0)}s"
        )

    usage = production.get("usage_reconciliation") or {}
    if usage.get("enabled"):
        lines.extend([
            "",
            "## 用量对账（脚本 vs 网关）",
            "",
            f"- 网关增量 Token：{usage.get('gateway_delta_tokens', 0)}",
            f"- 脚本累计 Token：{usage.get('script_total_tokens', 0)}",
            f"- Token 偏差率：{usage.get('token_diff_ratio', 0):.2%}",
            f"- 网关增量积分：{usage.get('gateway_delta_points', 0)}",
            f"- 脚本累计积分：{usage.get('script_total_points', 0)}",
            f"- 积分偏差率：{usage.get('points_diff_ratio', 0):.2%}",
            f"- 对账结论：{'一致' if usage.get('consistent') else '需排查'}",
        ])

    infra = production.get("infra") or {}
    if infra.get("enabled") and infra.get("by_service"):
        lines.extend(["", "## 基础设施峰值", ""])
        for service, stats in infra["by_service"].items():
            lines.append(
                f"- **{service}**：CPU 峰值 {stats['max_cpu_percent']}%，"
                f"内存峰值 {stats['max_memory_mb']} MB"
            )

    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def main() -> None:
    api_key = load_api_key()
    timestamp = datetime.now().strftime("%Y%m%d%H%M%S")
    run_dir = OUTPUT_DIR / f"local-model-{timestamp}"
    run_dir.mkdir(parents=True, exist_ok=True)
    results_file = run_dir / "results.jsonl"
    summary_json = run_dir / "summary.json"
    summary_md = run_dir / "summary.md"

    print(f"[local-load-test] 结果目录: {run_dir}")
    print(f"[local-load-test] 模型: {[m for m, _ in LOAD_TEST_MODELS]}")
    print(f"[local-load-test] 积分换算: {POINTS_DIVISOR} 微元 = 1 积分")
    print(f"[local-load-test] 生产观测: infra={ENABLE_INFRA_SAMPLE}, usage_verify={ENABLE_USAGE_VERIFY}")

    health_before = check_gateway_health()
    usage_before: dict[str, Any] = {}
    session_token = ""
    if ENABLE_USAGE_VERIFY:
        session_token = login_console_session()
        usage_before = fetch_usage_overview(session_token)
        if usage_before:
            print("[local-load-test] 已采集压测前 usage 基线")
        else:
            print("[local-load-test] usage 对账未启用（缺少 admin 凭据或登录失败）")

    infra_sampler = InfrastructureSampler(run_dir)
    infra_sampler.start()

    all_results: list[RequestResult] = []
    stage_stats: list[StageStats] = []
    budget = BudgetGuard()
    bad_stage_count = 0

    try:
        for phase, concurrency, duration in LOAD_STAGES:
            if budget.should_stop():
                print(f"[local-load-test] 预算触发停止：{budget.stop_reason}")
                break

            print(f"[local-load-test] 阶段 {phase}: 并发={concurrency}, 时长={duration}s")
            stage = run_stage(phase, concurrency, duration, api_key, budget, all_results)
            stage_stats.append(stage)

            print(
                f"  -> QPS={stage.qps:.2f}, tokens/s={stage.tokens_per_sec:.2f}, "
                f"points/s={stage.points_per_sec:.2f}, fail={stage.failure_rate:.1%}, "
                f"p99={stage.latency_percentile(99):.3f}s"
            )

            if stage.failure_rate > MAX_FAILURE_RATE:
                bad_stage_count += 1
            else:
                bad_stage_count = 0

            if bad_stage_count >= MAX_CONSECUTIVE_BAD_STAGES:
                print("[local-load-test] 连续阶段失败率过高，停止增压")
                break

            if budget.should_stop():
                print(f"[local-load-test] 预算触发停止：{budget.stop_reason}")
                break
    finally:
        infra_sampler.stop()

    with results_file.open("w", encoding="utf-8") as fh:
        for result in all_results:
            fh.write(json.dumps(asdict(result), ensure_ascii=False) + "\n")

    health_after = check_gateway_health()
    usage_after: dict[str, Any] = {}
    if ENABLE_USAGE_VERIFY and session_token:
        usage_after = fetch_usage_overview(session_token)

    usage_reconciliation = reconcile_usage(
        usage_before,
        usage_after,
        sum(r.total_tokens for r in all_results),
        sum(r.points for r in all_results),
    )

    summary = summarize_results(
        all_results,
        stage_stats,
        budget,
        run_dir,
        infra_sampler.summarize(),
        usage_reconciliation,
        health_before,
        health_after,
    )
    summary_json.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    write_markdown_summary(summary, summary_md)
    print(f"[local-load-test] 完成，摘要: {summary_md}")


if __name__ == "__main__":
    main()
