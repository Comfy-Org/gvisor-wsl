#!/usr/bin/env bash
# Copyright 2026 The gVisor Authors.
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
bash "$repo_dir/tools/wsl/bazel.sh" build --jobs="${WSL_BUILD_JOBS:-8}" //:release "$@"
bazel_bin_container="$(bash "$repo_dir/tools/wsl/bazel.sh" info bazel-bin)"
case "$bazel_bin_container" in
    /build-cache/*) bazel_bin_host="$repo_dir/.build-cache/${bazel_bin_container#/build-cache/}" ;;
    *) echo "Unexpected Bazel output path: $bazel_bin_container" >&2; exit 1 ;;
esac
release_source="$bazel_bin_host/release"
[[ -x "$release_source/runsc" && -d "$release_source/gvisor-bin" ]]
mkdir -p "$repo_dir/.wsl-artifacts/release/gvisor-bin"
install -m 755 "$release_source/runsc" "$repo_dir/.wsl-artifacts/release/runsc"
install -m 755 "$release_source/containerd-shim-runsc-v1" "$repo_dir/.wsl-artifacts/release/containerd-shim-runsc-v1"
for binary in "$release_source/gvisor-bin/"*; do
    install -m 755 "$binary" "$repo_dir/.wsl-artifacts/release/gvisor-bin/"
done
"$repo_dir/.wsl-artifacts/release/runsc" --version
