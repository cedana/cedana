#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cuda.h>
#include <sys/mman.h>
#include <unistd.h>
#include <vector>

constexpr char invalidPtx[] = ".version 7.0\n.target sm_70\n.address_size 64\nNOT_VALID_PTX\n";

// Run natively and through Cedana; a gate holds every successful module for
// checkpointing.
static void check(CUresult result, const char* operation) {
    if (result != CUDA_SUCCESS) {
        std::fprintf(stderr, "%s failed: CUDA error %d\n", operation, result);
        std::exit(1);
    }
}
static void require(bool condition, const char* description) {
    if (!condition) {
        std::fprintf(stderr, "FAIL: %s\n", description);
        std::exit(1);
    }
}
int main(int argc, char** argv) {
    CUdevice device;
    CUcontext context;
    check(cuInit(0), "init");
    check(cuDeviceGet(&device, 0), "device");
    check(cuDevicePrimaryCtxRetain(&context, device), "context");
    check(cuCtxSetCurrent(context), "set context");
    // Deliberately do NOT call cuDriverGetVersion first (LAMMPS's call order).
    const char* ptxSource =
        ".version 7.0\n.target sm_70\n.address_size 64\n"
        ".visible .entry probe(.param .u64 out) {\n"
        ".reg .u64 %rd; .reg .u32 %r;\n"
        "ld.param.u64 %rd, [out]; mov.u32 %r, 42; "
        "st.global.u32 [%rd], %r; ret; }\n";
    // The terminating NUL is the last readable byte: one extra copied byte
    // faults.
    size_t pageSize = static_cast<size_t>(sysconf(_SC_PAGESIZE));
    char* pages = static_cast<char*>(
        mmap(nullptr, 2 * pageSize, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0));
    require(pages != MAP_FAILED, "allocate guarded PTX");
    require(mprotect(pages + pageSize, pageSize, PROT_NONE) == 0, "protect next page");
    size_t ptxSize = std::strlen(ptxSource) + 1;
    char* ptx = pages + pageSize - ptxSize;
    std::memcpy(ptx, ptxSource, ptxSize);
    CUmodule noOptions;
    check(cuModuleLoadDataEx(&noOptions, ptx, 0, nullptr, nullptr), "zero options");
    check(cuModuleUnload(noOptions), "unload zero-options module");

    char info[4096] = {}, error[4096] = {};
    // Include both log types; put one size before its buffer and one after.
    CUjit_option options[] = {CU_JIT_INFO_LOG_BUFFER_SIZE_BYTES, CU_JIT_INFO_LOG_BUFFER,
                              CU_JIT_ERROR_LOG_BUFFER,           CU_JIT_ERROR_LOG_BUFFER_SIZE_BYTES,
                              CU_JIT_THREADS_PER_BLOCK,          CU_JIT_WALL_TIME};
    void* values[] = {reinterpret_cast<void*>(sizeof(info)),
                      info,
                      error,
                      reinterpret_cast<void*>(sizeof(error)),
                      reinterpret_cast<void*>(128),
                      nullptr};
    float wall = -123.0f;
    values[5] = &wall;
    CUmodule module;
    check(cuModuleLoadDataEx(&module, ptx, 6, options, values), "JIT options and logs");
    require(std::memchr(info, 0, sizeof(info)) && std::memchr(error, 0, sizeof(error)),
            "returned logs are terminated");
    require(reinterpret_cast<uintptr_t>(values[0]) <= sizeof(info) &&
                reinterpret_cast<uintptr_t>(values[3]) <= sizeof(error),
            "returned log sizes stay within the original capacities");
    require(values[1] == info && values[2] == error,
            "application log buffer pointers are preserved");
    require(reinterpret_cast<uintptr_t>(values[4]) > 0 &&
                reinterpret_cast<uintptr_t>(values[4]) <= 1024,
            "threads-per-block output is copied back");

    float returnedWall = -1.0f;
    std::memcpy(&returnedWall, &values[5], sizeof(returnedWall));
    require(wall == -123.0f && values[5] != &wall,
            "wall time overwrites the option slot, leaving the pointed-to float "
            "untouched");
    require(std::isfinite(returnedWall) && returnedWall >= 0,
            "wall-time option slot contains a nonnegative float");
    CUjit_option wallOption = CU_JIT_WALL_TIME;
    void* wallValue = nullptr;
    CUmodule wallModule;
    check(cuModuleLoadDataEx(&wallModule, ptx, 1, &wallOption, &wallValue),
          "wall-time output with initially null slot");
    std::memcpy(&returnedWall, &wallValue, sizeof(returnedWall));
    require(std::isfinite(returnedWall) && returnedWall >= 0,
            "initially null wall-time slot contains a nonnegative float");
    check(cuModuleUnload(wallModule), "unload wall-time module");
    char diagnostic[4096] = {};
    CUjit_option badOptions[] = {CU_JIT_ERROR_LOG_BUFFER, CU_JIT_ERROR_LOG_BUFFER_SIZE_BYTES};
    void* badValues[] = {diagnostic, reinterpret_cast<void*>(sizeof(diagnostic))};
    CUmodule rejected = nullptr;
    CUresult invalid = cuModuleLoadDataEx(&rejected, invalidPtx, 2, badOptions, badValues);
    require(invalid != CUDA_SUCCESS, "malformed PTX is rejected");
    require(diagnostic[0] != 0 && std::memchr(diagnostic, 0, sizeof(diagnostic)),
            "error diagnostics are copied back on failed compilation");
    require(badValues[0] == diagnostic &&
                reinterpret_cast<uintptr_t>(badValues[1]) <= sizeof(diagnostic),
            "failed compilation preserves the log pointer and returns its size");
    require(cuModuleLoadDataEx(nullptr, ptx, 0, nullptr, nullptr) == CUDA_ERROR_INVALID_VALUE,
            "null module output is rejected");

    std::vector<CUmodule> liveModules{module};
    auto load = [&](const char* operation, std::vector<CUjit_option> opts,
                    std::vector<void*> vals) {
        CUmodule loaded;
        check(cuModuleLoadDataEx(&loaded, ptx, opts.size(), opts.data(), vals.data()), operation);
        liveModules.push_back(loaded);
    };
    for (auto logOption : {CU_JIT_INFO_LOG_BUFFER, CU_JIT_ERROR_LOG_BUFFER}) {
        auto sizeOption = logOption == CU_JIT_INFO_LOG_BUFFER ? CU_JIT_INFO_LOG_BUFFER_SIZE_BYTES
                                                              : CU_JIT_ERROR_LOG_BUFFER_SIZE_BYTES;
        char sentinel[8];
        std::memset(sentinel, 'X', sizeof(sentinel));
        CUjit_option opts[] = {logOption, sizeOption};
        void* vals[] = {sentinel, nullptr};
        CUmodule loaded;
        check(cuModuleLoadDataEx(&loaded, ptx, 2, opts, vals), "zero-capacity log");
        liveModules.push_back(loaded);
        require(sentinel[0] == 0 && sentinel[1] == 'X',
                "zero-capacity log writes only the terminator");
        require(vals[0] == sentinel && vals[1] == nullptr, "zero log output slots");
        for (uintptr_t capacity : {uintptr_t(0), uintptr_t(8)}) {
            vals[0] = nullptr;
            vals[1] = reinterpret_cast<void*>(capacity);
            require(cuModuleLoadDataEx(&rejected, ptx, 2, opts, vals) == CUDA_ERROR_INVALID_VALUE,
                    "null log buffer rejected at zero and positive capacity");
        }
        vals[0] = sentinel;
        std::memset(sentinel, 'X', sizeof(sentinel));
        require(cuModuleLoadDataEx(&rejected, ptx, 1, opts, vals) == CUDA_ERROR_INVALID_VALUE,
                "missing log size rejected by driver");
        require(sentinel[0] == 'X', "invalid log configuration leaves buffer untouched");
    }
    load("duplicate equal scalars", {CU_JIT_OPTIMIZATION_LEVEL, CU_JIT_OPTIMIZATION_LEVEL},
         {nullptr, nullptr});
    load("duplicate different scalars", {CU_JIT_OPTIMIZATION_LEVEL, CU_JIT_OPTIMIZATION_LEVEL},
         {nullptr, reinterpret_cast<void*>(4)});
    load("option count exceeds enum count",
         std::vector<CUjit_option>(CU_JIT_NUM_OPTIONS + 1, CU_JIT_OPTIMIZATION_LEVEL),
         std::vector<void*>(CU_JIT_NUM_OPTIONS + 1, reinterpret_cast<void*>(4)));
    load("position independent code", {CU_JIT_POSITION_INDEPENDENT_CODE},
         {reinterpret_cast<void*>(1)});
    load("minimum CTAs", {CU_JIT_MIN_CTA_PER_SM, CU_JIT_MAX_THREADS_PER_BLOCK},
         {reinterpret_cast<void*>(1), reinterpret_cast<void*>(128)});
    load("maximum threads", {CU_JIT_MAX_THREADS_PER_BLOCK}, {reinterpret_cast<void*>(128)});
    load("override PTX directives",
         {CU_JIT_OVERRIDE_DIRECTIVE_VALUES, CU_JIT_MAX_THREADS_PER_BLOCK},
         {reinterpret_cast<void*>(1), reinterpret_cast<void*>(128)});
#if CUDA_VERSION >= 13000
    int driverVersion;
    check(cuDriverGetVersion(&driverVersion), "driver version for split compile");
    if (driverVersion >= 13000) {
        for (uintptr_t threads : {uintptr_t(0), uintptr_t(1), uintptr_t(2)}) {
            load("split compile", {CU_JIT_SPLIT_COMPILE}, {reinterpret_cast<void*>(threads)});
        }
    }
#endif
    // CUDA validates earlier duplicate pointers even though only the last is
    // written.
    char duplicateInfo[64] = {}, ignoredInfo[64];
    std::memset(ignoredInfo, 'X', sizeof(ignoredInfo));
    CUjit_option nullDuplicateOptions[] = {CU_JIT_INFO_LOG_BUFFER, CU_JIT_INFO_LOG_BUFFER,
                                           CU_JIT_INFO_LOG_BUFFER_SIZE_BYTES};
    void* nullDuplicateValues[] = {nullptr, duplicateInfo,
                                   reinterpret_cast<void*>(sizeof(duplicateInfo))};
    require(cuModuleLoadDataEx(&rejected, ptx, 3, nullDuplicateOptions, nullDuplicateValues) ==
                CUDA_ERROR_INVALID_VALUE,
            "null earlier duplicate log pointer is rejected");
    load("duplicate info logs",
         {CU_JIT_INFO_LOG_BUFFER, CU_JIT_INFO_LOG_BUFFER, CU_JIT_INFO_LOG_BUFFER_SIZE_BYTES},
         {ignoredInfo, duplicateInfo, reinterpret_cast<void*>(sizeof(duplicateInfo))});
    require(ignoredInfo[0] == 'X' && ignoredInfo[63] == 'X',
            "earlier duplicate info log is untouched");
    for (bool smallFirst : {false, true}) {
        char first[4096], last[4096];
        std::memset(first, 'A', sizeof(first));
        std::memset(last, 'B', sizeof(last));
        CUjit_option opts[] = {CU_JIT_ERROR_LOG_BUFFER, CU_JIT_ERROR_LOG_BUFFER,
                               CU_JIT_ERROR_LOG_BUFFER_SIZE_BYTES,
                               CU_JIT_ERROR_LOG_BUFFER_SIZE_BYTES};
        uintptr_t capacity = smallFirst ? 4096 : 16;
        void* vals[] = {first, last, reinterpret_cast<void*>(smallFirst ? 16 : 4096),
                        reinterpret_cast<void*>(capacity)};
        require(cuModuleLoadDataEx(&rejected, invalidPtx, 4, opts, vals) == CUDA_ERROR_INVALID_PTX,
                "duplicate error log compilation");
        require(first[0] == 'A' && first[4095] == 'A', "earlier duplicate buffer untouched");
        auto end = static_cast<char*>(std::memchr(last, 0, capacity));
        require(last[0] != 'B' && end != nullptr, "last capacity governs output");
        require(vals[2] == reinterpret_cast<void*>(smallFirst ? 16 : 4096),
                "earlier duplicate size unchanged");
        require(reinterpret_cast<uintptr_t>(vals[3]) <= capacity, "last size receives usage");
        if (capacity < sizeof(last)) {
            require(last[capacity] == 'B', "bytes beyond capacity preserved");
        }
    }
    std::vector<CUfunction> functions;
    for (auto loaded : liveModules) {
        CUfunction function;
        check(cuModuleGetFunction(&function, loaded, "probe"), "get JIT kernel");
        functions.push_back(function);
    }
    CUdeviceptr output;
    check(cuMemAlloc(&output, sizeof(int)), "allocate");
    std::puts("READY");
    std::fflush(stdout);
    if (argc > 1) {
        while (access(argv[1], F_OK) != 0) {
            usleep(100000);  // Hold live modules until the test releases the gate.
        }
    }
    void* parameters[] = {&output};
    for (auto function : functions) {
        check(cuMemsetD32(output, 0, 1), "reset result");
        check(cuLaunchKernel(function, 1, 1, 1, 1, 1, 1, 0, nullptr, parameters, nullptr),
              "launch after checkpoint");
        check(cuCtxSynchronize(), "synchronize");
        int result = 0;
        check(cuMemcpyDtoH(&result, output, sizeof(result)), "copy result");
        require(result == 42, "kernel writes expected result through restored handles");
    }
    check(cuMemFree(output), "free");
    for (auto loaded : liveModules) {
        check(cuModuleUnload(loaded), "unload");
    }
    check(cuDevicePrimaryCtxRelease(device), "release context");
    require(munmap(pages, 2 * pageSize) == 0, "release guarded PTX");
    std::puts("PASS module JIT regression: result=42");
}
