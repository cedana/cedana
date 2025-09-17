package cmd

import (
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/style"
	containerd_flags "github.com/cedana/cedana/plugins/containerd/pkg/flags"
	k8s_flags "github.com/cedana/cedana/plugins/k8s/pkg/flags"
	runc_flags "github.com/cedana/cedana/plugins/runc/pkg/flags"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func init() {
	QueryCmd.Flags().StringP(runc_flags.RootFlag.Full, runc_flags.RootFlag.Short, "", "root")
	QueryCmd.Flags().StringP(containerd_flags.NamespaceFlag.Full, containerd_flags.NamespaceFlag.Short, "", "pod namespace")
	QueryCmd.Flags().StringSliceP(k8s_flags.NameFlag.Full, k8s_flags.NameFlag.Short, nil, "pod name(s)")
	QueryCmd.Flags().StringP(k8s_flags.ContainerTypeFlag.Full, k8s_flags.ContainerTypeFlag.Short, "", "container type (container, sandbox)")
}

var QueryCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Query k8s containers",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		root, _ := cmd.Flags().GetString(runc_flags.RootFlag.Full)
		namespace, _ := cmd.Flags().GetString(containerd_flags.NamespaceFlag.Full)
		names, _ := cmd.Flags().GetStringSlice(k8s_flags.NameFlag.Full)
		containerType, _ := cmd.Flags().GetString(k8s_flags.ContainerTypeFlag.Full)

		req := &daemon.QueryReq{
			Type: "k8s",
			K8S: &k8s.QueryReq{
				Root:          root,
				Namespace:     namespace,
				Names:         names,
				ContainerType: containerType,
			},
		}

		resp, err := client.Query(cmd.Context(), req)
		if err != nil {
			return err
		}

		result := resp.K8S

		if len(result.Pods) == 0 {
			fmt.Println("No pods found")
			return nil
		}

		tableWriter := table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)

		tableWriter.AppendHeader(table.Row{
			"Sandbox ID",
			"Name",
			"Namespace",
			"Containers",
			"Container Type",
		})

		tableWriter.SortBy([]table.SortBy{
			{Name: "ID", Mode: table.Asc},
		})

		for _, pod := range result.Pods {
			for i, container := range pod.Containerd {
				if i == 0 {
					tableWriter.AppendRow(table.Row{
						pod.ID,
						pod.Name,
						pod.Namespace,
						container.ID,
					})
				} else {
					tableWriter.AppendRow(table.Row{
						"",
						"",
						"",
						container.ID,
					})
				}
			}
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
