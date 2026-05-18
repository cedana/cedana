package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/style"
	containerd_flags "github.com/cedana/cedana/plugins/containerd/pkg/flags"
	containerd_utils "github.com/cedana/cedana/plugins/containerd/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func init() {
	QueryCmd.Flags().StringP(containerd_flags.AddressFlag.Full, containerd_flags.AddressFlag.Short, "", "containerd socket address")
	QueryCmd.Flags().StringP(containerd_flags.NamespaceFlag.Full, containerd_flags.NamespaceFlag.Short, "", "containerd namespace")
}

var QueryCmd = &cobra.Command{
	Use:   "containerd <ID1> [<ID2> ...]",
	Short: "Query containerd containers",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, ids []string) error {
		ctx := cmd.Context()
		client, ok := ctx.Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}
		req, ok := ctx.Value(keys.QUERY_REQ_CONTEXT_KEY).(*daemon.QueryReq)
		if !ok {
			return fmt.Errorf("invalid query request in context")
		}

		address, _ := cmd.Flags().GetString(containerd_flags.AddressFlag.Full)
		namespace, _ := cmd.Flags().GetString(containerd_flags.NamespaceFlag.Full)

		req.Type = "containerd"
		req.Containerd = &containerd.QueryReq{
			IDs:       ids,
			Address:   address,
			Namespace: namespace,
		}

		resp, err := client.Query(ctx, req)
		if err != nil {
			return err
		}

		result := resp.Containerd
		var output string

		defer func() {
			ctx = context.WithValue(ctx, keys.QUERY_RESP_CONTEXT_KEY, resp)
			ctx = context.WithValue(ctx, keys.QUERY_OUTPUT_CONTEXT_KEY, output)
			cmd.SetContext(ctx)
		}()

		if len(result.Containers) == 0 {
			return nil
		}

		tableWriter := table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)

		tableWriter.AppendHeader(table.Row{
			"ID",
			"Namespace",
			"Image",
			"Runtime",
		})

		tableWriter.SortBy([]table.SortBy{
			{Name: "Root", Mode: table.Asc},
		})

		for _, container := range result.Containers {
			tableWriter.AppendRow(table.Row{
				container.ID,
				container.Namespace,
				container.GetImage().GetName(),
				containerd_utils.Runtime(container),
			})
		}

		output = tableWriter.Render()

		return nil
	},
}
