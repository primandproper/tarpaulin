#!/usr/bin/env bash
set -euo pipefail

# Format Go imports using goimports
# Usage: goimports.sh [project_root]
#
# testdata/ is excluded deliberately: it holds the analyzer's fixture corpus,
# whose golden files pin declaration line numbers, and one fixture is
# deliberately unparseable. Rewriting those files would either invalidate the
# goldens or fail the run outright. Every Go-wildcard-driven target (test, lint,
# go fix, fieldalignment, tagalign) already skips testdata for free.

PROJECT_ROOT="${1:-$(pwd)}"

go_files=()
while IFS= read -r -d '' file; do
  go_files+=("${file}")
done < <(find "${PROJECT_ROOT}" -type f -not -path '*/vendor/*' -not -path '*/testdata/*' -name "*.go" -print0)

if [ ${#go_files[@]} -gt 0 ]; then
  go tool goimports -w "${go_files[@]}"
fi
