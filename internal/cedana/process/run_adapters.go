package process

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
)

// Adapter that writes PID to a file after the next handler is called.
func WritePIDFile(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		code, err = next(ctx, opts, resp, req)
		if err != nil {
			return code, err
		}

		pidFile := req.PidFile
		if pidFile == "" {
			return code, err
		}

		file, err := os.Create(pidFile)
		if err != nil {
			log.Warn().Err(err).Msgf("Failed to create PID file %s", pidFile)
			resp.Messages = append(resp.Messages, fmt.Sprintf("Failed to create PID file %s: %s", pidFile, err.Error()))
		}

		_, err = fmt.Fprintf(file, "%d", resp.PID)
		if err != nil {
			log.Warn().Err(err).Msgf("Failed to write PID to file %s", pidFile)
			resp.Messages = append(resp.Messages, fmt.Sprintf("Failed to write PID to file %s: %s", pidFile, err.Error()))
		}

		log.Debug().Msgf("Wrote PID %d to file %s", resp.PID, pidFile)

		// Do not fail the request if we cannot write the PID file

		return code, nil
	}
}
