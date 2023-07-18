package container

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/checkpoint-restore/go-criu/v5"
	criurpc "github.com/checkpoint-restore/go-criu/v5/rpc"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	dockercli "github.com/docker/docker/client"
	"github.com/nravic/cedana/utils"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/cgroups/manager"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

type RuncContainer struct {
	id                   string
	root                 string
	pid                  int
	config               *configs.Config // standin for configs.Config from runc
	cgroupManager        cgroups.Manager
	initProcessStartTime uint64
	m                    sync.Mutex
	criuVersion          int
	created              time.Time
	dockerConfig         *types.ContainerJSON
}

// this comes from runc, see github.com/opencontainers/runc
// they use an external CriuOpts struct that's populated
type VethPairName struct {
	ContainerInterfaceName string
	HostInterfaceName      string
}

// Higher level CriuOptions that are used to turn on/off the flags passed to criu
type CriuOpts struct {
	ImagesDirectory         string             // directory for storing image files
	WorkDirectory           string             // directory to cd and write logs/pidfiles/stats to
	ParentImage             string             // directory for storing parent image files in pre-dump and dump
	LeaveRunning            bool               // leave container in running state after checkpoint
	TcpEstablished          bool               // checkpoint/restore established TCP connections
	ExternalUnixConnections bool               // allow external unix connections
	ShellJob                bool               // allow to dump and restore shell jobs
	FileLocks               bool               // handle file locks, for safety
	PreDump                 bool               // call criu predump to perform iterative checkpoint
	VethPairs               []VethPairName     // pass the veth to criu when restore
	ManageCgroupsMode       criurpc.CriuCgMode // dump or restore cgroup mode
	EmptyNs                 uint32             // don't c/r properties for namespace from this mask
	AutoDedup               bool               // auto deduplication for incremental dumps
	LazyPages               bool               // restore memory pages lazily using userfaultfd
	StatusFd                int                // fd for feedback when lazy server is ready
	LsmProfile              string             // LSM profile used to restore the container
	LsmMountContext         string             // LSM mount context value to use during restore
}

// Pretty wacky function. "creates" a runc container from a docker container,
// basically piecing it together from information we can parse out from the docker go lib
func getContainerFromDocker(containerID string) *RuncContainer {
	l := utils.GetLogger()
	cli, err := dockercli.NewClientWithOpts(client.FromEnv)
	if err != nil {
		l.Fatal().Err(err).Msg("could not create docker client")
	}

	cli.NegotiateAPIVersion(context.Background())

	container, err := cli.ContainerInspect(context.Background(), containerID)
	if err != nil {
		l.Fatal().Err(err).Msg("could not inspect container")
	}

	criu := criu.MakeCriu()
	criuVersion, err := criu.GetCriuVersion()
	if err != nil {
		l.Fatal().Err(err).Msg("could not get criu version")
	}

	// need to build a config from the information we can parse out from the docker lib
	// start with bare minimum
	runcConf := &configs.Config{
		Rootfs: container.GraphDriver.Data["MergedDir"], // does this work lol
	}

	// create a cgroup manager for cgroup freezing
	// need c.Path, c.Parent & c.Name, c.Systemd. We can grab t his from proc/pid
	var cgroupsConf *configs.Cgroup
	if container.State.Pid != 0 {
		cgroupPaths := []string{fmt.Sprintf("/proc/%d/cgroup", container.State.Pid)}
		// assume we're in cgroup v2 unified
		// for cgroup v2 unified hierarchy, there are no per-controller cgroup paths
		cgroupsPaths, err := cgroups.ParseCgroupFile(cgroupPaths[0])
		if err != nil {
			l.Fatal().Err(err).Msg("could not parse cgroup paths")
		}

		path := cgroupsPaths[""]

		// Splitting the string by / separator
		cgroupParts := strings.Split(path, "/")

		if len(cgroupParts) < 3 {
			l.Fatal().Err(err).Msg("could not parse cgroup path")
		}

		name := cgroupParts[2]
		parent := cgroupParts[1]
		cgpath := "/" + parent + "/" + name

		var isSystemd bool
		if strings.Contains(path, ".slice") {
			isSystemd = true
		}

		cgroupsConf = &configs.Cgroup{
			Parent:  parent,
			Name:    name,
			Path:    cgpath,
			Systemd: isSystemd,
		}

	}

	cgroupManager, err := manager.New(cgroupsConf)
	if err != nil {
		l.Fatal().Err(err).Msg("could not create cgroup manager")
	}

	// this is so stupid hahahaha
	c := &RuncContainer{
		id:            containerID,
		root:          fmt.Sprintf("%s", container.Config.WorkingDir),
		criuVersion:   criuVersion,
		cgroupManager: cgroupManager,
		dockerConfig:  &container,
		config:        runcConf,
		pid:           container.State.Pid,
	}

	return c
}

func Dump(dir string, containerID string) error {
	// create a CriuOpts and pass into RuncCheckpoint
	opts := &CriuOpts{
		ImagesDirectory: dir,
		LeaveRunning:    false,
	}

	c := getContainerFromDocker(containerID)

	err := c.RuncCheckpoint(opts, c.pid)
	if err != nil {
		return err
	}

	return nil
}

func (c *RuncContainer) RuncCheckpoint(criuOpts *CriuOpts, pid int) error {
	c.m.Lock()
	defer c.m.Unlock()

	// Checkpoint is unlikely to work if os.Geteuid() != 0 || system.RunningInUserNS().
	// (CLI prints a warning)
	// TODO(avagin): Figure out how to make this work nicely. CRIU 2.0 has
	//               support for doing unprivileged dumps, but the setup of
	//               rootless containers might make this complicated.

	// We are relying on the CRIU version RPC which was introduced with CRIU 3.0.0
	if err := c.checkCriuVersion(30000); err != nil {
		return err
	}

	if criuOpts.ImagesDirectory == "" {
		return errors.New("invalid directory to save checkpoint")
	}

	// Since a container can be C/R'ed multiple times,
	// the checkpoint directory may already exist.
	if err := os.Mkdir(criuOpts.ImagesDirectory, 0o700); err != nil && !os.IsExist(err) {
		return err
	}

	imageDir, err := os.Open(criuOpts.ImagesDirectory)
	if err != nil {
		return err
	}
	defer imageDir.Close()

	rpcOpts := criurpc.CriuOpts{
		ImagesDirFd:     proto.Int32(int32(imageDir.Fd())),
		LogLevel:        proto.Int32(4),
		LogFile:         proto.String("dump.log"),
		Root:            proto.String(c.config.Rootfs), // TODO NR:not sure if workingDir is analogous here
		ManageCgroups:   proto.Bool(true),
		NotifyScripts:   proto.Bool(true),
		Pid:             proto.Int32(int32(pid)),
		ShellJob:        proto.Bool(criuOpts.ShellJob),
		LeaveRunning:    proto.Bool(criuOpts.LeaveRunning),
		TcpEstablished:  proto.Bool(criuOpts.TcpEstablished),
		ExtUnixSk:       proto.Bool(criuOpts.ExternalUnixConnections),
		FileLocks:       proto.Bool(criuOpts.FileLocks),
		EmptyNs:         proto.Uint32(criuOpts.EmptyNs),
		OrphanPtsMaster: proto.Bool(true),
		AutoDedup:       proto.Bool(criuOpts.AutoDedup),
		LazyPages:       proto.Bool(criuOpts.LazyPages),
	}

	// if criuOpts.WorkDirectory is not set, criu default is used.
	if criuOpts.WorkDirectory != "" {
		if err := os.Mkdir(criuOpts.WorkDirectory, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		workDir, err := os.Open(criuOpts.WorkDirectory)
		if err != nil {
			return err
		}
		defer workDir.Close()
		rpcOpts.WorkDirFd = proto.Int32(int32(workDir.Fd()))
	}

	// CRIU can use cgroup freezer; when rpcOpts.FreezeCgroup
	// is not set, CRIU uses ptrace() to pause the processes.
	// Note cgroup v2 freezer is only supported since CRIU release 3.14.
	if !cgroups.IsCgroup2UnifiedMode() || c.checkCriuVersion(31400) == nil {
		if fcg := c.cgroupManager.Path("freezer"); fcg != "" {
			rpcOpts.FreezeCgroup = proto.String(fcg)
		}
	}

	// pre-dump may need parentImage param to complete iterative migration
	if criuOpts.ParentImage != "" {
		rpcOpts.ParentImg = proto.String(criuOpts.ParentImage)
		rpcOpts.TrackMem = proto.Bool(true)
	}

	// append optional manage cgroups mode
	if criuOpts.ManageCgroupsMode != 0 {
		mode := criuOpts.ManageCgroupsMode
		rpcOpts.ManageCgroupsMode = &mode
	}

	var t criurpc.CriuReqType
	if criuOpts.PreDump {
		feat := criurpc.CriuFeatures{
			MemTrack: proto.Bool(true),
		}

		if err := c.checkCriuFeatures(criuOpts, &rpcOpts, &feat); err != nil {
			return err
		}

		t = criurpc.CriuReqType_PRE_DUMP
	} else {
		t = criurpc.CriuReqType_DUMP
	}

	req := &criurpc.CriuReq{
		Type: &t,
		Opts: &rpcOpts,
	}

	err = c.criuSwrk(nil, req, criuOpts, nil)
	if err != nil {
		return err
	}
	return nil
}

func (c *RuncContainer) criuSwrk(process *libcontainer.Process, req *criurpc.CriuReq, opts *CriuOpts, extraFiles []*os.File) error {
	fds, err := unix.Socketpair(unix.AF_LOCAL, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}

	var logPath string
	if opts != nil {
		logPath = filepath.Join(opts.WorkDirectory, req.GetOpts().GetLogFile())
	} else {
		// For the VERSION RPC 'opts' is set to 'nil' and therefore
		// opts.WorkDirectory does not exist. Set logPath to "".
		logPath = ""
	}
	criuClient := os.NewFile(uintptr(fds[0]), "criu-transport-client")
	criuClientFileCon, err := net.FileConn(criuClient)
	criuClient.Close()
	if err != nil {
		return err
	}

	criuClientCon := criuClientFileCon.(*net.UnixConn)
	defer criuClientCon.Close()

	criuServer := os.NewFile(uintptr(fds[1]), "criu-transport-server")
	defer criuServer.Close()

	if c.criuVersion != 0 {
		// If the CRIU Version is still '0' then this is probably
		// the initial CRIU run to detect the version. Skip it.
		logrus.Debugf("Using CRIU %d", c.criuVersion)
	}
	cmd := exec.Command("criu", "swrk", "3")
	if process != nil {
		cmd.Stdin = process.Stdin
		cmd.Stdout = process.Stdout
		cmd.Stderr = process.Stderr
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, criuServer)
	if extraFiles != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, extraFiles...)
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	// we close criuServer so that even if CRIU crashes or unexpectedly exits, runc will not hang.
	criuServer.Close()
	// cmd.Process will be replaced by a restored init.
	criuProcess := cmd.Process

	var criuProcessState *os.ProcessState
	defer func() {
		if criuProcessState == nil {
			criuClientCon.Close()
			_, err := criuProcess.Wait()
			if err != nil {
				logrus.Warnf("wait on criuProcess returned %v", err)
			}
		}
	}()

	if err := c.criuApplyCgroups(criuProcess.Pid, req); err != nil {
		return err
	}

	logrus.Debugf("Using CRIU in %s mode", req.GetType().String())
	// In the case of criurpc.CriuReqType_FEATURE_CHECK req.GetOpts()
	// should be empty. For older CRIU versions it still will be
	// available but empty. criurpc.CriuReqType_VERSION actually
	// has no req.GetOpts().
	if logrus.GetLevel() >= logrus.DebugLevel &&
		!(req.GetType() == criurpc.CriuReqType_FEATURE_CHECK ||
			req.GetType() == criurpc.CriuReqType_VERSION) {

		val := reflect.ValueOf(req.GetOpts())
		v := reflect.Indirect(val)
		for i := 0; i < v.NumField(); i++ {
			st := v.Type()
			name := st.Field(i).Name
			if 'A' <= name[0] && name[0] <= 'Z' {
				value := val.MethodByName("Get" + name).Call([]reflect.Value{})
				logrus.Debugf("CRIU option %s with value %v", name, value[0])
			}
		}
	}
	data, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	_, err = criuClientCon.Write(data)
	if err != nil {
		return err
	}

	buf := make([]byte, 10*4096)
	oob := make([]byte, 4096)
	for {
		n, _, _, _, err := criuClientCon.ReadMsgUnix(buf, oob)
		if req.Opts != nil && req.Opts.StatusFd != nil {
			// Close status_fd as soon as we got something back from criu,
			// assuming it has consumed (reopened) it by this time.
			// Otherwise it will might be left open forever and whoever
			// is waiting on it will wait forever.
			fd := int(*req.Opts.StatusFd)
			_ = unix.Close(fd)
			req.Opts.StatusFd = nil
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("unexpected EOF")
		}
		if n == len(buf) {
			return errors.New("buffer is too small")
		}

		resp := new(criurpc.CriuResp)
		err = proto.Unmarshal(buf[:n], resp)
		if err != nil {
			return err
		}
		if !resp.GetSuccess() {
			typeString := req.GetType().String()
			return fmt.Errorf("criu failed: type %s errno %d\nlog file: %s", typeString, resp.GetCrErrno(), logPath)
		}

		t := resp.GetType()
		switch {
		case t == criurpc.CriuReqType_FEATURE_CHECK:
			logrus.Debugf("Feature check says: %s", resp)
			criuFeatures = resp.GetFeatures()
		case t == criurpc.CriuReqType_NOTIFY:
			// removed notify functionality
		case t == criurpc.CriuReqType_RESTORE:
		case t == criurpc.CriuReqType_DUMP:
		case t == criurpc.CriuReqType_PRE_DUMP:
		default:
			return fmt.Errorf("unable to parse the response %s", resp.String())
		}

		break
	}

	_ = criuClientCon.CloseWrite()
	// cmd.Wait() waits cmd.goroutines which are used for proxying file descriptors.
	// Here we want to wait only the CRIU process.
	criuProcessState, err = criuProcess.Wait()
	if err != nil {
		return err
	}

	// In pre-dump mode CRIU is in a loop and waits for
	// the final DUMP command.
	// The current runc pre-dump approach, however, is
	// start criu in PRE_DUMP once for a single pre-dump
	// and not the whole series of pre-dump, pre-dump, ...m, dump
	// If we got the message CriuReqType_PRE_DUMP it means
	// CRIU was successful and we need to forcefully stop CRIU
	if !criuProcessState.Success() && *req.Type != criurpc.CriuReqType_PRE_DUMP {
		return fmt.Errorf("criu failed: %s\nlog file: %s", criuProcessState.String(), logPath)
	}
	return nil
}
