//go:build linux && cgo

package measurements

// #cgo LDFLAGS: -ldl
// #include <stdlib.h>
// #include "nvml_linux.h"
import "C"

import "unsafe"

func gpuMemoryFacts(uuid string) (uint32, uint32, bool) {
	cUUID := C.CString(uuid)
	defer C.free(unsafe.Pointer(cUUID))

	var busWidth C.uint
	var clockMHz C.uint
	if C.cedana_nvml_memory_facts(cUUID, &busWidth, &clockMHz) != 0 {
		return 0, 0, false
	}
	return uint32(busWidth), uint32(clockMHz), true
}
