package caps

import (
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPermittedHave(t *testing.T) {
	const status = "Name:\tcedana\nCapInh:\t0000000000000000\nCapPrm:\t0000000000080000\nCapEff:\t0000000000080000\n"
	if !PermittedHave(strings.NewReader(status), unix.CAP_SYS_PTRACE) {
		t.Error("expected CAP_SYS_PTRACE (bit 19) to be found in CapPrm 0x80000")
	}
	if PermittedHave(strings.NewReader(status), unix.CAP_SYS_ADMIN) {
		t.Error("expected CAP_SYS_ADMIN not to be found")
	}
	if PermittedHave(strings.NewReader("Name:\tcedana\n"), unix.CAP_SYS_PTRACE) {
		t.Error("expected no CapPrm line to mean no")
	}
	if PermittedHave(strings.NewReader("CapPrm:\tnot-hex\n"), unix.CAP_SYS_PTRACE) {
		t.Error("expected an unreadable CapPrm to mean no")
	}
}

func TestHasPermittedReadsOurOwn(t *testing.T) {
	// Root has them all; anyone else may or may not. Only that it doesn't blow up.
	_ = HasPermitted(unix.CAP_SYS_PTRACE)
}
