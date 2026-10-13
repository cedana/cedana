#include <algorithm>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cuda.h>
#include <string>
#include <unistd.h>
#include <vector>

static void check(CUresult result, const char *operation) {
  if (result != CUDA_SUCCESS) {
    std::fprintf(stderr, "FAIL %s: CUDA %d\n", operation, result);
    std::exit(1);
  }
}
static void require(bool ok, const char *operation) {
  if (!ok) {
    std::fprintf(stderr, "FAIL %s\n", operation);
    std::exit(1);
  }
}
static const char ptxHeader[] =
    ".version 7.0\n.target sm_80\n.address_size 64\n";

int main(int argc, char **argv) {
  require(argc == 2 || (argc == 3 && std::strcmp(argv[2], "allocation") == 0),
          "usage: module-global-replay GATE_PATH [allocation]");
  const bool allocationCase = argc == 3;
  check(cuInit(0), "init");
  CUdevice device;
  CUcontext context;
  check(cuDeviceGet(&device, 0), "device");
  check(cuDevicePrimaryCtxRetain(&context, device), "context");
  check(cuCtxSetCurrent(context), "current context");
  CUmodule module;
  // A larger recorded range covers the driver's arena alignment rounding.
  const size_t elements = allocationCase ? 128 * 1024 * 1024 : 64;
  const std::string ptx = std::string(ptxHeader) +
                          ".visible .global .align 4 .u32 arr[" +
                          std::to_string(elements) + "];\n";
  check(cuModuleLoadData(&module, ptx.c_str()), "module");
  CUdeviceptr global;
  size_t bytes;
  check(cuModuleGetGlobal(&global, &bytes, module, "arr"), "global");
  require(bytes == elements * sizeof(int), "global size");
  CUstream stream;
  check(cuStreamCreate(&stream, CU_STREAM_NON_BLOCKING), "stream");

  int initial[64];
  std::fill_n(initial, 64, -1);
  check(cuMemcpyHtoD(global, initial, sizeof(initial)), "initialize sentinels");
  {
    // Preserve the recorded global across restore before making the interior
    // copy.
    const std::string gate = argv[1];
    FILE *ready = std::fopen((gate + ".ready").c_str(), "w");
    require(ready != nullptr, "create checkpoint-ready marker");
    require(std::fprintf(ready, "%llx\n", (unsigned long long)global) > 0,
            "record old global address");
    require(std::fclose(ready) == 0, "close checkpoint-ready marker");
    while (access(gate.c_str(), F_OK) != 0) {
      usleep(100000);
    }
  }

  int value = 42;
  if (allocationCase) {
    // The test audit library places the new arena around the recorded global.
    constexpr size_t allocationBytes = 4 * 1024 * 1024;
    CUdeviceptr fresh;
    check(cuMemAlloc(&fresh, allocationBytes), "post-restore allocation");
    const CUdeviceptr target = std::max(fresh, global) + 16 * sizeof(int);
    require(target >= fresh &&
                target - fresh <= allocationBytes - sizeof(int) &&
                target >= global && target - global <= bytes - sizeof(int),
            "post-restore allocation must overlap the old global range");
    std::printf("OVERLAP old=%llx allocation=%llx bytes=%zu\n",
                (unsigned long long)global, (unsigned long long)fresh,
                allocationBytes);
    std::vector<int> buffer(allocationBytes / sizeof(int), -7);
    check(cuMemcpyHtoD(fresh, buffer.data(), allocationBytes),
          "initialize new allocation");
    CUdeviceptr currentGlobal;
    size_t currentBytes;
    check(cuModuleGetGlobal(&currentGlobal, &currentBytes, module, "arr"),
          "restored global");
    require(currentGlobal != global && currentBytes == bytes,
            "global actually relocated");
    const CUdeviceptr globalTarget = currentGlobal + (target - global);
    int sentinel = -1;
    check(cuMemcpyHtoD(globalTarget, &sentinel, sizeof(sentinel)),
          "initialize restored global sentinel");
    check(cuMemcpyHtoDAsync(target, &value, sizeof(value), stream),
          "copy into allocation at old global interior address");
    check(cuStreamSynchronize(stream), "copy completion");
    check(cuMemcpyDtoH(buffer.data(), fresh, allocationBytes),
          "read new allocation");
    const size_t targetIndex = (target - fresh) / sizeof(int);
    for (size_t i = 0; i < buffer.size(); ++i) {
      require(buffer[i] == (i == targetIndex ? 42 : -7),
              "copy reaches new allocation, preserving surrounding bytes");
    }
    check(cuMemcpyDtoH(&sentinel, globalTarget, sizeof(sentinel)),
          "read restored global sentinel");
    require(sentinel == -1, "allocation copy must not alter restored global");
    check(cuMemFree(fresh), "free new allocation");
  } else {
    check(cuMemcpyHtoDAsync(global + 16 * sizeof(int), &value, sizeof(value),
                            stream),
          "interior global HtoDAsync after restore");
    check(cuStreamSynchronize(stream), "copy completion");
    int result[64];
    check(cuMemcpyDtoH(result, global, bytes), "read global");
    for (int i = 0; i < 64; ++i) {
      require(result[i] == (i == 16 ? 42 : -1),
              "interior value and surrounding sentinels");
    }
  }

  check(cuModuleUnload(module), "unload module");
  check(cuStreamDestroy(stream), "destroy stream");
  check(cuDevicePrimaryCtxRelease(device), "release context");
  std::puts("RESULT 42\nDONE");
}
