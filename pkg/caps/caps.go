// Package caps reads the capabilities a process holds, from /proc.
package caps

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

// HasPermitted tells whether we hold the capability in our permitted set, from which
// a child can be given it. Taken for no on anything unexpected.
func HasPermitted(c int) bool {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return false
	}
	defer f.Close()
	return PermittedHave(f, c)
}

// PermittedHave reads the CapPrm line of a /proc/<pid>/status, and tells whether the
// capability is in it.
func PermittedHave(status io.Reader, c int) bool {
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
