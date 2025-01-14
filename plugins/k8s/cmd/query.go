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
	QueryCmd.Flags().StringP(containerd_flags.NamespaceFlag.Full, containerd_flags.NamespaceFlag.Short, "", "namespace")
	QueryCmd.Flags().StringSliceP(k8s_flags.NameFlag.Full, k8s_flags.NameFlag.Short, nil, "container name (can be multiple)")
	QueryCmd.Flags().StringSliceP(k8s_flags.SandboxFlag.Full, k8s_flags.SandboxFlag.Short, nil, "sandbox name (can be multiple)")
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
		sandboxes, _ := cmd.Flags().GetStringSlice(k8s_flags.SandboxFlag.Full)

		req := &daemon.QueryReq{
			Type: "k8s",
			K8S: &k8s.QueryReq{
				Root:           root,
				Namespace:      namespace,
				ContainerNames: names,
				SandboxNames:   sandboxes,
			},
		}

		resp, err := client.Query(cmd.Context(), req)
		if err != nil {
			return err
		}

		result := resp.K8S

		if len(result.Containers) == 0 {
			fmt.Println("No containers found")
			return nil
		}

		tableWriter := table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)

		tableWriter.AppendHeader(table.Row{
			"Sandbox ID",
			"Sandbox Name",
			"Sandbox Namespace",
			"Sandbox UID",
			"Image",
			"Container ID",
			"Bundle",
			"Root",
		})

		tableWriter.SortBy([]table.SortBy{
			{Name: "Sandbox ID", Mode: table.Asc},
		})

		for _, container := range result.Containers {
			tableWriter.AppendRow(table.Row{
				container.SandboxID,
				container.SandboxName,
				container.SandboxNamespace,
				container.SandboxUID,
				container.Image,
				container.Runc.ID,
				container.Runc.Bundle,
				container.Runc.Root,
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
