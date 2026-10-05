#!/usr/bin/env bash
# Build/test the WSL fork with the same compiler dependencies on every host.
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
bazelisk_bin="${WSL_BAZELISK:-$(command -v bazelisk || true)}"
if [[ ! -x "$bazelisk_bin" ]]; then
    echo "Set WSL_BAZELISK to the absolute path of your bazelisk binary." >&2
    exit 1
fi
mkdir -p "$repo_dir/.build-cache"
docker run --rm --network=host --security-opt=seccomp=unconfined \
    --user "$(id -u):$(id -g)" \
    -e BAZELISK_HOME=/build-cache/bazelisk \
    -v "$repo_dir:/src" \
    -v "$repo_dir/.build-cache:/build-cache" \
    -v "$bazelisk_bin:/usr/local/bin/bazelisk:ro" \
    -w /src gvisor-wsl-build:local \
    --batch --output_user_root=/build-cache/bazel "$@"
