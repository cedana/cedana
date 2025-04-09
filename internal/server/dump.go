package server

import (
	"context"
	"net/http"
	"os"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana-go-sdk/models"
	"github.com/cedana/cedana/internal/server/criu"
	"github.com/cedana/cedana/internal/server/defaults"
	"github.com/cedana/cedana/internal/server/filesystem"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/internal/server/network"
	"github.com/cedana/cedana/internal/server/process"
	"github.com/cedana/cedana/internal/server/streamer"
	"github.com/cedana/cedana/internal/server/validation"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	cedanagosdk "github.com/cedana/cedana-go-sdk"
)

func (s *Server) Dump(ctx context.Context, req *daemon.DumpReq) (*daemon.DumpResp, error) {
	// The order below is the order followed before executing
	// the final handler (criu.Dump).

	dumpDirAdapter := filesystem.PrepareDumpDir
	if req.Stream > 0 || config.Global.Checkpoint.Stream > 0 {
		dumpDirAdapter = streamer.PrepareDumpDir
	}

	middleware := types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,
		dumpDirAdapter,

		pluginDumpMiddleware, // middleware from plugins

		// Process state-dependent adapters
		process.FillProcessStateForDump,
		process.DetectShellJobForDump,
		process.DetectIOUringForDump,
		process.AddExternalFilesForDump,
		process.CloseCommonFilesForDump,
		network.DetectNetworkOptionsForDump,

		criu.CheckOptsForDump,
	}

	dump := criu.Dump.With(middleware...)

	if req.GetDetails().GetJID() != "" { // If using job dump
		dump = dump.With(job.ManageDump(s.jobs))
	}

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.wg,
	}
	resp := &daemon.DumpResp{}

	criu := criu.New[daemon.DumpReq, daemon.DumpResp](s.plugins)

	_, err := criu(dump)(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	if utils.PathExists(resp.Path) {
		log.Info().Str("path", resp.Path).Str("type", req.Type).Msg("dump successful")
		resp.Messages = append(resp.Messages, "Dumped to "+resp.Path)

		client := cedanagosdk.NewCedanaClient(strings.ReplaceAll(os.Getenv("CEDANA_URL"), "/v1", ""), os.Getenv("CEDANA_AUTH_TOKEN"))
		uuid, err := client.V2().Checkpoints().Post(ctx, nil)
		if err != nil {
			// we can't write checkpoint to remote
			return resp, nil
		}
		go func() {
			// write information about process and checkpoint meta first before starting upload
			gpu := "none"
			platform := "none"
			if resp.State.GPUEnabled {
				gpu = "nvidia"
			}
			if resp.State.Host.Platform != "" {
				platform = resp.State.Host.Platform
			}
			body := models.CheckpointInfo{}
			body.SetGpu(&gpu)
			body.SetPlatform(&platform)
			_, err := client.V2().Checkpoints().Info().ById(*uuid).Put(context.WithoutCancel(ctx), &body, nil)
			if err != nil {
				return
			}
			// get upload url to upload the dumped artifacts to
			url, err := client.V2().Checkpoints().Upload().ById(*uuid).Patch(context.WithoutCancel(ctx), nil)
			if err != nil {
				return
			}
			// start uploading the file to url
			fs, err := os.Open(resp.Path)
			if err != nil {
				return
			}
			// TODO (SA): handle the multipart upload properly, make it resumable so that we can retry in case of transient network errors
			req, err := http.NewRequest("PUT", *url, fs)
			if err != nil {
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			if resp.StatusCode == 200 {
				return
			}
		}()
	}

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters after itself based on the type of dump.
func pluginDumpMiddleware(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		middleware := types.Middleware[types.Dump]{}
		t := req.GetType()
		switch t {
		case "process":
			middleware = append(middleware, process.SetPIDForDump)
		default:
			// Insert plugin-specific middleware
			err = features.DumpMiddleware.IfAvailable(func(
				name string,
				pluginMiddleware types.Middleware[types.Dump],
			) error {
				middleware = append(middleware, pluginMiddleware...)
				return nil
			}, t)
			if err != nil {
				return nil, status.Error(codes.Unimplemented, err.Error())
			}
		}
		return next.With(middleware...)(ctx, opts, resp, req)
	}
}
