#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${ROOT_DIR}/tests/output"

DEFAULT_URL="http://127.0.0.1:32658/v1/chat/completions"
DEFAULT_API_KEY_FILE="/root/.ai_gateway_secrets/gateway_seed_platform_api_key"
DEFAULT_MODE="all"
DEFAULT_REQUEST_TIMEOUT=45
DEFAULT_SHORT_MAX_TOKENS=16
DEFAULT_DEEPSEEK_MAX_TOKENS=512
DEFAULT_STAGE_A_SECONDS=10
DEFAULT_STAGE_B_SECONDS=20
DEFAULT_STAGE_C_SECONDS=30
DEFAULT_QWEN_TOKEN_BUDGET=300000
DEFAULT_MIMO_TOKEN_BUDGET=200000

URL="${AGW_URL:-$DEFAULT_URL}"
API_KEY="${AGW_API_KEY:-}"
API_KEY_FILE="${AGW_API_KEY_FILE:-$DEFAULT_API_KEY_FILE}"
MODE="${AGW_LOAD_TEST_MODE:-$DEFAULT_MODE}"
REQUEST_TIMEOUT="${AGW_REQUEST_TIMEOUT:-$DEFAULT_REQUEST_TIMEOUT}"
SHORT_MAX_TOKENS="${AGW_SHORT_MAX_TOKENS:-$DEFAULT_SHORT_MAX_TOKENS}"
DEEPSEEK_MAX_TOKENS="${AGW_DEEPSEEK_MAX_TOKENS:-$DEFAULT_DEEPSEEK_MAX_TOKENS}"
STAGE_A_SECONDS="${AGW_STAGE_A_SECONDS:-$DEFAULT_STAGE_A_SECONDS}"
STAGE_B_SECONDS="${AGW_STAGE_B_SECONDS:-$DEFAULT_STAGE_B_SECONDS}"
STAGE_C_SECONDS="${AGW_STAGE_C_SECONDS:-$DEFAULT_STAGE_C_SECONDS}"
QWEN_TOKEN_BUDGET="${AGW_QWEN_TOKEN_BUDGET:-$DEFAULT_QWEN_TOKEN_BUDGET}"
MIMO_TOKEN_BUDGET="${AGW_MIMO_TOKEN_BUDGET:-$DEFAULT_MIMO_TOKEN_BUDGET}"
PROMPT_SHORT="${AGW_LOAD_TEST_PROMPT_SHORT:-你好,一句话回答}"
PROMPT_DEEPSEEK="${AGW_LOAD_TEST_PROMPT_DEEPSEEK:-你好，请只回复：你好。}"
PRECHECK_REPEAT="${AGW_PRECHECK_REPEAT:-1}"
DRY_RUN=0

TIMESTAMP="$(date +%Y%m%d%H%M%S)-$$-$(date +%N)"
RUN_DIR="${OUTPUT_DIR}/load-test-${TIMESTAMP}"
RESULTS_FILE="${RUN_DIR}/results.jsonl"
SUMMARY_JSON="${RUN_DIR}/summary.json"
SUMMARY_MD="${RUN_DIR}/summary.md"
STOP_FILE="${RUN_DIR}/STOP"

usage() {
  cat <<'EOF'
用法：
  tests/run_gateway_load_test.sh [options]

选项：
  --mode <all|precheck|deepseek|mixed>
  --url <gateway-url>
  --api-key <platform-api-key>
  --api-key-file <path>
  --request-timeout <seconds>
  --short-max-tokens <int>
  --deepseek-max-tokens <int>
  --stage-a-seconds <int>
  --stage-b-seconds <int>
  --stage-c-seconds <int>
  --qwen-token-budget <int>
  --mimo-token-budget <int>
  --precheck-repeat <int>
  --dry-run
  --help

说明：
  - `precheck`：对模型做最简单流式/非流式验证
  - `deepseek`：仅验证 `deepseek-r1-distill-qwen-7b`
  - `mixed`：执行 `qwen-flash` + `mimo-v2.5-pro` 的轻压测
  - `all`：依次执行 `precheck` -> `deepseek` -> `mixed`
EOF
}

log() {
  printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "缺少命令：$1"
}

model_family() {
  local model="$1"
  case "$model" in
    mimo*) echo "mimo" ;;
    *) echo "qwen" ;;
  esac
}

json_escape() {
  jq -Rsa . <<<"${1}"
}

ensure_api_key() {
  if [[ -z "${API_KEY}" ]]; then
    [[ -f "${API_KEY_FILE}" ]] || die "找不到 API Key 文件：${API_KEY_FILE}"
    API_KEY="$(<"${API_KEY_FILE}")"
  fi
  [[ -n "${API_KEY}" ]] || die "平台 API Key 为空"
}

init_run_dir() {
  mkdir -p "${RUN_DIR}"
  : > "${RESULTS_FILE}"
  rm -f "${STOP_FILE}"
}

append_result() {
  local line="$1"
  printf '%s\n' "${line}" >> "${RESULTS_FILE}"
}

refresh_budget_guard() {
  local summary
  [[ -s "${RESULTS_FILE}" ]] || return 0
  summary="$(jq -s '
    reduce .[] as $item (
      {qwen: 0, mimo: 0};
      if ($item.family == "qwen") then .qwen += ($item.total_tokens // 0)
      elif ($item.family == "mimo") then .mimo += ($item.total_tokens // 0)
      else .
      end
    )' "${RESULTS_FILE}")"

  local qwen_used mimo_used
  qwen_used="$(jq -r '.qwen' <<<"${summary}")"
  mimo_used="$(jq -r '.mimo' <<<"${summary}")"

  if (( qwen_used >= QWEN_TOKEN_BUDGET )) || (( mimo_used >= MIMO_TOKEN_BUDGET )); then
    echo "budget_exceeded" > "${STOP_FILE}"
  fi
}

should_stop() {
  [[ -f "${STOP_FILE}" ]]
}

record_request() {
  local phase="$1"
  local model="$2"
  local stream="$3"
  local max_tokens="$4"
  local prompt="$5"

  local tmp_body tmp_http tmp_time tmp_payload tmp_out
  tmp_out="$(mktemp)"

  local payload
  payload="$(jq -nc \
    --arg model "${model}" \
    --arg prompt "${prompt}" \
    --argjson stream "${stream}" \
    --argjson max_tokens "${max_tokens}" \
    '{
      model: $model,
      messages: [{role: "user", content: $prompt}],
      max_tokens: $max_tokens
    } + (if $stream then {stream: true} else {} end)')"

  if (( DRY_RUN == 1 )); then
    local dry_result
    dry_result="$(jq -nc \
      --arg phase "${phase}" \
      --arg family "$(model_family "${model}")" \
      --arg model "${model}" \
      --argjson stream "${stream}" \
      --argjson max_tokens "${max_tokens}" \
      --arg url "${URL}" \
      '{phase: $phase, family: $family, model: $model, stream: $stream, max_tokens: $max_tokens, url: $url, success: false, dry_run: true}')"
    append_result "${dry_result}"
    printf '%s\n' "${dry_result}"
    rm -f "${tmp_out}"
    return 0
  fi

  local curl_meta http_code curl_time curl_exit=0
  curl_meta="$(
    curl -sS -N \
      --max-time "${REQUEST_TIMEOUT}" \
      -o "${tmp_out}" \
      -w '%{http_code} %{time_total}' \
      -H "Authorization: Bearer ${API_KEY}" \
      -H 'Content-Type: application/json' \
      -X POST "${URL}" \
      -d "${payload}"
  )" || curl_exit=$?
  http_code="${curl_meta%% *}"
  curl_time="${curl_meta#* }"

  local body
  body="$(<"${tmp_out}")"
  rm -f "${tmp_out}"

  local family returned_model prompt_tokens completion_tokens total_tokens cached_tokens
  local content reasoning finish_reason done_marker outcome success parse_ok error_message
  family="$(model_family "${model}")"
  returned_model=""
  prompt_tokens=0
  completion_tokens=0
  total_tokens=0
  cached_tokens=0
  content=""
  reasoning=""
  finish_reason=""
  done_marker=0
  outcome="unknown"
  success=false
  parse_ok=true
  error_message=""

  if (( curl_exit != 0 )); then
    outcome="curl_error"
    error_message="curl exit ${curl_exit}"
  elif [[ "${stream}" == "false" ]]; then
    if jq -e . >/dev/null 2>&1 <<<"${body}"; then
      returned_model="$(jq -r '.model // empty' <<<"${body}")"
      prompt_tokens="$(jq -r '.usage.prompt_tokens // 0' <<<"${body}")"
      completion_tokens="$(jq -r '.usage.completion_tokens // 0' <<<"${body}")"
      total_tokens="$(jq -r '.usage.total_tokens // 0' <<<"${body}")"
      cached_tokens="$(jq -r '.usage.cached_tokens // 0' <<<"${body}")"
      content="$(jq -r '.choices[0].message.content // empty' <<<"${body}")"
      reasoning="$(jq -r '.choices[0].message.reasoning_content // empty' <<<"${body}")"
      finish_reason="$(jq -r '.choices[0].finish_reason // empty' <<<"${body}")"
      if [[ "${http_code}" == "200" && -n "${content}" ]]; then
        outcome="content"
        success=true
      elif [[ "${http_code}" == "200" && -n "${reasoning}" ]]; then
        outcome="reasoning_only"
      else
        outcome="empty_or_error"
      fi
    else
      parse_ok=false
      outcome="invalid_json"
      error_message="non-stream response is not valid json"
    fi
  else
    done_marker="$(grep -c '\[DONE\]' <<<"${body}" || true)"
    while IFS= read -r line; do
      [[ "${line}" == data:\ * ]] || continue
      local chunk
      chunk="${line#data: }"
      [[ "${chunk}" == "[DONE]" ]] && continue
      if ! jq -e . >/dev/null 2>&1 <<<"${chunk}"; then
        continue
      fi
      local chunk_model
      chunk_model="$(jq -r '.model // empty' <<<"${chunk}")"
      [[ -n "${chunk_model}" ]] && returned_model="${chunk_model}"
      local chunk_prompt chunk_completion chunk_total chunk_cached
      chunk_prompt="$(jq -r '.usage.prompt_tokens // empty' <<<"${chunk}")"
      chunk_completion="$(jq -r '.usage.completion_tokens // empty' <<<"${chunk}")"
      chunk_total="$(jq -r '.usage.total_tokens // empty' <<<"${chunk}")"
      chunk_cached="$(jq -r '.usage.cached_tokens // empty' <<<"${chunk}")"
      [[ -n "${chunk_prompt}" ]] && prompt_tokens="${chunk_prompt}"
      [[ -n "${chunk_completion}" ]] && completion_tokens="${chunk_completion}"
      [[ -n "${chunk_total}" ]] && total_tokens="${chunk_total}"
      [[ -n "${chunk_cached}" ]] && cached_tokens="${chunk_cached}"
      content+="$(jq -r '.choices[0].delta.content // empty' <<<"${chunk}")"
      reasoning+="$(
        jq -r '.choices[0].delta.reasoning_content // .choices[0].reasoning_content // .reasoning_content // empty' <<<"${chunk}"
      )"
      local chunk_finish
      chunk_finish="$(jq -r '.choices[0].finish_reason // empty' <<<"${chunk}")"
      [[ -n "${chunk_finish}" ]] && finish_reason="${chunk_finish}"
    done <<<"${body}"

    if [[ "${http_code}" == "200" && -n "${content}" ]]; then
      outcome="content"
      success=true
    elif [[ "${http_code}" == "200" && -n "${reasoning}" ]]; then
      outcome="reasoning_only"
    else
      outcome="empty_or_error"
    fi
  fi

  local result
  result="$(jq -nc \
    --arg phase "${phase}" \
    --arg family "${family}" \
    --arg model "${model}" \
    --arg returned_model "${returned_model}" \
    --arg outcome "${outcome}" \
    --arg finish_reason "${finish_reason}" \
    --arg error_message "${error_message}" \
    --arg content "${content}" \
    --arg reasoning "${reasoning}" \
    --arg http_code "${http_code}" \
    --arg curl_time "${curl_time:-0}" \
    --argjson stream "${stream}" \
    --argjson max_tokens "${max_tokens}" \
    --argjson prompt_tokens "${prompt_tokens:-0}" \
    --argjson completion_tokens "${completion_tokens:-0}" \
    --argjson total_tokens "${total_tokens:-0}" \
    --argjson cached_tokens "${cached_tokens:-0}" \
    --argjson done_marker "${done_marker:-0}" \
    --argjson parse_ok "${parse_ok}" \
    --argjson success "${success}" \
    --arg created_at "$(date -Is)" \
    '{
      created_at: $created_at,
      phase: $phase,
      family: $family,
      model: $model,
      returned_model: $returned_model,
      stream: $stream,
      max_tokens: $max_tokens,
      http_code: ($http_code | tonumber? // 0),
      curl_time_s: ($curl_time | tonumber? // 0),
      prompt_tokens: $prompt_tokens,
      completion_tokens: $completion_tokens,
      total_tokens: $total_tokens,
      cached_tokens: $cached_tokens,
      content_length: ($content | length),
      reasoning_length: ($reasoning | length),
      done_marker_count: $done_marker,
      outcome: $outcome,
      finish_reason: $finish_reason,
      parse_ok: $parse_ok,
      success: $success,
      error_message: $error_message,
      content_excerpt: ($content | .[0:120]),
      reasoning_excerpt: ($reasoning | .[0:120])
    }')"

  append_result "${result}"
  refresh_budget_guard
  printf '%s\n' "${result}"
}

run_precheck() {
  local models=(qwen-flash mimo-v2.5-pro deepseek-r1-distill-qwen-7b)
  local model stream repeat max_tokens prompt
  log "开始预检"
  for model in "${models[@]}"; do
    for repeat in $(seq 1 "${PRECHECK_REPEAT}"); do
      for stream in false true; do
        if [[ "${model}" == "deepseek-r1-distill-qwen-7b" ]]; then
          max_tokens="${DEEPSEEK_MAX_TOKENS}"
          prompt="${PROMPT_DEEPSEEK}"
        else
          max_tokens="${SHORT_MAX_TOKENS}"
          prompt="${PROMPT_SHORT}"
        fi
        record_request "precheck" "${model}" "${stream}" "${max_tokens}" "${prompt}" >/dev/null
        if should_stop; then
          return 0
        fi
      done
    done
  done
}

run_deepseek_verify() {
  log "开始 deepseek 单独验证"
  record_request "deepseek" "deepseek-r1-distill-qwen-7b" false "${DEEPSEEK_MAX_TOKENS}" "${PROMPT_DEEPSEEK}" >/dev/null
  if should_stop; then
    return 0
  fi
  record_request "deepseek" "deepseek-r1-distill-qwen-7b" true "${DEEPSEEK_MAX_TOKENS}" "${PROMPT_DEEPSEEK}" >/dev/null
}

worker_loop() {
  local phase="$1"
  local model="$2"
  local stream="$3"
  local max_tokens="$4"
  local prompt="$5"
  local deadline="$6"

  while (( "$(date +%s)" < deadline )); do
    should_stop && break
    record_request "${phase}" "${model}" "${stream}" "${max_tokens}" "${prompt}" >/dev/null || true
  done
}

run_stage() {
  local phase="$1"
  local seconds="$2"
  shift 2
  local deadline=$(( $(date +%s) + seconds ))
  local pids=()

  log "开始阶段 ${phase}，持续 ${seconds}s"
  while (( "$#" )); do
    local model="$1"
    local stream="$2"
    shift 2
    worker_loop "${phase}" "${model}" "${stream}" "${SHORT_MAX_TOKENS}" "${PROMPT_SHORT}" "${deadline}" &
    pids+=("$!")
  done

  local pid
  for pid in "${pids[@]}"; do
    wait "${pid}" || true
  done
}

run_mixed() {
  log "开始 mixed 轻压测"
  run_stage "mixed_stage_a" "${STAGE_A_SECONDS}" \
    qwen-flash false
  if should_stop; then
    return 0
  fi
  run_stage "mixed_stage_b" "${STAGE_B_SECONDS}" \
    qwen-flash false \
    mimo-v2.5-pro false
  if should_stop; then
    return 0
  fi
  run_stage "mixed_stage_c" "${STAGE_C_SECONDS}" \
    qwen-flash false \
    qwen-flash false \
    mimo-v2.5-pro false \
    mimo-v2.5-pro false
}

write_summary() {
  jq -s \
    --arg qwen_budget "${QWEN_TOKEN_BUDGET}" \
    --arg mimo_budget "${MIMO_TOKEN_BUDGET}" '
      map(select(.dry_run != true)) as $items
      |
      def stats(items):
        {
          requests: (items | length),
          success: (items | map(select(.success == true)) | length),
          reasoning_only: (items | map(select(.outcome == "reasoning_only")) | length),
          failures: (items | map(select(.success != true and .outcome != "reasoning_only")) | length),
          prompt_tokens: (items | map(.prompt_tokens // 0) | add // 0),
          completion_tokens: (items | map(.completion_tokens // 0) | add // 0),
          total_tokens: (items | map(.total_tokens // 0) | add // 0),
          avg_time_s: ((items | map(.curl_time_s // 0) | add // 0) / ((items | length) | if . == 0 then 1 else . end))
        };
      {
        generated_at: (now | todateiso8601),
        budgets: {
          qwen_total_tokens_limit: ($qwen_budget | tonumber),
          mimo_total_tokens_limit: ($mimo_budget | tonumber)
        },
        overall: stats($items),
        by_model: (
          ($items | group_by(.model))
          | map({key: .[0].model, value: stats(.)})
          | from_entries
        ),
        by_phase: (
          ($items | group_by(.phase))
          | map({key: .[0].phase, value: stats(.)})
          | from_entries
        ),
        by_family: (
          ($items | group_by(.family))
          | map({key: .[0].family, value: stats(.)})
          | from_entries
        )
      }' "${RESULTS_FILE}" > "${SUMMARY_JSON}"

  {
    echo "# 压测结果摘要"
    echo
    echo "- 结果目录：\`${RUN_DIR}\`"
    echo "- 原始结果：\`${RESULTS_FILE}\`"
    echo "- 汇总 JSON：\`${SUMMARY_JSON}\`"
    echo
    echo "## Overall"
    jq -r '
      [
        "- 请求总数：\(.overall.requests)",
        "- 成功数：\(.overall.success)",
        "- reasoning_only：\(.overall.reasoning_only)",
        "- 失败数：\(.overall.failures)",
        "- 总 Token：\(.overall.total_tokens)",
        "- 平均耗时：\(.overall.avg_time_s | tostring)s"
      ] | .[]' "${SUMMARY_JSON}"
    echo
    echo "## By Model"
    jq -r '
      .by_model
      | to_entries[]
      | "- \(.key)：请求 \(.value.requests)，成功 \(.value.success)，reasoning_only \(.value.reasoning_only)，失败 \(.value.failures)，总 Token \(.value.total_tokens)，平均耗时 \(.value.avg_time_s | tostring)s"
    ' "${SUMMARY_JSON}"
    echo
    echo "## By Phase"
    jq -r '
      .by_phase
      | to_entries[]
      | "- \(.key)：请求 \(.value.requests)，成功 \(.value.success)，reasoning_only \(.value.reasoning_only)，失败 \(.value.failures)，总 Token \(.value.total_tokens)"
    ' "${SUMMARY_JSON}"
  } > "${SUMMARY_MD}"
}

parse_args() {
  while (( "$#" )); do
    case "$1" in
      --mode) MODE="$2"; shift 2 ;;
      --url) URL="$2"; shift 2 ;;
      --api-key) API_KEY="$2"; shift 2 ;;
      --api-key-file) API_KEY_FILE="$2"; shift 2 ;;
      --request-timeout) REQUEST_TIMEOUT="$2"; shift 2 ;;
      --short-max-tokens) SHORT_MAX_TOKENS="$2"; shift 2 ;;
      --deepseek-max-tokens) DEEPSEEK_MAX_TOKENS="$2"; shift 2 ;;
      --stage-a-seconds) STAGE_A_SECONDS="$2"; shift 2 ;;
      --stage-b-seconds) STAGE_B_SECONDS="$2"; shift 2 ;;
      --stage-c-seconds) STAGE_C_SECONDS="$2"; shift 2 ;;
      --qwen-token-budget) QWEN_TOKEN_BUDGET="$2"; shift 2 ;;
      --mimo-token-budget) MIMO_TOKEN_BUDGET="$2"; shift 2 ;;
      --precheck-repeat) PRECHECK_REPEAT="$2"; shift 2 ;;
      --dry-run) DRY_RUN=1; shift ;;
      --help|-h) usage; exit 0 ;;
      *) die "未知参数：$1" ;;
    esac
  done
}

main() {
  parse_args "$@"
  require_cmd curl
  require_cmd jq
  ensure_api_key
  init_run_dir

  log "运行模式：${MODE}"
  log "结果目录：${RUN_DIR}"

  case "${MODE}" in
    precheck) run_precheck ;;
    deepseek) run_deepseek_verify ;;
    mixed) run_mixed ;;
    all)
      run_precheck
      if ! should_stop; then
        run_deepseek_verify
      fi
      if ! should_stop; then
        run_mixed
      fi
      ;;
    *)
      die "不支持的 mode：${MODE}"
      ;;
  esac

  write_summary
  log "完成。摘要：${SUMMARY_MD}"
}

main "$@"
