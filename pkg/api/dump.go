package api

// Internal functions used by service for dumping processes and containers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	criu "buf.build/gen/go/cedana/criu/protocolbuffers/go"
	gpu "buf.build/gen/go/cedana/gpu/protocolbuffers/go/cedanagpu"
	task "buf.build/gen/go/cedana/task/protocolbuffers/go"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cedana/cedana/pkg/api/kata"
	"github.com/cedana/cedana/pkg/container"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/xid"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"google.golang.org/grpc/codes"
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

type KataService interface {
	KataDump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error)
}

func useKataDump(s KataService) {

}

// prepareDump =/= preDump.
// prepareDump sets up the folders to dump into, and sets the criu options.
// preDump on the other hand does any process cleanup right before the checkpoint.
func (s *service) prepareDump(ctx context.Context, state *task.ProcessState, args *task.DumpArgs, opts *criu.CriuOpts) (string, *exec.Cmd, error) {
	stats, ok := ctx.Value(utils.DumpStatsKey).(*task.DumpStats)
	if !ok {
		return "", nil, fmt.Errorf("could not get dump stats from context")
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
	opts.TcpClose = proto.Bool(args.GetCriuOpts().GetTcpClose())
	opts.TcpSkipInFlight = proto.Bool(args.GetCriuOpts().GetTcpSkipInFlight())
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
	opts.Stream = proto.Bool(args.Stream > 0)

	// jobID + UTC time (nanoseconds)
	// strip out non posix-compliant characters from the jobID
	var dumpDirPath string
	if args.Stream > 0 {
		dumpDirPath = args.Dir
	} else {
		timeString := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
		processDumpDir := strings.Join([]string{state.JID, timeString}, "_")
		dumpDirPath = filepath.Join(args.Dir, processDumpDir)
	}
	_, err := os.Stat(dumpDirPath)
	if err != nil {
		if err := os.MkdirAll(dumpDirPath, DUMP_FOLDER_PERMS); err != nil {
			return "", nil, err
		}
	}

	err = os.Chown(args.Dir, int(state.UIDs[0]), int(state.GIDs[0]))
	if err != nil {
		return "", nil, err
	}
	err = chownRecursive(dumpDirPath, state.UIDs[0], state.GIDs[0])
	if err != nil {
		return "", nil, err
	}

	err = os.Chmod(args.Dir, DUMP_FOLDER_PERMS)
	if err != nil {
		return "", nil, err
	}
	err = chmodRecursive(dumpDirPath, DUMP_FOLDER_PERMS)
	if err != nil {
		return "", nil, err
	}

	// close common fds
	err = closeCommonFds(int32(os.Getpid()), state.PID)
	if err != nil {
		return "", nil, err
	}

	// setup cedana-image-streamer
	var streamCmd *exec.Cmd
	if args.Stream > 0 {
		streamCmd, err = s.setupStreamerCapture(ctx, dumpDirPath, state.GPU, args.Bucket, args.Stream)
		if err != nil {
			return "", nil, err
		}
	}

	elapsed := time.Since(start)
	stats.PrepareDuration = elapsed.Milliseconds()

	return dumpDirPath, streamCmd, nil
}

func getBucketSize(bucket string) (int64, error) {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return 0, err
	}
	s3Client := s3.NewFromConfig(cfg)
	paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
		Bucket: &bucket,
	})
	var totalSize int64 = 0
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return 0, err
		}
		for _, obj := range page.Contents {
			totalSize += *obj.Size
		}
	}
	return totalSize, nil
}

func getDumpdirSize(path string) (int64, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var size int64
	var err error

	handleFile := func(filePath string, info os.DirEntry) {
		defer wg.Done()
		if !info.IsDir() {
			if strings.HasSuffix(filePath, ".lz4") {
				fileInfo, fileErr := info.Info()
				if fileErr != nil {
					mu.Lock()
					if err == nil {
						err = fmt.Errorf("error reading file info for %s: %w", filePath, fileErr)
					}
					mu.Unlock()
					return
				}
				mu.Lock()
				size += fileInfo.Size()
				mu.Unlock()
			}
		}
	}

	err = filepath.WalkDir(path, func(filePath string, info os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		wg.Add(1)
		go handleFile(filePath, info)
		return nil
	})

	wg.Wait()
	if err != nil {
		return 0, err
	}

	return size, nil
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
		compressedCheckpointPath = dumpdir
	} else {
		compressedCheckpointPath = strings.Join([]string{dumpdir, ".tar"}, "")
	}
	log.Info().Msgf("compressedCheckpointPath = %s", compressedCheckpointPath)

	state.CheckpointPath = compressedCheckpointPath
	state.CheckpointState = task.CheckpointState_CHECKPOINTED

	// sneak in a serialized state obj
	err := serializeStateToDir(dumpdir, state, streamCmd != nil)
	if err != nil {
		log.Error().Err(err)
		return err
	}

	ready_path := filepath.Join(dumpdir, "ckpt")
	ready_file, err := os.Create(ready_path)
	if err != nil {
		log.Fatal().Err(err)
	}
	defer ready_file.Close()
	log.Info().Msg("created ready file, cedana-image-streamer shutting down")

	var size int64
	if streamCmd != nil {
		streamCmd.Wait()
		log.Info().Str("Path", compressedCheckpointPath).Msg("getting checkpoint size")
		var bucket string
		remote := false
		for i, arg := range streamCmd.Args {
			if arg == "--bucket" && i+1 < len(streamCmd.Args) {
				bucket = streamCmd.Args[i+1] // bucket name follows bucket flag
				remote = true
				break
			}
		}
		if remote {
			size, err = getBucketSize(bucket)
		} else {
			size, err = getDumpdirSize(compressedCheckpointPath)
		}
		if err != nil {
			log.Fatal().Err(err)
		}
	} else {
		log.Info().Str("Path", compressedCheckpointPath).Msg("compressing checkpoint")
		err = utils.TarFolder(dumpdir, compressedCheckpointPath)
		if err != nil {
			log.Error().Err(err)
			return nil
		}
		// get size of compressed checkpoint
		info, err := os.Stat(compressedCheckpointPath)
		if err != nil {
			log.Fatal().Err(err)
		}
		size = info.Size()
	}

	elapsed := time.Since(start)
	stats.CheckpointFileStats = &task.CheckpointFileStats{
		Size:     size,
		Duration: elapsed.Milliseconds(),
	}

	// final update to db
	err = s.updateState(ctx, state.JID, state)
	if err != nil {
		log.Error().Err(err)
		return err
	}

	return nil
}

func (s *service) prepareDumpOpts() *criu.CriuOpts {
	opts := criu.CriuOpts{
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
		err = s.gpuDump(ctx, opts.ImagesDirectory, false, state.JID)
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

func (s *service) setupStreamerCapture(ctx context.Context, dumpdir string, gpu bool, bucket string, num_pipes int32) (*exec.Cmd, error) {
	args := []string{"--dir", dumpdir, "--num-pipes", fmt.Sprint(num_pipes)}
	if gpu {
		args = append(args, "--gpu")
	}
	if bucket != "" {
		args = append(args, "--bucket", bucket)
	}
	args = append(args, "capture") // subcommand must be after options
	cmd := exec.CommandContext(ctx, "cedana-image-streamer", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	outpipe := bufio.NewReader(stdout)
	go func() {
		for {
			line, err := outpipe.ReadString('\n')
			if err != nil {
				break
			}
			line = strings.TrimSuffix(line, "\n")
			log.Info().Msg(line)
		}
	}()
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	i := 0
	errpipe := bufio.NewReader(stderr)
	go func() {
		for {
			line, err := errpipe.ReadString('\n')
			if err != nil {
				break
			}
			line = strings.TrimSuffix(line, "\n")
			if i != 0 {
				log.Error().Msg(line)
			}
			i += 1
		}
	}()
	err = cmd.Start()
	if err != nil {
		log.Error().Msgf("unable to exec image streamer server: %v", err)
		return nil, err
	}
	log.Info().Int("PID", cmd.Process.Pid).Msg("started cedana-image-streamer")

	for i == 0 {
		log.Info().Msgf("waiting for cedana-image-streamer to setup...")
		time.Sleep(2 * time.Millisecond)
	}

	return cmd, nil
}

func (s *service) dump(ctx context.Context, state *task.ProcessState, args *task.DumpArgs) error {
	opts := s.prepareDumpOpts()
	dumpdir, streamCmd, err := s.prepareDump(ctx, state, args, opts)
	if err != nil {
		return err
	}

	if state.GPU {
		err = s.gpuDump(ctx, dumpdir, args.Stream > 0, state.JID)
		if err != nil {
			return err
		}
		log.Info().Msg("gpu dumped")
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

	return s.postDump(ctx, dumpdir, state, streamCmd)
}

func (s *service) kataDump(ctx context.Context, state *task.ProcessState, args *task.DumpArgs) error {
	opts := s.prepareDumpOpts()
	dumpdir, _, err := s.prepareDump(ctx, state, args, opts)
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
	allExternalMounts, err := findAllExternalBindMounts()
	if err != nil {
		return err
	}

	// Here we extend external mounts to include those from the config.json of the container
	// a 2D array is used as the function does not assume 1 container in kata vm but here
	// it is assumed we only use the first container found, this is the case for non-side car
	// containers which we do not yet support.
	opts.External = append(opts.External, allExternalMounts[0]...)
	opts.LeaveRunning = proto.Bool(true)
	opts.OrphanPtsMaster = proto.Bool(true)

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

func (s *service) gpuDump(ctx context.Context, dumpdir string, stream bool, jid string) error {
	start := time.Now()
	stats, ok := ctx.Value(utils.DumpStatsKey).(*task.DumpStats)
	if !ok {
		return fmt.Errorf("could not get dump stats from context")
	}

	gpuController := s.GetGPUController(jid)
	if gpuController == nil {
		return fmt.Errorf("did not find gpu controller for job %s", jid)
	}

	args := gpu.CheckpointRequest{
		Directory: dumpdir,
		Stream:    stream,
	}

	resp, err := gpuController.Client.Checkpoint(ctx, &args)
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

const requestTimeout = 30 * time.Second

// Does rpc over vsock to kata vm for the cedana KataDump function
func (s *service) HostKataDump(ctx context.Context, args *task.HostDumpKataArgs) (*task.HostDumpKataResp, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	vm := args.VmName
	port := args.Port
	dir := args.Dir

	if args.VMSnapshot {
		err := s.vmSnapshotter.Pause(args.VMSocketPath)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Checkpoint task failed: %v", err)
		}

		var resumeErr error
		defer func() {
			if err := s.vmSnapshotter.Resume(args.VMSocketPath); err != nil {
				resumeErr = status.Errorf(codes.Internal, "Checkpoint task failed during resume: %v", err)
			}
		}()

		err = s.vmSnapshotter.Snapshot(args.Dir, args.VMSocketPath)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Checkpoint task failed during snapshot: %v", err)
		}

		if resumeErr != nil {
			return nil, resumeErr
		}
		return &task.HostDumpKataResp{TarDumpDir: args.Dir}, nil
	}

	cts, err := kata.NewVSockClient(vm, port)
	if err != nil {
		log.Error().Msgf("Error creating client: %v", err)
		return nil, status.Errorf(codes.Internal, "Error creating client: %v", err)
	}
	defer cts.Close()

	id := xid.New().String()
	log.Info().Msgf("no job id specified, using %s", id)

	cpuDumpArgs := task.DumpArgs{
		Dir: "/tmp",
		JID: id,
	}

	go func() {
		listener, err := vsock.Listen(9999, nil)
		if err != nil {
			log.Error().Msgf("Failed to start vsock listener: %v", err)
			return
		}
		defer listener.Close()

		conn, err := listener.Accept()
		if err != nil {
			log.Error().Msgf("Failed to accept connection: %v", err)
			return
		}
		defer conn.Close()

		file, err := os.Create(dir + "/dmp.tar")
		if err != nil {
			log.Error().Msgf("Failed to create file: %v", err)
			return
		}
		defer file.Close()

		buffer := make([]byte, 1024)

		for {
			select {
			case <-ctx.Done():
				log.Error().Msg("File write operation canceled due to context timeout")
				return
			default:
				bytesReceived, err := conn.Read(buffer)
				if err != nil {
					if err == io.EOF {
						break
					}
					log.Error().Msgf("Error reading data: %v", err)
					return
				}

				_, err = file.Write(buffer[:bytesReceived])
				if err != nil {
					log.Error().Msgf("Error writing to file: %v", err)
					return
				}
			}
		}
	}()

	resp, err := cts.KataDump(ctx, &cpuDumpArgs)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			log.Error().Msgf("Checkpoint task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			return nil, st.Err()
		}
		log.Error().Msgf("Checkpoint task failed: %v", err)
		return nil, status.Errorf(codes.Internal, "Checkpoint task failed: %v", err)
	}

	log.Info().Msgf("Response: %v", resp.Message)
	// TODO implement the host tar dump dir location
	return &task.HostDumpKataResp{TarDumpDir: "NOT IMPLEMENTED"}, nil
}
