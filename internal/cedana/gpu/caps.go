package gpu

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// hasPermittedCap tells whether we hold the capability in our permitted set,
// from which a child can be given it. Taken for no on anything unexpected.
func hasPermittedCap(c int) bool {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return false
	}
	defer f.Close()
	return permittedCapsHave(f, c)
}

// permittedCapsHave reads the CapPrm line of a /proc/<pid>/status
func permittedCapsHave(status interface{ Read([]byte) (int, error) }, c int) bool {
	scanner := bufio.NewScanner(status)
	for scanner.Scan() {
		value, ok := strings.CutPrefix(scanner.Text(), "CapPrm:")
		if !ok {
			continue
		}
		mask, err := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
		if err != nil || c < 0 || c >= 64 {
			return false
		}
		return mask&(1<<uint(c)) != 0
	}
	return false
}
