package defaults

import (
	"context"
	"io"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/podman"
	"github.com/cedana/cedana/pkg/types"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
	"github.com/rs/zerolog/log"
)

func FillMissingRestoreDefaults(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetPodman() == nil {
			req.Details.Podman = &podman.Podman{}
		}
		if req.GetDetails().GetPodman().GetID() == "" {
			req.Details.Podman.ID = req.GetDetails().GetJID()
		}
		if req.GetDetails().GetPodman().GetImage() == "" {
			file, err := opts.DumpFs.Open(podman_keys.DUMP_IMAGE_NAME_KEY)
			if err != nil {
				log.Warn().Err(err).Msg("could not open image name file from dump")
				return next(ctx, opts, resp, req)
			}
			defer file.Close()
			name, err := io.ReadAll(file)
			if err != nil {
				log.Warn().Err(err).Msg("could not read image name from dump")
				return next(ctx, opts, resp, req)
			}
			req.Details.Podman.Image = string(name)
			log.Debug().Str("image", string(name)).Msg("using image name from dump")
		}

		return next(ctx, opts, resp, req)
	}
}
