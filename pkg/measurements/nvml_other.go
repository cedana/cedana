//go:build !linux || !cgo

package measurements

func gpuMemoryFacts(string) (uint32, uint32, bool) {
	return 0, 0, false
}
