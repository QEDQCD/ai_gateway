#!/usr/bin/env bash
set -euo pipefail

(cd gateway && go test ./...)
(cd rag-service && pytest)
npm --prefix web test -- --runInBand
