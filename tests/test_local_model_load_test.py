#!/usr/bin/env python3
"""本地模型压测脚本单元验证。"""

from run_local_model_load_test import (
    classify_error,
    compute_points,
    is_pseudo_success,
    reconcile_usage,
    round_microyuan_cost,
    routing_mismatch,
)


def test_round_microyuan_cost() -> None:
    assert round_microyuan_cost(1_000_000, 2_000_000) == 2_000_000
    assert round_microyuan_cost(0, 2_000_000) == 0


def test_compute_points_cheap_vs_expensive() -> None:
    cheap_points, cheap_cost = compute_points("local-qwen-7b", 100, 50, 0)
    expensive_points, expensive_cost = compute_points("local-qwen-72b", 100, 50, 0)

    assert expensive_cost["total_cost_microyuan"] > cheap_cost["total_cost_microyuan"]
    assert expensive_points > cheap_points


def test_compute_points_cached_tokens() -> None:
    no_cache, _ = compute_points("qwen-flash", 1000, 100, 0)
    with_cache, _ = compute_points("qwen-flash", 1000, 100, 800)
    assert with_cache < no_cache


def test_classify_error() -> None:
    assert classify_error(401, "empty_or_error", "", False) == "auth_401"
    assert classify_error(429, "empty_or_error", "", False) == "rate_limit_429"
    assert classify_error(200, "reasoning_only", "", False) == "reasoning_only"
    assert classify_error(200, "empty_or_error", "", False) == "empty_content"
    assert classify_error(0, "empty_or_error", "timed out", True) == "timeout"


def test_pseudo_success_and_routing() -> None:
    assert is_pseudo_success(200, "reasoning_only") is True
    assert is_pseudo_success(200, "content") is False
    assert routing_mismatch("local-qwen-7b", "local-qwen-7b") is False
    assert routing_mismatch("local-qwen-7b", "local-qwen-72b") is True


def test_reconcile_usage() -> None:
    before = {"input_tokens": "100", "output_tokens": "50", "total_cost": "10000"}
    after = {"input_tokens": "200", "output_tokens": "150", "total_cost": "30000"}
    result = reconcile_usage(before, after, script_tokens=200, script_points=2.0)
    assert result["enabled"] is True
    assert result["gateway_delta_tokens"] == 200
    assert result["gateway_delta_points"] == 2.0
    assert result["consistent"] is True


def main() -> None:
    test_round_microyuan_cost()
    test_compute_points_cheap_vs_expensive()
    test_compute_points_cached_tokens()
    test_classify_error()
    test_pseudo_success_and_routing()
    test_reconcile_usage()
    print("ok: local model load test helpers")


if __name__ == "__main__":
    main()
