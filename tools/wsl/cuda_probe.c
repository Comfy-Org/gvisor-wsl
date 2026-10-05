// Copyright 2026 The gVisor Authors.
// SPDX-License-Identifier: Apache-2.0
// CUDA Driver API smoke test without a build-time CUDA SDK dependency.
#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

typedef int CUresult;
typedef int CUdevice;
typedef uint64_t CUdeviceptr;
typedef void *CUcontext;
typedef void *CUmodule;
typedef void *CUfunction;
typedef void *CUstream;

static void check(const char *name, CUresult result) {
  if (result) {
    fprintf(stderr, "%s failed: CUDA error %d\n", name, result);
    exit(1);
  }
}

// 256 threads add seven to independent uint32 elements uploaded by the CPU.
static const char ptx[] =
    ".version 8.0\n"
    ".target sm_80\n"
    ".address_size 64\n"
    ".visible .entry add_seven(.param .u64 data) {\n"
    "  .reg .u32 %r<3>;\n"
    "  .reg .u64 %rd<4>;\n"
    "  ld.param.u64 %rd1, [data];\n"
    "  mov.u32 %r1, %tid.x;\n"
    "  mul.wide.u32 %rd2, %r1, 4;\n"
    "  add.u64 %rd3, %rd1, %rd2;\n"
    "  ld.global.u32 %r2, [%rd3];\n"
    "  add.u32 %r2, %r2, 7;\n"
    "  st.global.u32 [%rd3], %r2;\n"
    "  ret;\n"
    "}\n";

int main(void) {
  setvbuf(stdout, NULL, _IONBF, 0);
  void *lib = dlopen("libcuda.so.1", RTLD_NOW | RTLD_LOCAL);
  if (!lib) {
    fprintf(stderr, "dlopen: %s\n", dlerror());
    return 1;
  }
#define LOAD(name, signature)                              \
  CUresult (*name) signature = dlsym(lib, #name);           \
  if (!name) {                                            \
    fprintf(stderr, "dlsym " #name ": %s\n", dlerror());   \
    return 1;                                             \
  }
  LOAD(cuInit, (unsigned int))
  LOAD(cuDeviceGetCount, (int *))
  LOAD(cuDeviceGet, (CUdevice *, int))
  LOAD(cuDeviceGetName, (char *, int, CUdevice))
  LOAD(cuCtxCreate_v2, (CUcontext *, unsigned int, CUdevice))
  LOAD(cuCtxDestroy_v2, (CUcontext))
  LOAD(cuCtxSynchronize, (void))
  LOAD(cuMemAlloc_v2, (CUdeviceptr *, size_t))
  LOAD(cuMemGetInfo_v2, (size_t *, size_t *))
  LOAD(cuMemFree_v2, (CUdeviceptr))
  LOAD(cuMemcpyHtoD_v2, (CUdeviceptr, const void *, size_t))
  LOAD(cuMemcpyDtoH_v2, (void *, CUdeviceptr, size_t))
  LOAD(cuModuleLoadData, (CUmodule *, const void *))
  LOAD(cuModuleUnload, (CUmodule))
  LOAD(cuModuleGetFunction, (CUfunction *, CUmodule, const char *))
  LOAD(cuLaunchKernel, (CUfunction, unsigned int, unsigned int, unsigned int,
                       unsigned int, unsigned int, unsigned int, unsigned int,
                       CUstream, void **, void **))
#undef LOAD

  check("cuInit", cuInit(0));
  int count = 0;
  check("cuDeviceGetCount", cuDeviceGetCount(&count));
  if (!count) {
    fputs("No CUDA device\n", stderr);
    return 1;
  }
  CUdevice dev;
  char name[256];
  check("cuDeviceGet", cuDeviceGet(&dev, 0));
  check("cuDeviceGetName", cuDeviceGetName(name, sizeof(name), dev));
  printf("devices=%d device=%s\n", count, name);

  CUcontext ctx;
  check("cuCtxCreate_v2", cuCtxCreate_v2(&ctx, 0, dev));
  size_t free_bytes, total_bytes;
  check("cuMemGetInfo_v2", cuMemGetInfo_v2(&free_bytes, &total_bytes));
  if (total_bytes == 0 || free_bytes > total_bytes) {
    fputs("Invalid CUDA memory budget\n", stderr);
    return 1;
  }
  printf("memory free=%zu total=%zu\n", free_bytes, total_bytes);
  CUmodule module;
  check("cuModuleLoadData", cuModuleLoadData(&module, ptx));
  CUfunction kernel;
  check("cuModuleGetFunction", cuModuleGetFunction(&kernel, module, "add_seven"));

  CUdeviceptr ptr;
  uint32_t data[256];
  check("cuMemAlloc_v2", cuMemAlloc_v2(&ptr, sizeof(data)));
  for (unsigned int iteration = 0; iteration < 3; ++iteration) {
    for (unsigned int i = 0; i < 256; ++i) data[i] = i + iteration;
    check("cuMemcpyHtoD_v2", cuMemcpyHtoD_v2(ptr, data, sizeof(data)));
    void *args[] = {&ptr};
    check("cuLaunchKernel", cuLaunchKernel(kernel, 1, 1, 1, 256, 1, 1,
                                           0, NULL, args, NULL));
    check("cuCtxSynchronize", cuCtxSynchronize());
    check("cuMemcpyDtoH_v2", cuMemcpyDtoH_v2(data, ptr, sizeof(data)));
    for (unsigned int i = 0; i < 256; ++i) {
      if (data[i] != i + iteration + 7) {
        fprintf(stderr, "Mismatch at %u: got %u, expected %u\n",
                i, data[i], i + iteration + 7);
        return 1;
      }
    }
  }
  check("cuMemFree_v2", cuMemFree_v2(ptr));
  check("cuModuleUnload", cuModuleUnload(module));
  check("cuCtxDestroy_v2", cuCtxDestroy_v2(ctx));
  puts("PASS: 3 kernel launches, HtoD/DtoH copies, 768 values verified, cleanup");
  dlclose(lib);
  return 0;
}
