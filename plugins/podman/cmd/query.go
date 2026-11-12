package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/podman"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/style"
	podman_utils "github.com/cedana/cedana/plugins/podman/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

var QueryCmd = &cobra.Command{
	Use:   "podman <container1> [<container2> ...]",
	Short: "Query podman containers",
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

		req.Type = "podman"
		req.Podman = &podman.QueryReq{IDs: ids}

		resp, err := client.Query(ctx, req)
		if err != nil {
			return err
		}

		result := resp.Podman
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
			"Image",
			"Runtime",
		})

		for _, container := range result.Containers {
			tableWriter.AppendRow(table.Row{
				container.ID,
				container.Image,
				podman_utils.Runtime(container),
			})
		}

		output = tableWriter.Render()

		return nil
	},
}
