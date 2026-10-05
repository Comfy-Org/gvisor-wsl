#!/usr/bin/env python3
# Copyright 2026 The gVisor Authors.
# SPDX-License-Identifier: Apache-2.0
"""Trusted WSL integration tests; never installs a Docker runtime or edits its config."""

import argparse
import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["cuda", "nvidia-smi", "abi", "disabled", "no-device"])
    parser.add_argument("--image", default="nvidia/cuda:12.8.1-base-ubuntu24.04")
    parser.add_argument("--debug", action="store_true")
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[2]
    artifacts = repo / ".wsl-artifacts"
    runtime = artifacts / "release"
    if not (runtime / "runsc").is_file() or not (runtime / "gvisor-bin/gvisor_sentry").is_file():
        parser.error("Build the complete release first: bash tools/wsl/build.sh")
    device = os.stat("/dev/dxg")
    if not stat.S_ISCHR(device.st_mode) or os.major(device.st_rdev) != 10:
        parser.error("/dev/dxg is not a WSL misc character device")
    artifacts.mkdir(exist_ok=True)
    bundle = Path(tempfile.mkdtemp(prefix=f"{args.mode}-", dir=artifacts))
    bundle_relative = bundle.relative_to(repo)
    # Only trusted probes execute in the outer privileged harness. The nested
    # gVisor workload has no capabilities, networking or writable host mounts.
    subprocess.run([
        "docker", "run", "--rm", "--runtime=runc", "--network=none",
        "--user", f"{os.getuid()}:{os.getgid()}",
        "-v", f"{repo}:/src", "-w", "/src", "--entrypoint", "gcc",
        "gvisor-wsl-build:local", "-Wall", "-Wextra", "-Werror", "-O2",
        "-o", str(bundle_relative / "cuda_probe"), "tools/wsl/cuda_probe.c", "-ldl",
    ], check=True)
    subprocess.run([
        "docker", "run", "--rm", "--runtime=runc", "--network=none",
        "--user", f"{os.getuid()}:{os.getgid()}",
        "-v", f"{repo}:/src", "-w", "/src", "--entrypoint", "gcc",
        "gvisor-wsl-build:local", "-Wall", "-Wextra", "-Werror", "-O2",
        "-o", str(bundle_relative / "dxg_abi_probe"), "tools/wsl/dxg_abi_probe.c",
    ], check=True)
    if args.mode == "cuda":
        command = ["/cuda_probe"]
    elif args.mode == "nvidia-smi":
        command = ["/usr/bin/nvidia-smi", "-L"]
    else:
        command = ["/dxg_abi_probe"]
        if args.mode in ("disabled", "no-device"):
            command.append("denied")
    spec = {
        "ociVersion": "1.0.2",
        "root": {"path": "/", "readonly": True},
        "hostname": "dxg-smoke",
        "process": {
            "terminal": False, "user": {"uid": 0, "gid": 0},
            "args": command, "cwd": "/", "noNewPrivileges": True,
            "env": ["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                    "LD_LIBRARY_PATH=/usr/lib/wsl/lib", "NVIDIA_VISIBLE_DEVICES=void"],
            "capabilities": {key: [] for key in ("bounding", "effective", "inheritable", "permitted", "ambient")},
        },
        "mounts": [
            {"destination": "/proc", "type": "proc", "source": "proc"},
            {"destination": "/dev", "type": "tmpfs", "source": "tmpfs"},
            {"destination": "/dev/pts", "type": "devpts", "source": "devpts",
             "options": ["newinstance", "ptmxmode=0666", "mode=0620"]},
            {"destination": "/dev/shm", "type": "tmpfs", "source": "shm"},
            {"destination": "/tmp", "type": "tmpfs", "source": "tmpfs"},
            {"destination": "/sys", "type": "sysfs", "source": "sysfs", "options": ["ro"]},
        ],
        "linux": {
            "namespaces": [{"type": name} for name in ("pid", "ipc", "uts", "mount")],
            "devices": [] if args.mode == "no-device" else [{
                "path": "/dev/dxg", "type": "c", "major": 10,
                "minor": os.minor(device.st_rdev), "fileMode": 0o666, "uid": 0, "gid": 0,
            }],
        },
    }
    (bundle / "config.json").write_text(json.dumps(spec, indent=2) + "\n")
    container_name = f"dxg-smoke-{bundle.name}"
    cmd = [
        "docker", "run", "--rm", "--name", container_name, "--runtime=runc", "--network=none",
        "--device", "nvidia.com/gpu=all", "--cap-add", "SYS_ADMIN", "--cap-add", "SYS_PTRACE",
        "--security-opt", "seccomp=unconfined", "--security-opt", "apparmor=unconfined",
        "-v", f"{runtime}:/runtime:ro", "-v", f"{bundle}:/bundle:ro",
        "-v", f"{bundle / 'cuda_probe'}:/cuda_probe:ro",
        "-v", f"{bundle / 'dxg_abi_probe'}:/dxg_abi_probe:ro",
        args.image, "/runtime/runsc", "--root=/tmp/runsc", "--platform=systrap",
        "--network=none", "--ignore-cgroups", "--overlay2=all:memory",
    ]
    if args.mode != "disabled":
        cmd.append("--dxgproxy=experimental")
    if args.debug:
        cmd += ["--debug", "--alsologtostderr"]
    cmd += ["run", "--bundle=/bundle", "dxg-smoke"]
    print(f"Running {args.mode}; fixture and output: {bundle}", flush=True)
    try:
        result = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=120)
    except subprocess.TimeoutExpired:
        subprocess.run(["docker", "stop", "--time", "5", container_name], check=False)
        raise
    (bundle / "output.log").write_text(result.stdout)
    print(result.stdout, end="")
    raise SystemExit(result.returncode)


if __name__ == "__main__":
    main()
