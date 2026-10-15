package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func CreateContainerForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		// Read runtime from dump

		runtime := defaultRuntime(ctx)

		file, err := opts.DumpFs.Open(podman_keys.DUMP_RUNTIME_KEY)
		if err != nil {
			log.Warn().Err(err).Msgf("could not open runtime file from dump, will use %s", runtime)
		} else {
			defer file.Close()
			bytes, err := io.ReadAll(file)
			if err != nil {
				log.Warn().Err(err).Msgf("could not read runtime from dump, will use %s", runtime)
			} else {
				log.Debug().Str("runtime", string(bytes)).Msg("read runtime from dump")
				runtime = string(bytes)
			}
		}

		createOpts, err := newCreateOpts(opts, req.GetDetails().GetPodman(), runtime)
		if err != nil {
			return nil, err
		}

		req.Criu.InheritFd = nil // NOTE: Ignore previously set values as they will be added again by the inner restore

		criuOptsJson, err := json.Marshal(req.Criu)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to marshal CRIU options: %v", err)
		}

		createOpts.Annotate(podman_keys.CHECKPOINT_PATH_ANNOTATION, req.Path) // For cedana-oci-runtime to know this is a restore and not a run
		createOpts.Annotate(podman_keys.CRIU_OPTS_ANNOTATION, base64.StdEncoding.EncodeToString(criuOptsJson))

		ctx = context.WithValue(ctx, podman_keys.CREATE_OPTS_CONTEXT_KEY, createOpts)

		return next(ctx, opts, resp, req)
	}
}
