package api

// Internal functions used by service for restoring processes and containers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/api/runc"
	"github.com/cedana/cedana/pkg/api/services/gpu"
	"github.com/cedana/cedana/pkg/api/services/rpc"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/cedana/cedana/pkg/container"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/typeurl/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/rs/zerolog/log"

	"github.com/opencontainers/runtime-spec/specs-go"

	"github.com/mdlayher/vsock"
)

const (
	CRIU_RESTORE_LOG_FILE        = "cedana-restore.log"
	CRIU_RESTORE_LOG_LEVEL       = 4
	RESTORE_TEMPDIR              = "/tmp/cedana_restore"
	RESTORE_TEMPDIR_PERMS        = 0o755
	RESTORE_OUTPUT_LOG_PATH      = "/var/log/cedana-output-%s.log"
	KATA_RESTORE_OUTPUT_LOG_PATH = "/tmp/cedana-output-%s.log"
	KATA_TAR_FILE_RECEIVER_PORT  = 9998
)

func (s *service) setupStreamerServe(dumpdir string) (*exec.Cmd, error) {
	buf := new(bytes.Buffer)
	cmd := exec.Command("cedana-image-streamer", "--dir", dumpdir, "serve")
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

func (s *service) prepareRestore(ctx context.Context, opts *rpc.CriuOpts, args *task.RestoreArgs, stream task.TaskService_RestoreAttachServer, isKata bool) (*string, *task.ProcessState, []*os.File, []*os.File, error) {
	start := time.Now()
	stats, ok := ctx.Value(utils.RestoreStatsKey).(*task.RestoreStats)
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("could not get restore stats from context")
	}

	var isShellJob bool
	var inheritFds []*rpc.InheritFd
	var tcpEstablished bool
	var extraFiles []*os.File
	var ioFiles []*os.File
	isManagedJob := args.JID != ""

	tempDir := RESTORE_TEMPDIR

	// check if tmpdir exists
	// XXX YA: Tempdir usage is not thread safe
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		err := os.Mkdir(tempDir, RESTORE_TEMPDIR_PERMS)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	} else {
		// likely an old checkpoint hanging around, delete
		err := os.RemoveAll(tempDir)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		err = os.Mkdir(tempDir, RESTORE_TEMPDIR_PERMS)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}

	var cmd *exec.Cmd
	if args.Stream {
		absPath, err := filepath.Abs(args.CheckpointPath)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		tempDir = filepath.Dir(absPath)
		cmd, err = s.setupStreamerServe(tempDir)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		log.Debug().Msgf("cmd = %v", cmd)
	} else {
		err := utils.UntarFolder(args.CheckpointPath, tempDir)
		if err != nil {
			log.Error().Err(err).Msg("error decompressing checkpoint")
			return nil, nil, nil, nil, err
		}
	}

	checkpointState, err := deserializeStateFromDir(tempDir, args.Stream)
	if err != nil {
		log.Error().Err(err).Msg("error unmarshaling checkpoint state")
		return nil, nil, nil, nil, err
	}

	if !isKata {
		open_fds := checkpointState.ProcessInfo.OpenFds

		var in_r, in_w, out_r, out_w, er_r, er_w *os.File
		if stream == nil {
			filename := fmt.Sprintf(RESTORE_OUTPUT_LOG_PATH, fmt.Sprint(time.Now().Unix()))
			out_w, err = os.Create(filename)
		} else {
			in_r, in_w, err = os.Pipe()
			out_r, out_w, err = os.Pipe()
			er_r, er_w, err = os.Pipe()
		}
		if err != nil {
			log.Error().Err(err).Msg("error creating output file")
			return nil, nil, nil, nil, err
		}
		ioFiles = append(ioFiles, in_w)
		ioFiles = append(ioFiles, out_r)
		ioFiles = append(ioFiles, er_r)

		for _, f := range open_fds {
			if strings.Contains(f.Path, "pts") {
				isShellJob = true
			}
			// if stdout or stderr, always redirect fds
			f.Path = strings.TrimPrefix(f.Path, "/")
			if stream == nil {
				if f.Fd == 1 || f.Fd == 2 {
					extraFiles = append(extraFiles, out_w)
					inheritFds = append(inheritFds, &rpc.InheritFd{
						Fd:  proto.Int32(2 + int32(len(extraFiles))),
						Key: proto.String(f.Path),
					})
				}
			} else {
				if f.Fd == 0 {
					extraFiles = append(extraFiles, in_r)
					inheritFds = append(inheritFds, &rpc.InheritFd{
						Fd:  proto.Int32(2 + int32(len(extraFiles))),
						Key: proto.String(f.Path),
					})
				} else if f.Fd == 1 {
					extraFiles = append(extraFiles, out_w)
					inheritFds = append(inheritFds, &rpc.InheritFd{
						Fd:  proto.Int32(2 + int32(len(extraFiles))),
						Key: proto.String(f.Path),
					})
				} else if f.Fd == 2 {
					extraFiles = append(extraFiles, er_w)
					inheritFds = append(inheritFds, &rpc.InheritFd{
						Fd:  proto.Int32(2 + int32(len(extraFiles))),
						Key: proto.String(f.Path),
					})
				}
			}
		}
	} else {
		open_fds := checkpointState.ProcessInfo.OpenFds

		// create logfile for redirection
		filename := fmt.Sprintf(KATA_RESTORE_OUTPUT_LOG_PATH, fmt.Sprint(time.Now().Unix()))
		file, err := os.Create(filename)
		if err != nil {
			log.Error().Err(err).Msg("error creating logfile")
			return nil, nil, nil, nil, err
		}

		for _, f := range open_fds {
			if f.Fd == 0 || f.Fd == 1 || f.Fd == 2 {
				// strip leading slash from f
				f.Path = strings.TrimPrefix(f.Path, "/")

				extraFiles = append(extraFiles, file)
				inheritFds = append(inheritFds, &rpc.InheritFd{
					Fd:  proto.Int32(2 + int32(len(extraFiles))),
					Key: proto.String(f.Path),
				})
			}
		}
	}

	// TODO NR - wtf?
	for _, oc := range checkpointState.ProcessInfo.OpenConnections {
		if oc.Type == syscall.SOCK_STREAM { // TCP
			tcpEstablished = true
		}
	}

	opts.ShellJob = proto.Bool(isShellJob)
	opts.Stream = proto.Bool(args.Stream)
	opts.InheritFd = inheritFds
	opts.TcpEstablished = proto.Bool(tcpEstablished || args.GetCriuOpts().GetTcpEstablished())
	opts.RstSibling = proto.Bool(isManagedJob) // restore as pure child of daemon

	if err := chmodRecursive(tempDir, RESTORE_TEMPDIR_PERMS); err != nil {
		log.Error().Err(err).Msg("error changing permissions")
		return nil, nil, nil, nil, err
	}

	elapsed := time.Since(start)
	stats.PrepareDuration = elapsed.Milliseconds()

	return &tempDir, checkpointState, extraFiles, ioFiles, nil
}

// chmodRecursive changes the permissions of the given path and all its contents.
func chmodRecursive(path string, mode os.FileMode) error {
	return filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chmod(filePath, mode)
	})
}

func chownRecursive(path string, uid, gid int32) error {
	return filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(filePath, int(uid), int(gid))
	})
}

func (s *service) containerdRestore(ctx context.Context, imgPath string, containerId string) error {
	log.Info().Msgf("restoring container %s from %s", containerId, imgPath)
	err := container.Restore(imgPath, containerId)
	if err != nil {
		return err
	}
	return nil
}

func (s *service) prepareRestoreOpts() *rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel: proto.Int32(CRIU_RESTORE_LOG_LEVEL),
		LogFile:  proto.String(CRIU_RESTORE_LOG_FILE),
	}

	return &opts
}

func (s *service) criuRestore(ctx context.Context, opts *rpc.CriuOpts, nfy Notify, dir string, extraFiles []*os.File) (*int32, error) {
	start := time.Now()
	stats, ok := ctx.Value(utils.RestoreStatsKey).(*task.RestoreStats)
	if !ok {
		return nil, fmt.Errorf("could not get restore stats from context")
	}

	img, err := os.Open(dir)
	if err != nil {
		log.Error().Err(err).Msg("could not open directory")
		return nil, err
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))

	resp, err := s.CRIU.Restore(opts, &nfy, extraFiles)
	if err != nil {
		// cleanup along the way
		os.RemoveAll(dir)
		log.Warn().Msgf("error restoring process: %v", err)
		return nil, err
	}

	log.Info().Int32("PID", *resp.Restore.Pid).Msgf("process restored")

	elapsed := time.Since(start)
	stats.CRIUDuration = elapsed.Milliseconds()

	return resp.Restore.Pid, nil
}

func recursivelyReplace(data interface{}, oldValue, newValue string) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if str, isString := value.(string); isString {
				v[key] = strings.Replace(str, oldValue, newValue, -1)
			} else {
				recursivelyReplace(value, oldValue, newValue)
			}
		}
	case []interface{}:
		for _, value := range v {
			recursivelyReplace(value, oldValue, newValue)
		}
	}
}

type linkPairs struct {
	Key   string
	Value string
}

func (s *service) runcRestore(ctx context.Context, imgPath, containerId string, criuOpts *container.CriuOpts, opts *container.RuncOpts, isManagedJob bool) (int32, chan int, error) {
	start := time.Now()
	stats, ok := ctx.Value(utils.RestoreStatsKey).(*task.RestoreStats)
	if !ok {
		return 0, nil, fmt.Errorf("could not get restore stats from context")
	}

	state, err := deserializeStateFromDir(imgPath, false)
	if err != nil {
		return 0, nil, fmt.Errorf("does the img path exist? %w", err)
	}

	var gpuCmd *exec.Cmd
	gpuOutBuf := &bytes.Buffer{}

	// FIXME: GPU restore should instead be done in the pre-resume hook, so as to ensure it does not
	// continue to run as an orphan process if the restore fails early. Once process has started, then
	// it's lifecycle is tied to it (see below goroutines).
	if state.GPU {
		var err error
		gpuCmd, err = s.gpuRestore(ctx, imgPath, state.UIDs[0], state.GIDs[0], state.Groups, io.Writer(gpuOutBuf))
		if err != nil {
			log.Error().Err(err).Str("stdout/stderr", gpuOutBuf.String()).Msg("failed to restore GPU")
			return 0, nil, err
		}
	}

	if opts.Bundle == "" {
		opts.Bundle = state.ContainerBundle // Use saved bundle if not overridden from args
	}

	err = container.RuncRestore(imgPath, containerId, criuOpts, opts)
	if err != nil {
		// Kill GPU controller if it was started
		// FIXME: Remove later when GPU controller is started in pre-resume hook
		if gpuCmd != nil {
			gpuCmd.Process.Kill()
			gpuCmd.Wait()
		}

		return 0, nil, err
	}

	// HACK YA: RACE The container might not exit yet
	// time.Sleep(1 * time.Second)

	pid, err := runc.GetPidByContainerId(containerId, opts.Root)
	if err != nil {
		// Kill GPU controller if it was started
		// FIXME: Remove later when GPU controller is started in pre-resume hook
		if gpuCmd != nil {
			gpuCmd.Wait()
			gpuCmd.Process.Kill()
		}

		return 0, nil, fmt.Errorf("failed to get pid by container id: %w", err)
	}

	exitCode := make(chan int)
	if isManagedJob {
		// Wait to cleanup
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			var status syscall.WaitStatus
			syscall.Wait4(int(pid), &status, 0, nil) // since managed jobs are restored as children of the daemon
			code := status.ExitStatus()
			log.Info().Int32("PID", pid).Str("JID", containerId).Int("status", code).Msgf("runc container exited")

			if gpuCmd != nil {
				err = gpuCmd.Process.Kill()
				if err != nil {
					log.Error().Err(err).Msg("failed to kill GPU controller after runc container exit")
				}
			}

			exitCode <- code
		}()

		// Clean up GPU controller and also handle premature exit
		if gpuCmd != nil {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				err := gpuCmd.Wait()
				if err != nil {
					log.Debug().Err(err).Msg("GPU controller Wait()")
				}
				log.Info().Int("PID", gpuCmd.Process.Pid).
					Int("status", gpuCmd.ProcessState.ExitCode()).
					Str("out/err", gpuOutBuf.String()).
					Msg("GPU controller exited")

				// Should kill process if still running since GPU controller might have exited prematurely
				syscall.Kill(int(pid), syscall.SIGKILL)
			}()
		}

		// Kill on server/stream shutdown
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			<-s.serverCtx.Done()
			log.Debug().Int32("PID", pid).Str("JID", containerId).Msgf("killing runc container")
			syscall.Kill(int(pid), syscall.SIGKILL)
			if err != nil {
				log.Warn().Err(err).Int32("PID", pid).Str("JID", containerId).Msgf("could not kill runc container")
				return
			}
		}()
	}

	elapsed := time.Since(start)
	stats.CRIUDuration = elapsed.Milliseconds()

	return pid, exitCode, nil
}

// Bundle represents an OCI bundle
type OCIBundle struct {
	// ID of the bundle
	ID string
	// Path to the bundle
	Path string
	// Namespace of the bundle
	Namespace string
}

// ociSpecUserNS is a subset of specs.Spec used to reduce garbage during
// unmarshal.
type ociSpecUserNS struct {
	Linux *linuxSpecUserNS
}

// linuxSpecUserNS is a subset of specs.Linux used to reduce garbage during
// unmarshal.
type linuxSpecUserNS struct {
	GIDMappings []specs.LinuxIDMapping
}

// remappedGID reads the remapped GID 0 from the OCI spec, if it exists. If
// there is no remapping, remappedGID returns 0. If the spec cannot be parsed,
// remappedGID returns an error.
func remappedGID(spec []byte) (uint32, error) {
	var ociSpec ociSpecUserNS
	err := json.Unmarshal(spec, &ociSpec)
	if err != nil {
		return 0, err
	}
	if ociSpec.Linux == nil || len(ociSpec.Linux.GIDMappings) == 0 {
		return 0, nil
	}
	for _, mapping := range ociSpec.Linux.GIDMappings {
		if mapping.ContainerID == 0 {
			return mapping.HostID, nil
		}
	}
	return 0, nil
}

// prepareBundleDirectoryPermissions prepares the permissions of the bundle
// directory according to the needs of the current platform.
// On Linux when user namespaces are enabled, the permissions are modified to
// allow the remapped root GID to access the bundle.
func prepareBundleDirectoryPermissions(path string, spec []byte) error {
	gid, err := remappedGID(spec)
	if err != nil {
		return err
	}
	if gid == 0 {
		return nil
	}
	if err := os.Chown(path, -1, int(gid)); err != nil {
		return err
	}
	return os.Chmod(path, 0710)
}

// NewBundle returns a new bundle on disk
func NewBundle(ctx context.Context, root, state, id string, spec typeurl.Any) (b *OCIBundle, err error) {
	if err := identifiers.Validate(id); err != nil {
		return nil, fmt.Errorf("invalid task id %s: %w", id, err)
	}

	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}
	work := filepath.Join(root, ns, id)
	b = &OCIBundle{
		ID:        id,
		Path:      filepath.Join(state, ns, id),
		Namespace: ns,
	}
	var paths []string
	defer func() {
		if err != nil {
			for _, d := range paths {
				os.RemoveAll(d)
			}
		}
	}()
	// create state directory for the bundle
	if err := os.MkdirAll(filepath.Dir(b.Path), 0711); err != nil {
		return nil, err
	}
	if err := os.Mkdir(b.Path, 0700); err != nil {
		return nil, err
	}
	if typeurl.Is(spec, &specs.Spec{}) {
		if err := prepareBundleDirectoryPermissions(b.Path, spec.GetValue()); err != nil {
			return nil, err
		}
	}
	paths = append(paths, b.Path)
	// create working directory for the bundle
	if err := os.MkdirAll(filepath.Dir(work), 0711); err != nil {
		return nil, err
	}
	rootfs := filepath.Join(b.Path, "rootfs")
	if err := os.MkdirAll(rootfs, 0711); err != nil {
		return nil, err
	}
	paths = append(paths, rootfs)
	if err := os.Mkdir(work, 0711); err != nil {
		if !os.IsExist(err) {
			return nil, err
		}
		os.RemoveAll(work)
		if err := os.Mkdir(work, 0711); err != nil {
			return nil, err
		}
	}
	paths = append(paths, work)
	// symlink workdir
	if err := os.Symlink(work, filepath.Join(b.Path, "work")); err != nil {
		return nil, err
	}
	if spec := spec.GetValue(); spec != nil {
		// write the spec to the bundle
		specPath := filepath.Join(b.Path, "config.json")
		err = os.WriteFile(specPath, spec, 0666)
		if err != nil {
			return nil, fmt.Errorf("failed to write bundle spec: %w", err)
		}
	}
	return b, nil
}

// Define the rsync command and arguments
// Set the output and error streams to os.Stdout and os.Stderr to see the output of rsync
// Run the rsync command

// Using rsync instead of cp -r, for some reason cp -r was not copying all the files and directories over but rsync does...
func rsyncDirectories(source, destination string) error {
	cmd := exec.Command("sudo", "rsync", "-av", "--exclude=attach", source, destination)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func (s *service) restore(ctx context.Context, args *task.RestoreArgs, stream task.TaskService_RestoreAttachServer) (int32, chan int, error) {
	var dir *string
	var pid *int32

	opts := s.prepareRestoreOpts()
	nfy := Notify{}

	dir, state, extraFiles, ioFiles, err := s.prepareRestore(ctx, opts, args, stream, false)
	if err != nil {
		return 0, nil, err
	}

	var gpuCmd *exec.Cmd
	gpuOutBuf := &bytes.Buffer{}

	// No GPU flag passed in args - if state.GPU = true, always restore using gpu-controller
	if state.GPU {
		// NOTE: Running on pre-resume hook also ensures that there's little room for failure as the
		// process is just about to be resumed. If we do GPU restore too early, then we will have to
		// ensure it's killed on every failure. Once the process starts, then it's lifecycle is tied to it
		// (see below goroutines).
		nfy.PreResumeFunc = NotifyFunc{
			Avail: true,
			Callback: func() error {
				var err error
				gpuCmd, err = s.gpuRestore(ctx, *dir, state.UIDs[0], state.GIDs[0], state.Groups, io.Writer(gpuOutBuf))
				if err != nil {
					log.Error().Err(err).Str("stdout/stderr", gpuOutBuf.String()).Msg("failed to restore GPU")
				}
				return err
			},
		}
	}

	pid, err = s.criuRestore(ctx, opts, nfy, *dir, extraFiles)
	if err != nil {
		return 0, nil, err
	}

	// If it's a managed restored job
	exitCode := make(chan int)
	if args.JID != "" {
		if stream != nil {
			// last 3 files of ioFiles are stdin, stdout, stderr
			in := ioFiles[len(ioFiles)-3]
			out := ioFiles[len(ioFiles)-2]
			er := ioFiles[len(ioFiles)-1]

			// Receive stdin from stream
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer in.Close()
				for {
					resp, err := stream.Recv()
					if err != nil {
						log.Debug().Err(err).Msg("finished reading stdin")
						return
					}
					_, err = in.Write([]byte(resp.Stdin))
					if err != nil {
						log.Error().Err(err).Msg("failed to write to stdin")
						return
					}
				}
			}()
			// Scan stdout
			stdoutScanner := bufio.NewScanner(out)
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer out.Close()
				for stdoutScanner.Scan() {
					if err := stream.Send(&task.AttachResp{Stdout: stdoutScanner.Text() + "\n"}); err != nil {
						log.Error().Err(err).Msg("failed to send stdout")
						return
					}
				}
				if err := stdoutScanner.Err(); err != nil {
					log.Debug().Err(err).Msgf("finished reading stdout")
				}
			}()
			// Scan stderr
			stderrScanner := bufio.NewScanner(er)
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer er.Close()
				for stderrScanner.Scan() {
					if err := stream.Send(&task.AttachResp{Stderr: stderrScanner.Text() + "\n"}); err != nil {
						log.Error().Err(err).Msg("failed to send stderr")
						return
					}
				}
				if err := stderrScanner.Err(); err != nil {
					log.Debug().Err(err).Msgf("finished reading stderr")
				}
			}()
		}

		// Wait to cleanup
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			var status syscall.WaitStatus
			syscall.Wait4(int(*pid), &status, 0, nil) // since managed jobs are restored as children of the daemon
			code := status.ExitStatus()
			log.Info().Int32("PID", *pid).Str("JID", args.JID).Int("status", code).Msgf("process exited")

			if gpuCmd != nil {
				err = gpuCmd.Process.Kill()
				if err != nil {
					log.Error().Err(err).Msg("failed to kill GPU controller after process exit")
				}
			}

			exitCode <- code
		}()

		// Clean up GPU controller and also handle premature exit
		if gpuCmd != nil {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				err := gpuCmd.Wait()
				if err != nil {
					log.Debug().Err(err).Msg("GPU controller Wait()")
				}
				log.Info().Int("PID", gpuCmd.Process.Pid).
					Int("status", gpuCmd.ProcessState.ExitCode()).
					Str("out/err", gpuOutBuf.String()).
					Msg("GPU controller exited")

				// Should kill process if still running since GPU controller might have exited prematurely
				syscall.Kill(int(*pid), syscall.SIGKILL)
			}()
		}

		// Kill on server/stream shutdown
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if stream != nil {
				defer ioFiles[len(ioFiles)-3].Close()
				defer ioFiles[len(ioFiles)-2].Close()
				defer ioFiles[len(ioFiles)-1].Close()
			}
			var cmdCtx context.Context
			if stream != nil {
				cmdCtx = utils.CombineContexts(s.serverCtx, stream.Context()) // either should terminate the process
			} else {
				cmdCtx = s.serverCtx
			}
			<-cmdCtx.Done()
			log.Debug().Int32("PID", *pid).Str("JID", args.JID).Msgf("killing process")
			syscall.Kill(int(*pid), syscall.SIGKILL)
			if err != nil {
				log.Warn().Err(err).Int32("PID", *pid).Str("JID", args.JID).Msgf("could not kill process")
				return
			}
		}()
	}

	return *pid, exitCode, nil
}

func (s *service) kataRestore(ctx context.Context, args *task.RestoreArgs) (*int32, error) {
	var dir *string
	var pid *int32

	opts := s.prepareRestoreOpts()
	nfy := Notify{}

	listener, err := vsock.Listen(KATA_TAR_FILE_RECEIVER_PORT, nil)
	if err != nil {
		return nil, err
	}
	defer listener.Close()

	conn, err := listener.Accept()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Open the file for writing
	recvFile, err := os.Create(args.CheckpointPath)
	if err != nil {
		return nil, err
	}
	defer recvFile.Close()

	buffer := make([]byte, 1024)

	// Receive data and write to file
	for {
		bytesReceived, err := conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		_, err = recvFile.Write(buffer[:bytesReceived])
		if err != nil {
			return nil, err
		}
	}

	dir, _, extraFiles, _, err := s.prepareRestore(ctx, opts, args, nil, true)
	if err != nil {
		return nil, err
	}

	opts.External = append(opts.External, "mnt[]:m")
	opts.Root = proto.String("/run/kata-containers/shared/containers/" + args.CheckpointID + "/rootfs")

	pid, err = s.criuRestore(ctx, opts, nfy, *dir, extraFiles)
	if err != nil {
		return nil, err
	}

	return pid, nil
}

func (s *service) gpuRestore(ctx context.Context, dir string, uid, gid int32, groups []int32, out io.Writer) (*exec.Cmd, error) {
	start := time.Now()
	stats, ok := ctx.Value(utils.RestoreStatsKey).(*task.RestoreStats)
	if !ok {
		return nil, fmt.Errorf("could not get restore stats from context")
	}

	gpuCmd, err := s.StartGPUController(ctx, uid, gid, groups, out)
	if err != nil {
		log.Warn().Msgf("could not start cedana-gpu-controller: %v", err)
		return nil, err
	}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	gpuConn, err := grpc.NewClient("127.0.0.1:50051", opts...)
	if err != nil {
		log.Error().Msgf("fail to dial GPU controller: %v", err)
		return nil, fmt.Errorf("fail to dial GPU controller: %v", err)
	}
	defer gpuConn.Close()

	gpuServiceConn := gpu.NewCedanaGPUClient(gpuConn)

	args := gpu.RestoreRequest{
		Directory: dir,
	}
	resp, err := gpuServiceConn.Restore(ctx, &args)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			log.Error().Str("message", st.Message()).Str("code", st.Code().String()).Msgf("gpu checkpoint failed")
			return nil, fmt.Errorf("gpu checkpoint failed")
		} else {
			return nil, err
		}
	}

	if resp.GpuRestoreStats != nil {
		stats.GPURestoreStats = &gpu.GPURestoreStats{
			CopyMemTime:     resp.GpuRestoreStats.CopyMemTime,
			ReplayCallsTime: resp.GpuRestoreStats.ReplayCallsTime,
		}
	}
	log.Info().Msgf("gpu controller returned %v", resp)

	if !resp.Success {
		return nil, fmt.Errorf("could not restore gpu")
	}

	elapsed := time.Since(start)
	stats.GPUDuration = elapsed.Milliseconds()

	return gpuCmd, nil
}
