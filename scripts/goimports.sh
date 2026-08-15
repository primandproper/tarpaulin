#!/usr/bin/env bash
set -euo pipefail

# Format Go imports using goimports
# Usage: goimports.sh [project_root]
#
# The file list comes from go_files.sh, which asks the Go toolchain. That is
# what keeps testdata/ out, and testdata/ has to stay out: it holds the
# analyzer's fixture corpus, whose expectations pin declaration line numbers,
# and one fixture is deliberately unparseable. Rewriting those files would
# either invalidate the fixtures or fail the run outright.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="${1:-$(pwd)}"

# Through a file rather than `< <(go_files.sh)`: process substitution discards
# the exit status of what it runs, so a failure to produce the list would read
# here as a list of no files, and formatting nothing would look like success.
file_list="$(mktemp)"
trap 'rm -f "${file_list}"' EXIT

"${SCRIPT_DIR}/go_files.sh" "${PROJECT_ROOT}" >"${file_list}"

go_files=()
while IFS= read -r -d '' file; do
  go_files+=("${file}")
done <"${file_list}"

if [ ${#go_files[@]} -gt 0 ]; then
  go tool goimports -w "${go_files[@]}"
fi
