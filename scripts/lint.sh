#!/usr/bin/env bash
set -euo pipefail

gofmt -l gateway | tee /tmp/gofmt.out
if [[ -s /tmp/gofmt.out ]]; then
  echo "gofmt check failed"
  exit 1
fi

npm --prefix web run build >/dev/null
