# Experimental WSL DXG proxy

**Trusted compatibility testing only. This fork is not ready to isolate hostile
custom nodes. Do not deploy it as a security boundary or replace your system
runtime with it.** The feature is disabled by default and requires both
`--dxgproxy=experimental` and explicit OCI authorization for `/dev/dxg`.

## Status (2026-10-02)

Local branch: `wsl-dxgproxy`, based on upstream gVisor master. No remote fork or PR
has been published. This is a reviewable compatibility prototype, not a proposed
production GPU security policy.

Verified on Ubuntu 24.04 in WSL2, kernel
`6.18.40.1-microsoft-standard-WSL2`, RTX 5080 Laptop GPU, NVIDIA Linux-visible
driver 570.142 / Windows driver 572.97, CUDA 12.8:

- `nvidia-smi -L` inside systrap reports the real GPU.
- CUDA initialization, context creation, allocations and teardown succeed.
- The current-process video-memory budget query succeeds (`cuMemGetInfo_v2`);
  its exact 56-byte `LX_DXQUERYVIDEOMEMORYINFO` UAPI was checked against the WSL
  kernel. PyTorch CUDA allocations require this query.
- The Driver API test compiles PTX through the installed driver, launches a
  256-thread addition kernel three times, copies memory both ways, and verifies
  all 768 results.
- Integration checks reject unknown/malformed ioctls and use of an inherited
  DXG FD from a different memory manager after fork.
- Device access fails without the opt-in flag or without the OCI device entry.
- The eight affected package suites, including their static-analysis targets,
  pass (24 test targets).

The sibling `ComfyUI_secure_nodes` branch `wsl-gvisor-realms` now runs managed
Python 3.13 / PyTorch 2.8.0+cu128 inside sealed node realms. Its trusted tensor
probe passed 10,000 alternating control/CPU/CUDA dispatches across two realms,
with warm reuse, crash recovery and cross-tenant scrub. A converted Checkerboard
pack also passed the actual manifest-to-CloudExecutionBackend routing. See
that repository's `backend/scripts/wsl/README.md` for commands and measurement
limits. No bubblewrap is involved; the broker uses private mounted UDS files.
CPU-staged transport remains available. The fork now also supports explicit
CUDA VMM resource-FD IPC for GPU-resident realm inputs and outputs.

### Experimental CUDA IPC

`LX_DXSHAREOBJECTS`, `LX_DXQUERYRESOURCEINFOFROMNTHANDLE` and
`LX_DXOPENRESOURCEFROMNTHANDLE` are modeled using the actual Microsoft UAPI
(including nested output buffers). The helper retains a private socket endpoint
in addition to its device FD. Resource FDs cross that private socket with
SCM_RIGHTS; a raw application FD is never looked up in the Sentry FD table.
Temporary helper FDs are closed after every operation. Send metadata is in a
guest-read-only page, and descriptor numbers returned in writable memory are
not trusted: the helper knows its next free private FD slot.

The Sentry wraps resource FDs as opaque, non-mappable capabilities. Only those
capabilities can be sent over a connected host UDS; ordinary host-backed file
rights remain disallowed. Anonymous FDs donated by the trusted broker are
wrapped in this mode; dxgkrnl checks resource `f_ops` on import. This is still an
experimental broker contract, not a general secure anonymous-FD import policy.

This supports public `cuMemCreate` / `cuMemExportToShareableHandle` /
`cuMemImportFromShareableHandle` APIs. It does **not** transparently support
legacy `cuIpcOpenMemHandle` across independent Sentries: this WSL driver embeds
PID/FD references in those handles and uses `pidfd_getfd`. Guest PID isolation
has not been weakened. CUDA event/sync-file sharing is also not implemented;
the test carrier synchronizes GPU work explicitly before handing off ownership.

The sibling realm harness uses `--dxg --channel cuda_ipc` and
`benchmark.py --resident`. Its initial VMM carrier copied GPU->GPU at both
boundaries; those measurements did not satisfy the no-copy buffer contract.
The replacement uses independently shareable allocations and storage-owned
DLPack mappings, with no transport payload copies. Writable aliases are
explicitly trusted-only, not private guest copies or cross-tenant cache safety.
See the sibling README/results for allocator setup, lifetime tests and the
historical outgoing-FD reference leak.

This establishes a narrow PyTorch/custom-node compatibility slice on this WSL
machine. It does **not** establish full ComfyUI/model compatibility, a multi-hour
soak, robustness under driver reset, or hostile-workload isolation.

## Why a separate proxy

Native Linux nvproxy models NVIDIA's `/dev/nvidia*` kernel ABI. WSL exposes
`/dev/dxg`, a different ABI. The relevant Microsoft sources are
`drivers/hv/dxgkrnl/{ioctl.c,dxgvmbus.c}` and
`include/uapi/misc/d3dkmthk.h` in
[WSL2-Linux-Kernel](https://github.com/microsoft/WSL2-Linux-Kernel).
The ABI used here was checked against commit
`14794180686c2fb6307fbe359c359bec765249f3` (`linux-msft-wsl-6.18.y`).

DXG binds a device open to the opener's host TGID. It also installs CPU fence
pages with `vm_mmap`/`io_remap_pfn_range` in the calling process's address space.
Forwarding from the Sentry would give the application pointers into the wrong
address space. This implementation instead:

1. Creates a dedicated, ptrace-controlled systrap helper sharing the guest MM.
2. Gives it a **private** FD table, pruned with `close_range`, and opens only
   `dxg` relative to the restricted device-gofer directory with `O_NOFOLLOW`.
3. Executes exact allowlisted ioctl numbers with application-space pointers.
   Normal guest execution threads still trap these host syscalls.
4. Materializes nested buffers, conservatively pins host-memory allocation VMAs,
   and records driver-created fence pages in the guest MM.
5. Preserves fence pages across broad PMA invalidations and reserves their
   addresses until the DXG file closes. Guest replacement, unmap, remap,
   protection changes and decommit of those ranges return `EBUSY`.
6. Reserves the high-address region unavailable to systrap applications using
   PROT_NONE/MAP_NORESERVE guards, preserving stub code and trusted mappings.
   Otherwise host ASLR can place a kernel-created fence above the guest address
   limit, causing registration failure during CUDA warmup. The guards remain
   across address-space recycling; native (non-DXG) systrap is unchanged.

The observed failures before steps 4–5 were kernel `pin_user_pages_fast` EFAULT
(reported to CUDA as allocation failure) and SIGSEGV when gVisor invalidated an
untracked fence page. No bubblewrap is involved.

## Build

Prerequisites: native Docker Engine in WSL, access to the Docker socket,
Bazelisk, and a working NVIDIA CDI installation. No Linux NVIDIA kernel module
is installed by these scripts; the existing WSL/Windows driver is used.

From the repository root:

```sh
docker build -t gvisor-wsl-build:local tools/wsl
export WSL_BAZELISK=/absolute/path/to/bazelisk
bash tools/wsl/build.sh
```

This writes the complete release layout to `.wsl-artifacts/release/`: `runsc`,
the containerd shim and `gvisor-bin/` sidecars. Keep the sidecars next to runsc;
an isolated runsc binary is not sufficient for this upstream revision. The
scripts do not change Docker's runtime configuration or `/usr/bin/runsc`.

If the current shell has stale group membership, use `sg docker -c '…'` with
`WSL_BAZELISK` supplied inside that command. Do not make the Docker socket
world-writable.

## Tests

```sh
bash tools/wsl/bazel.sh test --jobs=8 --test_output=errors \
  //pkg/abi/dxg:all //pkg/sentry/devices/dxgproxy:all \
  //pkg/sentry/mm:all //pkg/sentry/platform/systrap:all \
  //runsc/config:all //runsc/specutils:all \
  //pkg/sentry/socket/unix/transport:all //pkg/sentry/socket/control:all

python3 tools/wsl/smoke.py nvidia-smi
python3 tools/wsl/smoke.py cuda
python3 tools/wsl/smoke.py abi
python3 tools/wsl/smoke.py disabled
python3 tools/wsl/smoke.py no-device
```

`smoke.py` uses a disposable **outer runc container with SYS_ADMIN, SYS_PTRACE
and unrestricted outer seccomp/AppArmor**, solely to launch a nested gVisor
runtime. Only trusted test programs belong in this harness. The inner process
has no capabilities, no networking, a read-only root and memory-backed writable
directories. The real device is provided by CDI (`nvidia.com/gpu=all`); the
script derives its current minor number from `stat`, not a hard-coded host value.
The OCI fixture and output are retained under `.wsl-artifacts/`.

The default CUDA test image is `nvidia/cuda:12.8.1-base-ubuntu24.04`. The tested
image digest was
`nvidia/cuda@sha256:133c78a0575303be34164d0b90137a042172bdf60696af01a3c424ab402d86e2`;
pass it with `--image` to pin the test. `--debug` records detailed runtime logs.

To verify the ABI constants/offsets against an independently checked-out WSL
kernel, compile `tools/wsl/check_abi.c` with that kernel's `include/uapi` on the
include path. This uses the actual C UAPI, rather than only testing Go constants
against themselves.

## Known blockers before untrusted custom nodes

- **Mutable arguments / TOCTOU:** buffer inspection does not freeze guest
  threads or marshal immutable ioctl snapshots. Concurrent mutation can change
  nested pointers, lengths, output fence pointers and allocation identity after
  inspection. The private FD table prevents access to Sentry FDs, but does not
  solve these races.
- **Opaque private driver data:** the allowlist covers outer DXG request IDs,
  not NVIDIA escape/private-data semantics. It is not nvproxy's audited native
  NVIDIA ABI policy and must not be presented as equivalent.
- **Exact pin lifetime and size:** non-standard allocation sizes are reported
  by Windows after the host request. The prototype pins a bounded suffix of the
  containing writable VMA, retaining references until device close, with a
  conservative 512 MiB budget per open. This can over-pin and reject legitimate
  workloads; it is not sufficient against malicious lengths or mapping races.
  Per-allocation accounting and race-free pin/mapping lifetimes are required.
- **Host-chosen addresses:** mapping registration is serialized with MM changes,
  rejects guest VMA collisions, and disables the file on registration failure.
  High-address guards prevent mappings in systrap's excluded region, but do not
  guarantee a collision-free window among lazily mapped guest VMAs, or provide
  a race-free trusted channel for ioctl output metadata.
- **Mapping semantics:** fence pages have no Sentry-side `memmap.File`
  translation. Sentry copies/futexes against them are unsupported. Reservations
  persist until close, even after individual GPU objects are destroyed. Forked
  address spaces do not inherit fences; inherited DXG ioctls return `EXDEV`.
- **Unsupported operations:** checkpoint is explicitly rejected for this mode;
  legacy PID-based IPC across Sentries, sync-file/event-sharing APIs, additional DXG ioctls,
  non-systrap platforms and combinations with native nvproxy are unsupported.
- **Coverage:** test driver resets, process death during ioctls, concurrent
  contexts, allocation churn, multiple opens, adverse resource limits, fuzzing,
  other driver/WSL versions and real model/custom-sampler workloads. The small
  PyTorch node tests do not cover those cases. A fresh
  security review is required before enabling untrusted code.

Upstream submission should start with a design discussion about process-bound
device ioctls and platform-owned mappings. Keep this feature experimental until
the blockers above are resolved. Per upstream CONTRIBUTING.md, AI-assisted work
requires human review and an `Assisted-by: Codex` disclosure before submission.
