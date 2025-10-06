package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/gen/plugins/slurm"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/spf13/cobra"
)

var DumpCmd = &cobra.Command{
	Use:   "slurm <job-id>",
	Short: "Dump a slurm job",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.DUMP_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid dump request in context")
		}

		var id string
		if len(args) > 0 {
			id = args[0]
		}

		req.Type = "slurm"
		req.Details = &daemon.Details{Slurm: &slurm.Slurm{
			ID: id,
		}}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		if err := dump(id); err != nil {
			return fmt.Errorf("failed to dump slurm job: %v", err)
		}

		return nil
	},
}

func dump(jobid string) error {
	cedanaURL := os.Getenv("CEDANA_URL")
	if cedanaURL != "" {
		return fmt.Errorf("CEDANA_URL environment variable is not set")
	}

	authToken := os.Getenv("CEDANA_AUTH_TOKEN")
	if authToken == "" {
		return fmt.Errorf("CEDANA_AUTH_TOKEN environment variable is not set")
	}

	data, err := json.Marshal(map[string]interface{}{
		"job_id": jobid,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request data: %v", err)
	}

	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/v2/slurm/checkpoint/job", cedanaURL),
		bytes.NewBuffer(data),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authToken))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response status: %s", resp.Status)
	}

	return nil
}
