package cmd

// Hidden CLI option to generate docs in docs/cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

const DOCS_DIR_CLI = "docs/references/cli"

// Index page of the CLI reference, as the generated pages have none
const DOCS_CLI_README = `# CLI

Reference for the ` + "`cedana`" + ` CLI, generated from its help text. Start with [cedana](cedana.md).
`

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

		if err := os.WriteFile(filepath.Join(DOCS_DIR_CLI, "README.md"), []byte(DOCS_CLI_README), 0644); err != nil {
			return err
		}

		links := aliasedSubcommandLinks(rootCmd)
		linkHandler := func(file string) string {
			if target, ok := links[file]; ok {
				return target
			}
			return file
		}

		return doc.GenMarkdownTreeCustom(rootCmd, DOCS_DIR_CLI, func(string) string { return "" }, linkHandler)
	},
}

// Subcommands of an alias (see utils.AliasOf) are shared with the aliased command, so their
// pages are generated under the aliased command's path. Maps the page file the alias links to,
// to the page that is actually generated.
func aliasedSubcommandLinks(cmd *cobra.Command) map[string]string {
	links := map[string]string{}
	for _, child := range cmd.Commands() {
		if child.Parent() != cmd {
			links[docFileName(cmd.CommandPath()+" "+child.Name())] = docFileName(child.CommandPath())
		}
		for k, v := range aliasedSubcommandLinks(child) {
			links[k] = v
		}
	}
	return links
}

func docFileName(commandPath string) string {
	return strings.ReplaceAll(commandPath, " ", "_") + ".md"
}
