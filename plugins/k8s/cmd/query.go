package cmd

import "github.com/spf13/cobra"

func init() {}

var QueryCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Query k8s containers",
	Args:  cobra.ArbitraryArgs,
}
