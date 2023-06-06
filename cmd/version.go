package cmd

import "fmt"

func SetVersionInfo(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (%s)", version, commit)
}
