package process

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	cedanagosdk "github.com/cedana/cedana-go-sdk"
	cedanagosdk_models "github.com/cedana/cedana-go-sdk/models"
)

const STATE_FILE = "process_state.json"

// Sets the PID from the request to the process state
// if not already set before.
func SetPIDForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		if resp.GetState() == nil {
			resp.State = &daemon.ProcessState{}
		}

		if resp.GetState().GetPID() == 0 {
			pid := req.GetDetails().GetProcess().GetPID()
			if pid == 0 {
				return nil, status.Errorf(codes.InvalidArgument, "missing PID")
			}
			resp.State.PID = pid
		}

		return next(ctx, opts, resp, req)
	}
}

// Fills process state in the dump response.
// Requires at least the PID to be present in the DumpResp.State
// Also saves the state to a file in the dump directory, post dump.
func FillProcessStateForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		state := resp.GetState()
		if state == nil {
			return nil, status.Errorf(
				codes.InvalidArgument,
				"missing state. at least PID is required in resp.state",
			)
		}

		if state.PID == 0 {
			return nil, status.Errorf(
				codes.NotFound,
				"missing PID. Ensure an adapter sets this PID in response.",
			)
		}

		err = utils.FillProcessState(ctx, state.PID, state)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to fill process state: %v", err)
		}

		exited, err = next(ctx, opts, resp, req)
		if err != nil {
			return exited, err
		}

		// Post dump, save the state to a file in the dump
		if err := utils.SaveJSONToFile(state, STATE_FILE, opts.DumpFs); err != nil {
			log.Warn().Err(err).Str("file", STATE_FILE).Msg("failed to save process state")
		}

		return exited, nil
	}
}

// Detect and sets shell job option for CRIU
func DetectShellJobForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		var isShellJob bool
		if state := resp.GetState(); state != nil {
			if state.SID != state.PID {
				isShellJob = true
			}
		} else {
			log.Warn().Msg("No process info found. it should have been filled by an adapter")
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		req.Criu.ShellJob = proto.Bool(isShellJob)

		return next(ctx, opts, resp, req)
	}
}

// Detects if the process is using IOUring and sets appropriate options for CRIU
// XXX: Currently IO uring C/R is not supported by CRIU, so we return an error.
// ref: https://criu.org/Google_Summer_of_Code_Ideas#IOUring_support
func DetectIOUringForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		state := resp.GetState()
		if state == nil {
			log.Warn().Msg("no process info found. it should have been filled by an adapter")
			return next(ctx, opts, resp, req)
		}

		for _, f := range state.GetOpenFiles() {
			if strings.Contains(f.Path, "io_uring") {
				return nil, status.Errorf(codes.Unimplemented, "IOUring dump is not supported at the moment")
			}
		}

		return next(ctx, opts, resp, req)
	}
}

// Close common file descriptors b/w the parent and child process
func CloseCommonFilesForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		pid := resp.GetState().GetPID()
		if pid == 0 {
			return nil, status.Errorf(
				codes.NotFound,
				"missing PID. Ensure an adapter sets this PID in response before.",
			)
		}

		err = utils.CloseCommonFds(ctx, int32(os.Getpid()), int32(pid))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to close common fds: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

// Detects any open files that are in an external namespace
// and sets appropriate options for CRIU.
// Also detects any TTY files and sets options for CRIU.
func AddExternalFilesForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		state := resp.GetState()
		if state == nil {
			log.Warn().Msg("no process info found. it should have been filled by an adapter")
			return next(ctx, opts, resp, req)
		}

		files := state.GetOpenFiles()
		mounts := state.GetMounts()

		mountIds := make(map[uint64]any)
		for _, m := range mounts {
			mountIds[m.ID] = nil
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		for _, f := range files {
			isPipe := strings.HasPrefix(f.Path, "pipe")
			isSocket := strings.HasPrefix(f.Path, "socket")
			isAnon := strings.HasPrefix(f.Path, "anon_inode")
			_, internal := mountIds[f.MountID]

			external := !(internal || isPipe || isSocket || isAnon) // sockets and pipes are always in external mounts

			if external {
				req.Criu.External = append(req.Criu.External, fmt.Sprintf("file[%x:%x]", f.MountID, f.Inode))
				continue
			}

			if f.IsTTY {
				req.Criu.External = append(req.Criu.External, fmt.Sprintf("tty[%x:%x]", f.Rdev, f.Dev))
			}
		}

		return next(ctx, opts, resp, req)
	}
}

// UploadCheckpoint Middleware
func UploadCheckpoint(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		res, err := next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
		}

		uuid := ctx.Value(keys.REMOTE_CHECKPOINT_KEY).(string)
		resp.Id = uuid

		go func() {
			cedanaUrl := config.Global.Connection.URL
			cedanaAuthToken := config.Global.Connection.AuthToken
			// Creating the client is no extra compute/work as this is not a durable connection
			client := cedanagosdk.NewCedanaClient(cedanaUrl, cedanaAuthToken)

			// ensure ctx cancel doesn't interrupt the upload
			ctx := context.WithoutCancel(ctx)
			// write information about process and checkpoint meta first before starting upload
			gpu := "none"
			platform := "none"
			if resp.State.GPUEnabled {
				gpu = "nvidia"
			}
			if resp.State.Host.Platform != "" {
				platform = resp.State.Host.Platform
			}
			body := cedanagosdk_models.CheckpointInfo{}
			body.SetGpu(&gpu)
			body.SetPlatform(&platform)
			_, err := client.V2().Checkpoints().Info().ById(uuid).Put(ctx, &body, nil)
			if err != nil {
				log.Warn().Msgf("put failed: checkpoint info to remote: %v", err)
				return
			}
			// get upload url to upload the dumped artifacts to
			url, err := client.V2().Checkpoints().Upload().ById(*uuid).Patch(ctx, nil)
			if err != nil {
				log.Warn().Msg("we can't get upload url from remote")
				return
			}
			// start uploading the file to url
			fs, err := os.Open(resp.Path)
			if err != nil {
				log.Warn().Msg("we can't open resp.Path")
				return
			}
			// TODO (SA): handle the multipart upload properly, make it resumable so that we can retry in case of transient network errors
			req, err := http.NewRequest("PUT", *url, fs)
			if err != nil {
				log.Warn().Msg("we can't build a request from checkpoint file")
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Warn().Msg("we can't get upload url from remote")
				return
			}
			if resp.StatusCode == 200 {
				log.Info().Msgf("successfully uploaded checkpoint: %v", *uuid)
				for {
					_, err := client.V2().Checkpoints().Uploaded().ById(*uuid).Post(ctx, nil)
					if err != nil {
						time.Sleep(time.Second * 10)
						continue
					}
					break
				}
			}
		}()
		// UploadCheckpoint after the checkpoints are completed
		return res, err
	}
}
