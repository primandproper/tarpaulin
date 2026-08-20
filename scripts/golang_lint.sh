#!/usr/bin/env bash
set -euo pipefail

# Lint Go code using golangci-lint.
# Usage: golang_lint.sh <container_runner> <linter_image>
#
# The tree is copied into the container and linted there, rather than
# bind-mounted and linted in place. Mounting is the obvious spelling and on
# Docker Desktop for macOS it is unusable: the run that finishes in ~190s
# against a copy does not finish in the thirty minutes golangci-lint gives
# itself against the mount. It is not the module cache (a cold `go mod
# download` in the container is 12s), not the build cache (a cold native run
# and a warm one are both ~390s), not the linter version, and not the volume
# of I/O — the whole tree walks over the mount in ~130ms. What is left is the
# concurrent small-file access of ~46 linters through Docker Desktop's
# fakeowner shim, which a sequential benchmark does not stress. The copy costs
# ~100ms.
#
# The price is that --fix cannot work here: the container would edit its own
# snapshot. Nothing passes --fix, and `make format` is where this repo rewrites
# files.

CONTAINER_RUNNER="${1:-docker}"
LINTER_IMAGE="${2:-golangci/golangci-lint:v2.10.1}"

# Named so the trap below can reach it, and PID-suffixed so two runs cannot
# collide. Killing the client does not stop the container it started, which is
# how an interrupted `make lint` leaves a linter running to compete with the
# next one for the rest of the afternoon.
CONTAINER_NAME="golangci-lint-$$"

cleanup() {
  "${CONTAINER_RUNNER}" kill "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}

trap cleanup EXIT INT TERM

"${CONTAINER_RUNNER}" pull --quiet "${LINTER_IMAGE}"

# .git and artifacts/ are skipped by the copy because no linter reads them and
# they are the two largest things in the tree. Paths in the report stay relative
# to the copy's root, which is to say relative to the repository root, so an
# editor can still jump to what it names.
"${CONTAINER_RUNNER}" run --rm --name "${CONTAINER_NAME}" \
  --volume "${PWD}:/src:ro" \
  --network=host \
  "${LINTER_IMAGE}" \
  sh -c 'mkdir -p /work \
    && cd /src \
    && tar cf - --exclude=./.git --exclude=./artifacts . | (cd /work && tar xf -) \
    && cd /work \
    && exec golangci-lint run --config=.golangci.yml --timeout 30m ./...'
