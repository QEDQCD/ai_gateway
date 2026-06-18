#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHON_BIN="${PYTHON_BIN:-python3}"

export AGW_URL="${AGW_URL:-http://127.0.0.1:32658/v1/chat/completions}"
export AGW_API_KEY_FILE="${AGW_API_KEY_FILE:-/root/.ai_gateway_secrets/gateway_seed_platform_api_key}"

# 可选：从 deploy/compose/.env.local 加载 admin 凭据（仅本地压测）
if [[ -f "${ROOT_DIR}/deploy/compose/.env.local" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/deploy/compose/.env.local"
  set +a
  export GATEWAY_SERVICE_AUTH_USERNAME="${GATEWAY_SERVICE_AUTH_USERNAME:-}"
  export GATEWAY_SERVICE_AUTH_PASSWORD="${GATEWAY_SERVICE_AUTH_PASSWORD:-}"
  export GATEWAY_CONSOLE_ADMIN_PASSWORD="${GATEWAY_CONSOLE_ADMIN_PASSWORD:-}"
fi

export AGW_INFRA_SAMPLE="${AGW_INFRA_SAMPLE:-1}"
export AGW_USAGE_VERIFY="${AGW_USAGE_VERIFY:-0}"

exec "${PYTHON_BIN}" "${ROOT_DIR}/tests/run_local_model_load_test.py" "$@"
