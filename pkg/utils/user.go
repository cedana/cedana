package utils

import (
	"os"
	"os/user"
	"syscall"
)

func GetUser() (*user.User, error) {
	username := os.Getenv("SUDO_USER")
	if username == "" {
		// fetch the current user
		// it uses getpwuid_r iirc
		u, err := user.Current()
		if err == nil {
			return u, nil
		}
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
