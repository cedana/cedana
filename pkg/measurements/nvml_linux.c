//go:build linux && cgo

#include "nvml_linux.h"

#include <dlfcn.h>
#include <stddef.h>

typedef int (*nvml_init_fn)(void);
typedef int (*nvml_shutdown_fn)(void);
typedef int (*nvml_device_by_uuid_fn)(const char*, void**);
typedef int (*nvml_memory_bus_width_fn)(void*, unsigned int*);
typedef int (*nvml_max_clock_fn)(void*, int, unsigned int*);

enum { CEDANA_NVML_CLOCK_MEMORY = 2 };

int cedana_nvml_memory_facts(
    const char* uuid,
    unsigned int* bus_width_bits,
    unsigned int* clock_mhz) {
    void* library = dlopen("libnvidia-ml.so.1", RTLD_NOW | RTLD_LOCAL);
    if (library == NULL) {
        return -1;
    }

    nvml_init_fn init = (nvml_init_fn)dlsym(library, "nvmlInit_v2");
    nvml_shutdown_fn shutdown = (nvml_shutdown_fn)dlsym(library, "nvmlShutdown");
    nvml_device_by_uuid_fn device_by_uuid =
        (nvml_device_by_uuid_fn)dlsym(library, "nvmlDeviceGetHandleByUUID");
    nvml_memory_bus_width_fn memory_bus_width =
        (nvml_memory_bus_width_fn)dlsym(library, "nvmlDeviceGetMemoryBusWidth");
    nvml_max_clock_fn max_clock =
        (nvml_max_clock_fn)dlsym(library, "nvmlDeviceGetMaxClockInfo");
    if (init == NULL || shutdown == NULL || device_by_uuid == NULL ||
        memory_bus_width == NULL || max_clock == NULL) {
        dlclose(library);
        return -2;
    }

    int result = init();
    if (result != 0) {
        dlclose(library);
        return result;
    }

    void* device = NULL;
    result = device_by_uuid(uuid, &device);
    if (result == 0) {
        result = memory_bus_width(device, bus_width_bits);
    }
    if (result == 0) {
        result = max_clock(device, CEDANA_NVML_CLOCK_MEMORY, clock_mhz);
    }

    shutdown();
    dlclose(library);
    return result;
}