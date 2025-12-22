package utils

import (
	"os"
	"os/exec"
	"os/user"
	"strings"
	"syscall"
)

func GetUser() (*user.User, error) {
	var username string

	path, err := exec.LookPath("whoami")
	if err == nil {
		cmd := exec.Command(path)
		output, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		username = strings.TrimSpace(string(output))
	} else {
		username = os.Getenv("USER")
	}

	return user.Lookup(username)
}

func GetCredentials() (*syscall.Credential, error) {
	uid := os.Getuid()
	gid := os.Getgid()
	groups, err := os.Getgroups()
	if err != nil {
		return nil, err
	}
	return &syscall.Credential{
		Uid:    uint32(uid),
		Gid:    uint32(gid),
		Groups: IntToUint32Slice(groups),
	}, nil
}

func GetRootCredentials() *syscall.Credential {
	return &syscall.Credential{
		Uid:    0,
		Gid:    0,
		Groups: []uint32{0},
	}
}

func IsRootUser() bool {
	return os.Getuid() == 0
}
