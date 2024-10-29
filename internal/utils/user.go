package utils

import (
	"os"
	"os/user"
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

func IsRootUser() bool {
	return os.Getuid() == 0
}
