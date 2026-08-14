#!/usr/bin/env bash
set -euo pipefail

# Build the release archives the GitHub Action downloads. PRD 8 requires a
# prebuilt binary so consumers never pay `go install` against platform-go's
# module graph, and this is what produces it.
#
# Usage: release.sh <version> <output_dir> <binary_name> <package>
#   e.g. release.sh v1.2.0 artifacts/release tarp github.com/org/mod/cmd/main
#
# Output is <output_dir>/<binary>_<version>_<os>_<arch>.{tar.gz,zip} plus a
# checksums.txt over exactly those archives. The version is used verbatim, with
# no leading "v" stripped: the action interpolates its `version:` input straight
# into the asset name, and a transformation here would be one the action has to
# reproduce exactly or silently 404.

VERSION="${1:?missing version}"
OUT_DIR="${2:?missing output directory}"
BINARY_NAME="${3:?missing binary name}"
PACKAGE="${4:?missing package path}"

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Every platform a Go project's CI might run on. Cross-compiling all of them
# from one runner costs nothing: tarp is pure Go, so CGO is off and no target
# needs a C toolchain.
PLATFORMS=(
	"linux/amd64"
	"linux/arm64"
	"darwin/amd64"
	"darwin/arm64"
	"windows/amd64"
	"windows/arm64"
)

# Rebuild the output directory from scratch, so a stale archive from an earlier
# run can never be picked up and published as part of this one.
rm -rf "${PROJECT_ROOT:?}/${OUT_DIR}"
mkdir -p "${PROJECT_ROOT}/${OUT_DIR}"
ABS_OUT_DIR="$(cd "${PROJECT_ROOT}/${OUT_DIR}" && pwd)"

STAGING="$(mktemp -d)"
trap 'rm -rf "${STAGING}"' EXIT

archives=()

for platform in "${PLATFORMS[@]}"; do
	goos="${platform%/*}"
	goarch="${platform#*/}"

	binary="${BINARY_NAME}"
	if [[ "${goos}" == "windows" ]]; then
		binary="${BINARY_NAME}.exe"
	fi

	stage="${STAGING}/${goos}_${goarch}"
	mkdir -p "${stage}"

	echo "Building ${goos}/${goarch}..."
	CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" VERSION="${VERSION}" \
		"${PROJECT_ROOT}/scripts/build.sh" -o "${stage}/${binary}" "${PACKAGE}"

	# The license and the README travel with the binary: an archive unpacked
	# onto someone else's CI runner should say what it is and how it is licensed.
	cp "${PROJECT_ROOT}/LICENSE" "${PROJECT_ROOT}/README.md" "${stage}/"

	archive="${BINARY_NAME}_${VERSION}_${goos}_${goarch}"

	# Members are listed explicitly rather than archiving the directory, so the
	# layout is a flat three entries with no "./" prefix to strip.
	if [[ "${goos}" == "windows" ]]; then
		(cd "${stage}" && zip --quiet "${ABS_OUT_DIR}/${archive}.zip" "${binary}" LICENSE README.md)
		archives+=("${archive}.zip")
	else
		tar -czf "${ABS_OUT_DIR}/${archive}.tar.gz" -C "${stage}" "${binary}" LICENSE README.md
		archives+=("${archive}.tar.gz")
	fi
done

# GNU coreutils on a runner, BSD shasum on a macOS laptop.
if command -v sha256sum &>/dev/null; then
	checksum=(sha256sum)
else
	checksum=(shasum --algorithm 256)
fi

# Checksummed by name, so checksums.txt can never accidentally cover itself or a
# file this run did not produce.
(cd "${ABS_OUT_DIR}" && "${checksum[@]}" "${archives[@]}" >checksums.txt)

echo
echo "Release artifacts in ${OUT_DIR}:"
(cd "${ABS_OUT_DIR}" && ls -1)
