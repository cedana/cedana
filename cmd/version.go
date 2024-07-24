package cmd

import "fmt"

func GetVersion(version, commit, date string) string {
	return fmt.Sprintf("%s (%s)", version, commit)
}
