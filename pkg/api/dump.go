package api

// Internal functions used by service for dumping processes and containers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/api/services/gpu"
	"github.com/cedana/cedana/pkg/api/services/rpc"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/cedana/cedana/pkg/container"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/mdlayher/vsock"
)

const (
	CRIU_DUMP_LOG_FILE  = "cedana-dump.log"
	CRIU_DUMP_LOG_LEVEL = 4
	GHOST_LIMIT         = 10000000
	DUMP_FOLDER_PERMS   = 0o700

	K8S_RUNC_ROOT     = "/run/containerd/runc/k8s.io"
	DOCKER_RUNC_ROOT  = "/run/docker/runtime-runc/moby"
	DEFAULT_RUNC_ROOT = "/run/runc"
)

// The bundle includes path to bundle and the runc/podman container id of the bundle. The bundle is a folder that includes the oci spec config.json
// as well as the rootfs used for setting up the container. Sometimes rootfs can be defined elsewhere. Podman adds extra directories and files in their
// bundle including a file called attach which is a unix socket for attaching stdin, stdout to the terminal
type Bundle struct {
	ContainerID string
	Bundle      string
}

// prepareDump =/= preDump.
// prepareDump sets up the folders to dump into, and sets the criu options.
// preDump on the other hand does any process cleanup right before the checkpoint.
func (s *service) prepareDump(ctx context.Context, state *task.ProcessState, args *task.DumpArgs, opts *rpc.CriuOpts) (string, error) {
	stats, ok := ctx.Value(utils.DumpStatsKey).(*task.DumpStats)
	if !ok {
		return "", fmt.Errorf("could not get dump stats from context")
	}

	start := time.Now()

	var hasTCP bool
	var hasExtUnixSocket bool

	if state.ProcessInfo != nil {
		for _, Conn := range state.ProcessInfo.OpenConnections {
			if Conn.Type == syscall.SOCK_STREAM { // TCP
				hasTCP = true
			}

			if Conn.Type == syscall.AF_UNIX { // Interprocess
				hasExtUnixSocket = true
			}
		}
	}

	opts.TcpEstablished = proto.Bool(hasTCP || args.GetCriuOpts().GetTcpEstablished())
	opts.ExtUnixSk = proto.Bool(hasExtUnixSocket)
	opts.FileLocks = proto.Bool(true)
	opts.LeaveRunning = proto.Bool(args.GetCriuOpts().GetLeaveRunning() || viper.GetBool("client.leave_running"))

	// check tty state
	// if pts is in open fds, chances are it's a shell job
	var isShellJob bool
	if state.ProcessInfo != nil {
		for _, f := range state.ProcessInfo.OpenFds {
			if strings.Contains(f.Path, "pts") {
				isShellJob = true
				break
			}
		}
	}
	opts.ShellJob = proto.Bool(isShellJob)
	opts.Stream = proto.Bool(args.Stream)

	// jobID + UTC time (nanoseconds)
	// strip out non posix-compliant characters from the jobID
	var dumpDirPath string
	if args.Stream {
		dumpDirPath = args.Dir
	} else {
		timeString := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
		processDumpDir := strings.Join([]string{state.JID, timeString}, "_")
		dumpDirPath = filepath.Join(args.Dir, processDumpDir)
	}
	_, err := os.Stat(dumpDirPath)
	if err != nil {
		if err := os.MkdirAll(dumpDirPath, DUMP_FOLDER_PERMS); err != nil {
			return "", err
		}
	}

	err = os.Chown(args.Dir, int(state.UIDs[0]), int(state.GIDs[0]))
	if err != nil {
		return "", err
	}
	err = chownRecursive(dumpDirPath, state.UIDs[0], state.GIDs[0])
	if err != nil {
		return "", err
	}

	err = os.Chmod(args.Dir, DUMP_FOLDER_PERMS)
	if err != nil {
		return "", err
	}
	err = chmodRecursive(dumpDirPath, DUMP_FOLDER_PERMS)
	if err != nil {
		return "", err
	}

	// close common fds
	err = closeCommonFds(int32(os.Getpid()), state.PID)
	if err != nil {
		return "", err
	}
	elapsed := time.Since(start)
	stats.PrepareDuration = elapsed.Milliseconds()

	return dumpDirPath, nil
}

func (s *service) postDump(ctx context.Context, dumpdir string, state *task.ProcessState, streamCmd *exec.Cmd) error {
	start := time.Now()
	stats, ok := ctx.Value(utils.DumpStatsKey).(*task.DumpStats)
	if !ok {
		log.Error().Msg("could not get dump stats from context")
		return fmt.Errorf("could not get dump stats from context")
	}

	var compressedCheckpointPath string
	if streamCmd != nil {
		compressedCheckpointPath = filepath.Join(dumpdir, "img.lz4")
	} else {
		compressedCheckpointPath = strings.Join([]string{dumpdir, ".tar"}, "")
	}

	state.CheckpointPath = compressedCheckpointPath
	state.CheckpointState = task.CheckpointState_CHECKPOINTED

	// sneak in a serialized state obj
	err := serializeStateToDir(dumpdir, state, streamCmd != nil)
	if err != nil {
		log.Error().Err(err)
		return err
	}

	log.Info().Str("Path", compressedCheckpointPath).Msg("compressing checkpoint")

	if streamCmd != nil {
		streamCmd.Wait()
	} else {
		err = utils.TarFolder(dumpdir, compressedCheckpointPath)
		if err != nil {
			log.Error().Err(err)
			return nil
		}
	}

	// get size of compressed checkpoint
	info, err := os.Stat(compressedCheckpointPath)
	if err != nil {
		log.Error().Err(err)
		return err
	}

	elapsed := time.Since(start)
	stats.CheckpointFileStats = &task.CheckpointFileStats{
		Size:     info.Size(),
		Duration: elapsed.Milliseconds(),
	}

	return nil
}

func (s *service) prepareDumpOpts() *rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel:   proto.Int32(CRIU_DUMP_LOG_LEVEL),
		LogFile:    proto.String(CRIU_DUMP_LOG_FILE),
		GhostLimit: proto.Uint32(GHOST_LIMIT),
	}
	return &opts
}

func (s *service) runcDump(ctx context.Context, root, containerID string, opts *container.CriuOpts, state *task.ProcessState) error {
	start := time.Now()
	stats, ok := ctx.Value(utils.DumpStatsKey).(*task.DumpStats)
	if !ok {
		return fmt.Errorf("could not get dump stats from context")
	}

	if _, err := os.Stat(opts.ImagesDirectory); os.IsNotExist(err) {
		err := os.MkdirAll(opts.ImagesDirectory, DUMP_FOLDER_PERMS)
		if err != nil {
			return fmt.Errorf("could not create dump dir: %v", err)
		}
	}

	runcContainer, err := container.GetContainerFromRunc(containerID, root)
	if err != nil {
		return fmt.Errorf("could not get container from runc: %v", err)
	}

	// TODO make into flag and describe how this redirects using container's init process pid and
	// instead a specific pid.

	if state.GPU {
		err = s.gpuDump(ctx, opts.ImagesDirectory)
		if err != nil {
			return err
		}
	}

	err = runcContainer.RuncCheckpoint(opts, runcContainer.Pid, root, runcContainer.Config)
	if err != nil {
		log.Error().Err(err).Send()
		return err
	}

	elapsed := time.Since(start)
	stats.CRIUDuration = elapsed.Milliseconds()

	if !(opts.LeaveRunning) {
		state.JobState = task.JobState_JOB_KILLED
	}

	// CRIU ntfy hooks get run before this,
	// so have to ensure that image files aren't tampered with
	return s.postDump(ctx, opts.ImagesDirectory, state, nil)
}

func (s *service) containerdDump(ctx context.Context, imagePath, containerID string, state *task.ProcessState) error {
	// CRIU ntfy hooks get run before this,
	// so have to ensure that image files aren't tampered with
	return s.postDump(ctx, imagePath, state, nil)
}

func (s *service) setupStreamerCapture(dumpdir string) (*exec.Cmd, error) {
	buf := new(bytes.Buffer)
	cmd := exec.Command("cedana-image-streamer", "--dir", dumpdir, "capture")
	cmd.Stderr = buf
	err := cmd.Start()
	if err != nil {
		log.Error().Msgf("unable to exec image streamer server: %v", err)
		return nil, err
	}
	log.Info().Int("PID", cmd.Process.Pid).Msg("started cedana-image-streamer")

	for buf.Len() == 0 {
		log.Info().Msgf("waiting for cedana-image-streamer to setup...")
		time.Sleep(2 * time.Millisecond)
	}

	return cmd, nil
}

func (s *service) dump(ctx context.Context, state *task.ProcessState, args *task.DumpArgs) error {
	opts := s.prepareDumpOpts()
	dumpdir, err := s.prepareDump(ctx, state, args, opts)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	if args.Stream {
		cmd, err = s.setupStreamerCapture(dumpdir)
		if err != nil {
			return err
		}
	}

	if state.GPU {
		err = s.gpuDump(ctx, dumpdir)
		if err != nil {
			return err
		}
	}

	img, err := os.Open(dumpdir)
	if err != nil {
		log.Warn().Err(err).Msgf("could not open checkpoint storage dir %s", dumpdir)
		return err
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))
	opts.Pid = proto.Int32(state.PID)

	nfy := Notify{}

	log.Info().Int32("PID", state.PID).Msg("beginning dump")

	start := time.Now()
	stats, ok := ctx.Value(utils.DumpStatsKey).(*task.DumpStats)
	if !ok {
		return fmt.Errorf("could not get dump stats from context")
	}

	_, err = s.CRIU.Dump(opts, &nfy)
	if err != nil {
		// check for sudo error
		if strings.Contains(err.Error(), "errno 0") {
			log.Warn().Msgf("error dumping, cedana is not running as root: %v", err)
			return err
		}

		log.Warn().Msgf("error dumping process: %v", err)
		return err
	}

	elapsed := time.Since(start)
	stats.CRIUDuration = elapsed.Milliseconds()

	if !(*opts.LeaveRunning) {
		state.JobState = task.JobState_JOB_KILLED
	}

	return s.postDump(ctx, dumpdir, state, cmd)
}

func (s *service) kataDump(ctx context.Context, state *task.ProcessState, args *task.DumpArgs) error {
	opts := s.prepareDumpOpts()
	dumpdir, err := s.prepareDump(ctx, state, args, opts)
	if err != nil {
		return err
	}

	img, err := os.Open(dumpdir)
	if err != nil {
		log.Warn().Err(err).Msgf("could not open checkpoint storage dir %s", args.Dir)
		return err
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))
	opts.Pid = proto.Int32(state.PID)
	opts.External = append(opts.External, fmt.Sprintf("mnt[]:m"))
	opts.LeaveRunning = proto.Bool(true)

	nfy := Notify{}

	log.Info().Msgf(`beginning dump of pid %d`, state.PID)

	_, err = s.CRIU.Dump(opts, &nfy)
	if err != nil {
		// check for sudo error
		if strings.Contains(err.Error(), "errno 0") {
			log.Warn().Msgf("error dumping, cedana is not running as root: %v", err)
			return err
		}

		log.Warn().Msgf("error dumping process: %v", err)
		return err
	}

	s.postDump(ctx, dumpdir, state, nil)

	conn, err := vsock.Dial(vsock.Host, 9999, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Open the file
	file, err := os.Open(dumpdir + ".tar")
	if err != nil {
		return err
	}
	defer file.Close()

	buffer := make([]byte, 1024)

	// Read from file and send over VSOCK connection
	for {
		bytesRead, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		_, err = conn.Write(buffer[:bytesRead])
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *service) gpuDump(ctx context.Context, dumpdir string) error {
	start := time.Now()
	stats, ok := ctx.Value(utils.DumpStatsKey).(*task.DumpStats)
	if !ok {
		return fmt.Errorf("could not get dump stats from context")
	}
	// TODO NR - these should move out of here and be part of the Client lifecycle
	// setting up a connection could be a source of slowdown for checkpointing
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	gpuConn, err := grpc.Dial("127.0.0.1:50051", opts...)
	if err != nil {
		log.Warn().Msgf("could not connect to gpu controller service: %v", err)
		return err
	}
	defer gpuConn.Close()

	gpuServiceConn := gpu.NewCedanaGPUClient(gpuConn)

	args := gpu.CheckpointRequest{
		Directory: dumpdir,
	}

	resp, err := gpuServiceConn.Checkpoint(ctx, &args)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			log.Error().Str("message", st.Message()).Str("code", st.Code().String()).Msgf("gpu checkpoint failed")
			return fmt.Errorf("gpu checkpoint failed")
		} else {
			return err
		}
	}

	if !resp.Success {
		return fmt.Errorf("could not checkpoint gpu")
	}
	if resp.MemPath == "" {
		return fmt.Errorf("gpu checkpoint did not return mempath")
	}
	if resp.CkptPath == "" {
		return fmt.Errorf("gpu checkpoint did not return ckptpath")
	}

	elapsed := time.Since(start)
	stats.GPUDuration = elapsed.Milliseconds()

	return nil
}
