#!/usr/bin/env bash
set -euo pipefail

# Run benchmarks
# Usage: bench.sh [go test flags...]
#
# No race detector and no shuffling: both distort what is being measured. The
# analyzer's own passes are microseconds against a go/packages load, so the
# default count is low — pass -benchtime/-count to look closer.

CGO_ENABLED=1 go test -run '^$' -bench . -benchtime 10x ./internal/... "$@"
