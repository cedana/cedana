package cmd

import (
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/style"
	runc_flags "github.com/cedana/cedana/plugins/runc/pkg/flags"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func init() {
	QueryCmd.Flags().StringP(runc_flags.RootFlag.Full, runc_flags.RootFlag.Short, "", "root")
	QueryCmd.Flags().StringSliceP(runc_flags.IdFlag.Full, runc_flags.IdFlag.Short, nil, "container id(s)")
}

var QueryCmd = &cobra.Command{
	Use:   "runc",
	Short: "Query runc containers. Can provide multiple IDs or names.",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		root, _ := cmd.Flags().GetString(runc_flags.RootFlag.Full)
		ids, _ := cmd.Flags().GetStringSlice(runc_flags.IdFlag.Full)

		req := &daemon.QueryReq{
			Type: "runc",
			Runc: &runc.QueryReq{
				Root: root,
				IDs:  ids,
			},
		}

		resp, err := client.Query(cmd.Context(), req)
		if err != nil {
			return err
		}

		result := resp.Runc

		if len(result.Containers) == 0 {
			fmt.Println("No containers found")
		}

		tableWriter := table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)

		tableWriter.AppendHeader(table.Row{
			"ID",
			"Bundle",
			"Root",
		})

		tableWriter.SortBy([]table.SortBy{
			{Name: "Root", Mode: table.Asc},
		})

		for _, container := range result.Containers {
			tableWriter.AppendRow(table.Row{
				container.ID,
				container.Bundle,
				container.Root,
			})
		}

		tableWriter.Render()

		if len(resp.Messages) > 0 {
			fmt.Println()
		}
		for _, msg := range resp.Messages {
			fmt.Println(msg)
		}

		return nil
	},
}
