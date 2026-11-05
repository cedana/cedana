package process

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestExitCodeOf(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"exit 0", []string{"-c", "exit 0"}, 0},
		{"exit 3", []string{"-c", "exit 3"}, 3},
		{"killed by SIGUSR2", []string{"-c", "kill -USR2 $$"}, 128 + int(syscall.SIGUSR2)},
		{"killed by SIGKILL", []string{"-c", "kill -KILL $$"}, 128 + int(syscall.SIGKILL)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command("/bin/sh", c.args...)
			_ = cmd.Run()
			if got := exitCodeOf(cmd.ProcessState); got != c.want {
				t.Errorf("exitCodeOf = %d, want %d", got, c.want)
			}
		})
	}
}
