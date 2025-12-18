package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/style"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	queryCmd.AddCommand(processQueryCmd)

	queryCmd.PersistentFlags().BoolP(flags.TreeFlag.Full, flags.TreeFlag.Short, false, "include entire process tree")
	queryCmd.PersistentFlags().BoolP(flags.InspectFlag.Full, flags.InspectFlag.Short, false, "view details of first result")

	///////////////////////////////////////////
	// Add subcommands from supported plugins
	///////////////////////////////////////////

	features.QueryCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			queryCmd.AddCommand(pluginCmd)
			return nil
		},
	)
}

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query containers/processes",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		client, err := client.New(config.Global.Address, config.Global.Protocol)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		tree, _ := cmd.Flags().GetBool(flags.TreeFlag.Full)

		req := &daemon.QueryReq{
			Tree: tree,
		}

		ctx = context.WithValue(ctx, keys.QUERY_REQ_CONTEXT_KEY, req)
		ctx = context.WithValue(ctx, keys.CLIENT_CONTEXT_KEY, client)
		cmd.SetContext(ctx)

		return nil
	},

	//******************************************************************************************
	// Let subcommands (incl. from plugins) add details to the request, in the `RunE` hook.
	// And also, call make the request to the server, allowing the plugin to handle it and
	// print the information as it likes.
	//******************************************************************************************

	PersistentPostRunE: func(cmd *cobra.Command, args []string) (err error) {
		ctx := cmd.Context()
		client, ok := ctx.Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}
		defer client.Close()

		inspect, _ := cmd.Flags().GetBool(flags.InspectFlag.Full)

		resp, ok := ctx.Value(keys.QUERY_RESP_CONTEXT_KEY).(*daemon.QueryResp)
		if !ok {
			return fmt.Errorf("invalid query response in context")
		}
		output, ok := ctx.Value(keys.QUERY_OUTPUT_CONTEXT_KEY).(string)
		if !ok {
			return fmt.Errorf("invalid query output in context")
		}

		if len(resp.States) == 0 {
			fmt.Println("No results found")
		} else {
			fmt.Println(output)
			if inspect {
				bytes, err := yaml.Marshal(resp.States[0])
				if err != nil {
					return fmt.Errorf("Error marshalling job: %v", err)
				}
				fmt.Println()
				fmt.Print(string(bytes))
			}
		}

		if len(resp.Messages) > 0 {
			fmt.Println()
			for _, msg := range resp.Messages {
				fmt.Println(style.DisabledColors.Sprint(msg))
			}
		}

		if len(resp.States) > 0 && !inspect {
			fmt.Println()
			fmt.Printf("Use `%s --%s` for details on the first result\n", cmd.CalledAs(), flags.InspectFlag.Full)
		}

		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var processQueryCmd = &cobra.Command{
	Use:   "process <PID1> [<PID2> ...]",
	Short: "Query a process",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		client, ok := ctx.Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}
		req, ok := ctx.Value(keys.QUERY_REQ_CONTEXT_KEY).(*daemon.QueryReq)
		if !ok {
			return fmt.Errorf("invalid query request in context")
		}

		var pids []uint32
		for _, arg := range args {
			var pid uint32
			_, err := fmt.Sscanf(arg, "%d", &pid)
			if err != nil {
				return fmt.Errorf("invalid PID: %s", arg)
			}
			pids = append(pids, pid)
		}

		req.Type = "process"
		req.PIDs = pids

		resp, err := client.Query(ctx, req)
		if err != nil {
			return err
		}

		result := resp.States
		var output string

		defer func() {
			ctx = context.WithValue(ctx, keys.QUERY_RESP_CONTEXT_KEY, resp)
			ctx = context.WithValue(ctx, keys.QUERY_OUTPUT_CONTEXT_KEY, output)
			cmd.SetContext(ctx)
		}()

		if len(result) == 0 {
			return nil
		}

		tableWriter := table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)

		tableWriter.AppendHeader(table.Row{
			"PID",
			"Status",
			"UID",
			"GID",
			"Command",
		})
		tableWriter.SortBy([]table.SortBy{
			{Name: "Status", Mode: table.Asc},
		})

		for _, proc := range result {
			var uid, gid uint32
			if len(proc.UIDs) > 0 {
				uid = proc.UIDs[0]
			}
			if len(proc.GIDs) > 0 {
				gid = proc.GIDs[0]
			}
			tableWriter.AppendRow(table.Row{
				proc.PID,
				proc.Status,
				uid,
				gid,
				proc.Cmdline,
			})
		}

		output = tableWriter.Render()

		return nil
	},
}
