package cmd

// Hidden CLI option to generate docs in docs/cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

const DOCS_DIR_CLI = "docs/cli"

var docGenCmd = &cobra.Command{
	Use:    "docs-gen",
	Hidden: true,
	Short:  "Generate markdown documentation for the CLI",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.RemoveAll(DOCS_DIR_CLI); err != nil {
			return err
		}

		if err := os.MkdirAll(DOCS_DIR_CLI, 0755); err != nil {
			return err
		}

		return doc.GenMarkdownTree(rootCmd, DOCS_DIR_CLI)
	},
}
