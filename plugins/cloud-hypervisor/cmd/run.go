package cmd

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/vm"
	"github.com/cedana/cedana/pkg/keys"
	clh_flags "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/flags"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

func init() {
	RunCmd.Flags().StringP(clh_flags.HypervisorConfigFlag.Full, clh_flags.HypervisorConfigFlag.Short, "", "config")
}

var RunCmd = &cobra.Command{
	Use:   "clh [optional-id]",
	Short: "run a clh VM",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid run request in context")
		}
		configPath, _ := cmd.Flags().GetString(clh_flags.HypervisorConfigFlag.Full)
		configFile, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read config file: %v", err)
		}

		configPb := &structpb.Struct{}
		err = protojson.Unmarshal(configFile, configPb)

		req.Type = "cloud-hypervisor"

		req.Details = &daemon.Details{Vm: &vm.Vm{
			HypervisorConfig: configPb,
		}}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}
