#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTRACT_BOUNDARY_TARGETS=(
  "${ROOT_DIR}/README.md"
  "${ROOT_DIR}/docs/specs/2026-04-27-tenant-api-key-governance-platform-design.md"
  "${ROOT_DIR}/deploy/compose/compose.yml"
  "${ROOT_DIR}/gateway/internal/http"
  "${ROOT_DIR}/gateway/internal/service/console_service.go"
  "${ROOT_DIR}/web/src"
)
IMPLEMENTATION_BOUNDARY_TARGETS=(
  "${ROOT_DIR}/gateway/internal/service/postgres_console_service.go"
)
CONTRACT_FORBIDDEN_TERMS=(
  "知""识库"
  "RAG 控制""台"
  "resolved""_provider"
  "provider_qwen_""primary"
  "内部执行线 A"
  "内部执行线 B"
  "OpenAI"" Primary"
  "DashScope"" Primary"
)
IMPLEMENTATION_FORBIDDEN_TERMS=(
  "知""识库"
  "RAG 控制""台"
  "provider_qwen_""primary"
  "OpenAI"" Primary"
  "DashScope"" Primary"
)

scan_product_boundary() {
  local -n terms_ref=$1
  local -n targets_ref=$2
  local term

  for term in "${terms_ref[@]}"; do
    if rg -n --fixed-strings --color=never "${term}" "${targets_ref[@]}"; then
      echo "boundary scan failed: found forbidden term '${term}'" >&2
      exit 1
    fi
  done
}

scan_product_boundary CONTRACT_FORBIDDEN_TERMS CONTRACT_BOUNDARY_TARGETS
scan_product_boundary IMPLEMENTATION_FORBIDDEN_TERMS IMPLEMENTATION_BOUNDARY_TARGETS
(cd "${ROOT_DIR}/gateway" && go test ./...)
(cd "${ROOT_DIR}/rag-service" && pytest)
npm --prefix "${ROOT_DIR}/web" test -- --runInBand
docker compose \
  --env-file "${ROOT_DIR}/deploy/compose/.env.example" \
  -f "${ROOT_DIR}/deploy/compose/compose.yml" \
  config >/dev/null
