#include <algorithm>
#include <cstdio>
#include <cstdlib>
#include <cuda.h>
#include <string>
#include <unistd.h>

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
    ".version 7.0\n.target sm_52\n.address_size 64\n";

int main(int argc, char **argv) {
  require(argc == 2, "usage: module-global-replay GATE_PATH");
  check(cuInit(0), "init");
  CUdevice device;
  CUcontext context;
  check(cuDeviceGet(&device, 0), "device");
  check(cuDevicePrimaryCtxRetain(&context, device), "context");
  check(cuCtxSetCurrent(context), "current context");
  CUmodule module;
  constexpr size_t elements = 64;
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

  int initial[elements];
  std::fill_n(initial, elements, -1);
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
  check(cuMemcpyHtoDAsync(global + 16 * sizeof(int), &value, sizeof(value),
                          stream),
        "interior global HtoDAsync after restore");
  check(cuStreamSynchronize(stream), "copy completion");
  int result[elements];
  check(cuMemcpyDtoH(result, global, bytes), "read global");
  for (size_t i = 0; i < elements; ++i) {
    require(result[i] == (i == 16 ? 42 : -1),
            "interior value and surrounding sentinels");
  }

  check(cuModuleUnload(module), "unload module");
  check(cuStreamDestroy(stream), "destroy stream");
  check(cuDevicePrimaryCtxRelease(device), "release context");
  std::puts("RESULT 42\nDONE");
}
