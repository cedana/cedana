// Test-only glibc audit library: influence the real CUDA arena reservation
// after restore, without replacing allocation, mapping, copying, or ownership
// checks.
#define _GNU_SOURCE
#include <link.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef int (*get_proc_fn)(const char *, void **, int, uint64_t, void *);
typedef int (*total_mem_fn)(size_t *, int);
typedef int (*reserve_fn)(uint64_t *, size_t, size_t, uint64_t, uint64_t);
static get_proc_fn real_get_proc;
static reserve_fn real_reserve;
static total_mem_fn real_total_mem;
unsigned int la_version(unsigned int version) { return LAV_CURRENT; }
unsigned int la_objopen(struct link_map *map, Lmid_t id, uintptr_t *cookie) {
  return LA_FLG_BINDTO | LA_FLG_BINDFROM;
}
static int reserve_near_old_global(uint64_t *address, size_t size,
                                   size_t alignment, uint64_t hint,
                                   uint64_t flags) {
  const char *path = getenv("CEDANA_TEST_OLD_GLOBAL_FILE");
  FILE *file = path ? fopen(path, "r") : NULL;
  unsigned long long old = 0;
  if (file) {
    if (fscanf(file, "%llx", &old) != 1) {
      fclose(file);
      return 1; // CUDA_ERROR_INVALID_VALUE
    }
    fclose(file);
  }
  if (old)
    hint = (old & ~((uint64_t)(2 * 1024 * 1024) - 1)) - 2 * 1024 * 1024;
  int result = real_reserve(address, size, alignment, hint, flags);
  if (old)
    fprintf(stderr, "TEST_ARENA old=%llx hint=%llx actual=%llx status=%d\n",
            old, (unsigned long long)hint,
            result == 0 ? (unsigned long long)*address : 0, result);
  return result;
}
// The controller sizes its VA arena at eight times this figure. Limit only
// that test setup to 64 MiB, avoiding huge reservations beside an ASLR address.
static int arena_capacity(size_t *bytes, int device) {
  int result = real_total_mem(bytes, device);
  const char *path = getenv("CEDANA_TEST_OLD_GLOBAL_FILE");
  FILE *file = path ? fopen(path, "r") : NULL;
  if (file) {
    fclose(file);
    if (result == 0 && *bytes > 8 * 1024 * 1024)
      *bytes = 8 * 1024 * 1024;
  }
  return result;
}
static int get_proc(const char *name, void **fn, int version, uint64_t flags,
                    void *status) {
  int result = real_get_proc(name, fn, version, flags, status);
  if (result == 0 && strcmp(name, "cuMemAddressReserve") == 0) {
    real_reserve = (reserve_fn)*fn;
    *fn = reserve_near_old_global;
  }
  if (result == 0 && strcmp(name, "cuDeviceTotalMem") == 0) {
    real_total_mem = (total_mem_fn)*fn;
    *fn = arena_capacity;
  }
  return result;
}
uintptr_t la_symbind64(Elf64_Sym *symbol, unsigned int ndx, uintptr_t *ref,
                       uintptr_t *def, unsigned int *flags, const char *name) {
  if (strcmp(name, "cuGetProcAddress") == 0) {
    real_get_proc = (get_proc_fn)symbol->st_value;
    return (uintptr_t)get_proc;
  }
  return symbol->st_value;
}
