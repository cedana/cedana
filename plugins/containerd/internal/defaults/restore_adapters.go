package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/rs/zerolog/log"
)

func FillMissingRestoreDefaults(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetContainerd() == nil {
			req.Details.Containerd = &containerd.Containerd{}
		}
		if req.GetDetails().GetContainerd().GetAddress() == "" {
			req.Details.Containerd.Address = DEFAULT_ADDRESS
		}
		if req.GetDetails().GetContainerd().GetNamespace() == "" {
			req.Details.Containerd.Namespace = DEFAULT_NAMESPACE
		}
		if req.GetDetails().GetContainerd().GetID() == "" {
			req.Details.Containerd.ID = req.GetDetails().GetJID()
		}
		if req.GetDetails().GetContainerd().GetImage().GetName() == "" {
			file, err := opts.DumpFs.Open(containerd_keys.DUMP_IMAGE_NAME_KEY)
			if err != nil {
				log.Warn().Err(err).Msg("could not open image name file from dump")
				return next(ctx, opts, resp, req)
			}
			defer file.Close()
			var imageName [256]byte
			n, err := file.Read(imageName[:])
			if err != nil {
				log.Warn().Err(err).Msg("could not read image name from dump")
				return next(ctx, opts, resp, req)
			}
			name := string(imageName[:n])
			req.Details.Containerd.Image = &containerd.Image{
				Name: name,
			}
			log.Debug().Str("image", name).Msg("using image name from dump")
		}

		return next(ctx, opts, resp, req)
	}
}
