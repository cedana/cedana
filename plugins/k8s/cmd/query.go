package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/style"
	containerd_flags "github.com/cedana/cedana/plugins/containerd/pkg/flags"
	k8s_flags "github.com/cedana/cedana/plugins/k8s/pkg/flags"
	"github.com/cedana/cedana/plugins/k8s/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func init() {
	QueryCmd.Flags().StringP(containerd_flags.NamespaceFlag.Full, containerd_flags.NamespaceFlag.Short, "", "pod namespace")
	QueryCmd.Flags().StringP(k8s_flags.ContainerTypeFlag.Full, k8s_flags.ContainerTypeFlag.Short, "", "container type (container, sandbox)")
}

var QueryCmd = &cobra.Command{
	Use:   "k8s <name1> [<name2> ...]",
	Short: "Query Kubernetes pods and containers",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, names []string) error {
		ctx := cmd.Context()
		client, ok := ctx.Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}
		req, ok := ctx.Value(keys.QUERY_REQ_CONTEXT_KEY).(*daemon.QueryReq)
		if !ok {
			return fmt.Errorf("invalid query request in context")
		}

		namespace, _ := cmd.Flags().GetString(containerd_flags.NamespaceFlag.Full)
		containerType, _ := cmd.Flags().GetString(k8s_flags.ContainerTypeFlag.Full)

		req.Type = "k8s"
		req.K8S = &k8s.QueryReq{
			Namespace:     namespace,
			Names:         names,
			ContainerType: containerType,
		}

		resp, err := client.Query(ctx, req)
		if err != nil {
			return err
		}

		result := resp.K8S
		var output string

		defer func() {
			ctx = context.WithValue(ctx, keys.QUERY_RESP_CONTEXT_KEY, resp)
			ctx = context.WithValue(ctx, keys.QUERY_OUTPUT_CONTEXT_KEY, output)
			cmd.SetContext(ctx)
		}()

		if len(result.Pods) == 0 {
			return nil
		}

		tableWriter := table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)

		tableWriter.AppendHeader(table.Row{
			"Sandbox ID",
			"Name",
			"Namespace",
			"Runtime",
			"Containers",
		})

		tableWriter.SetColumnConfigs([]table.ColumnConfig{
			{Name: "Sandbox ID", AutoMerge: true},
		})
		tableWriter.SortBy([]table.SortBy{
			{Name: "Name", Mode: table.Asc},
		})

		for _, pod := range result.Pods {
			for _, container := range pod.Containerd {
				tableWriter.AppendRow(table.Row{
					pod.ID,
					pod.Name,
					pod.Namespace,
					utils.Runtime(pod),
					container.ID,
				})
			}
		}

		output = tableWriter.Render()

		return nil
	},
}
