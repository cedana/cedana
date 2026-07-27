#ifndef CEDANA_NVML_LINUX_H
#define CEDANA_NVML_LINUX_H

int cedana_nvml_memory_facts(
    const char* uuid,
    unsigned int* bus_width_bits,
    unsigned int* clock_mhz);

#endif