package api

// Implements the task service functions for kata container workloads

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	task "buf.build/gen/go/cedana/task/protocolbuffers/go"

	"github.com/cedana/cedana/pkg/api/kata"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/mdlayher/vsock"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/xid"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// const (
// 	KATA_OUTPUT_FILE_PATH  string      = "/tmp/log/cedana-output.log"
// 	KATA_OUTPUT_FILE_PERMS os.FileMode = 0o777
// 	KATA_OUTPUT_FILE_FLAGS int         = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
// )

var ERR_NO_KATA_CONTAINERS_FOUND = fmt.Errorf("No kata containers found!")

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

// Cedana KataDump function that lives in Kata VM
func (s *service) KataDump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	var err error

	state := &task.ProcessState{}
	pids, err := findPidFromCgroups()
	if err != nil && err != ERR_NO_KATA_CONTAINERS_FOUND {
		return nil, err
	}
	if err == ERR_NO_KATA_CONTAINERS_FOUND {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	if len(pids) > 1 {
		return nil, fmt.Errorf("Too many kata containers, I don't know what to do for this yet!")
	}

	// more than 1 kata container in same vm is not yet implemented
	state, err = s.generateState(ctx, pids[0])
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}

	state.JID = args.JID

	dumpStats := task.DumpStats{
		DumpType: task.DumpType_KATA,
	}
	ctx = context.WithValue(ctx, utils.DumpStatsKey, &dumpStats)

	err = s.kataDump(ctx, state, args)
	if err != nil {
		st := status.New(codes.Internal, err.Error())
		return nil, st.Err()
	}

	resp := task.DumpResp{
		Message:      fmt.Sprintf("Dumped process %d to %s", pids[0], args.Dir),
		CheckpointID: state.CheckpointPath, // XXX: Just return path for ID for now
		State:        state,
	}

	return &resp, err
}

func (s *service) handleConnection(conn net.Conn, socketPath string) {
	defer conn.Close()

	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		log.Logger.Warn().Msgf("Not a Unix connection")
		return
	}

	// Read file descriptors
	oob := make([]byte, syscall.CmsgSpace(4*4)) // Space for up to 4 FDs
	buf := make([]byte, 1024)
	n, oobn, _, _, err := unixConn.ReadMsgUnix(buf, oob)
	if err != nil {
		log.Logger.Warn().Msgf("Failed to read message: %v\n", err)
		return
	}

	cmsgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		log.Logger.Warn().Msgf("Failed to parse control messages: %v\n", err)
		return
	}

	var fds []int
	for _, cmsg := range cmsgs {
		fdArr, err := syscall.ParseUnixRights(&cmsg)
		if err != nil {
			log.Logger.Warn().Msgf("Failed to parse Unix rights: %v\n", err)
			return
		}
		fds = append(fds, fdArr...)
	}

	log.Logger.Debug().Msgf("Received FDs: %v, message: %s\n", fds, string(buf[:n]))

	requestID := string(buf[:n]) // Assume request ID is sent with the message
	s.fdStore.Store(requestID, fds)

	defer os.Remove(socketPath)

}

func (s *service) CreateUnixSocket(ctx context.Context, _ *task.Empty) (*task.SocketResp, error) {
	tempDir := os.TempDir()
	socketPath := filepath.Join(tempDir, fmt.Sprintf("ced_fdsock_%d.sock", os.Getpid()))
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create Unix socket: %w", err)
	}

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				fmt.Printf("Connection error: %v\n", err)
				break
			}

			go s.handleConnection(conn, socketPath)
		}
	}()

	return &task.SocketResp{SocketPath: socketPath}, nil
}

func (s *service) HostKataRestore(ctx context.Context, args *task.HostRestoreKataArgs) (*task.HostRestoreKataResp, error) {
	isVMSnapshot := args.GetVMSnapshot()
	snapshot := args.GetVMSnapshotPath()
	socketPath := args.GetVMSocketPath()
	restoredNetConfig := args.GetRestoredNetConfig()

	var netFds []int
	var netFdsInt64 []int64
	s.fdStore.Range(func(key, value any) bool {
		requestID := key.(string) // Adjust the type to match the actual key type
		netFds = value.([]int)    // Adjust the type to match the actual value type

		log.Logger.Info().Msgf("Request ID: %v, FDs: %v\n", requestID, netFds)

		return true
	})

	netFdsInt64 = make([]int64, len(netFds))
	for i, fd := range netFds {
		netFdsInt64[i] = int64(fd)
	}

	restoredNetConfig[0].Fds = netFdsInt64

	if isVMSnapshot {
		err := s.vmSnapshotter.Restore(snapshot, socketPath, restoredNetConfig)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Restore task failed during vmSnapshotter Restore: %v", err)
		}

		err = s.vmSnapshotter.Resume(socketPath)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Restore task failed during vmSnapshotter Restore: %v", err)
		}

		return &task.HostRestoreKataResp{State: "restored"}, nil
	}
	return &task.HostRestoreKataResp{State: "invalid args"}, nil
}

func (s *service) KataRestore(ctx context.Context, args *task.RestoreArgs) (*task.RestoreResp, error) {
	var resp task.RestoreResp
	var pid *int32
	var err error

	if args.CheckpointPath == "" {
		return nil, status.Error(codes.InvalidArgument, "checkpoint path cannot be empty")
	}

	restoreStats := task.RestoreStats{
		DumpType: task.DumpType_KATA,
	}
	ctx = context.WithValue(ctx, utils.RestoreStatsKey, &restoreStats)

	pid, err = s.kataRestore(ctx, args)
	if err != nil {
		staterr := status.Error(codes.Internal, fmt.Sprintf("failed to restore process: %v", err))
		return nil, staterr
	}

	state, err := s.generateState(ctx, *pid)
	if err != nil {
		log.Warn().Err(err).Msg("failed to generate state after restore")
	}

	resp = task.RestoreResp{
		Message: fmt.Sprintf("successfully restored process: %v", *pid),
		State:   state,
	}

	resp.State = state

	return &resp, nil
}

type VMSnapshot interface {
	Snapshot(destinationURL, vmSocketPath string) error
	Restore(snapshotPath, vmSocketPath string, netConfigs []*task.RestoredNetConfig) error
	Pause(vmSocketPath string) error
	Resume(vmSocketPath string) error
}

type SnapshotRequest struct {
	DestinationURL string `json:"destination_url"`
}

type CloudHypervisorVM struct {
	fdStore sync.Map
}

func (u *CloudHypervisorVM) Snapshot(destinationURL, vmSocketPath string) error {
	data := SnapshotRequest{DestinationURL: destinationURL}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal request data: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", vmSocketPath)
			},
		},
	}

	req, err := http.NewRequest("PUT", "http://localhost/api/v1/vm.snapshot", bytes.NewBuffer(jsonData))

	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("error snapshotting vm: %d, %v, req data: %v", resp.StatusCode, string(respBody), data)
	}

	return nil
}

type RestoreConfig struct {
	SourceURL string              `json:"source_url"`
	Prefault  bool                `json:"prefault"`
	NetFDs    []RestoredNetConfig `json:"net_fds,omitempty"`
}

type RestoredNetConfig struct {
	ID     string  `json:"id"`
	NumFDs int64   `json:"num_fds"`
	Fds    []int64 `json:"fds,omitempty"`
}

func (u *CloudHypervisorVM) Restore(snapshotPath, vmSocketPath string, netConfigs []*task.RestoredNetConfig) error {

	var clhNetConfigs []RestoredNetConfig

	for _, netCfg := range netConfigs {
		clhNetConfig := RestoredNetConfig{
			ID:     netCfg.GetID(),
			NumFDs: netCfg.GetNumFDs(),
			Fds:    netCfg.GetFds(),
		}

		clhNetConfigs = append(clhNetConfigs, clhNetConfig)
	}

	data := RestoreConfig{SourceURL: snapshotPath, Prefault: true, NetFDs: clhNetConfigs}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal request data: %w", err)
	}

	timeout := time.Minute * 20

	netDeviceAsIoReader := bytes.NewBuffer(jsonData)

	addr, err := net.ResolveUnixAddr("unix", vmSocketPath)
	if err != nil {
		return err
	}

	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	req, err := http.NewRequest(http.MethodPut, "http://localhost/api/v1/vm.restore", netDeviceAsIoReader)

	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", strconv.Itoa(int(netDeviceAsIoReader.Len())))

	payload, err := httputil.DumpRequest(req, true)
	if err != nil {
		return err
	}

	// This is for closing the open fds after restore has finished
	var files []*os.File
	defer func() {
		for _, file := range files {
			file.Close()
		}
	}()

	var fds []int
	for _, fd := range data.NetFDs[0].Fds {
		fds = append(fds, int(fd))

		file := os.NewFile(uintptr(fd), fmt.Sprintf("fd-%d", fd))
		if file != nil {
			files = append(files, file)
		}
	}

	oob := syscall.UnixRights(fds...)
	payloadn, oobn, err := conn.WriteMsgUnix([]byte(payload), oob, nil)
	if err != nil {
		return err
	}
	if payloadn != len(payload) || oobn != len(oob) {
		return fmt.Errorf("Failed to send all the request to Cloud Hypervisor. %d bytes expect to send as payload, %d bytes expect to send as oob date,  but only %d sent as payload, and %d sent as oob", len(payload), len(oob), payloadn, oobn)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		return err
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewBuffer(respBody))

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("error restoring vm: %d, %v, req data: %v, socket path: %s", resp.StatusCode, string(respBody), string(jsonData), vmSocketPath)
	}

	return nil
}

func (u *CloudHypervisorVM) Pause(vmSocketPath string) error {
	var jsonData []byte

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", vmSocketPath)
			},
		},
	}

	req, err := http.NewRequest("PUT", "http://localhost/api/v1/vm.pause", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create pause request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute pause request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("error pausing VM: %d, %v", resp.StatusCode, resp.Body)
	}

	return nil
}

// Resume implements the Resume method of the VMSnapshot interface
func (u *CloudHypervisorVM) Resume(vmSocketPath string) error {

	var jsonData []byte

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", vmSocketPath)
			},
		},
	}

	req, err := http.NewRequest("PUT", "http://localhost/api/v1/vm.resume", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create resume request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute resume request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("error resuming VM: %d, %v", resp.StatusCode, resp.Body)
	}
	return nil
}

// NewUnixSocketVMSnapshot creates a new UnixSocketVMSnapshot with the given socket path
func NewUnixSocketVMSnapshot(socketPath string) *CloudHypervisorVM {
	return &CloudHypervisorVM{}
}

//////////////////////////
///// Kata Utils //////
//////////////////////////

func childPidFromPPid(ppid int32) (int32, error) {
	// Replace PID with the actual parent process ID

	// Run the pgrep command
	cmd := exec.Command("pgrep", "--parent", strconv.Itoa(int(ppid)))
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return -1, err
	}

	// Get the first line of the output
	pgrepOutput := strings.TrimSpace(out.String())
	lines := strings.Split(pgrepOutput, "\n")
	if len(lines) == 0 {
		return -1, errors.New("No Child found")
	}
	firstLine := lines[0]

	// Convert the first line to an integer (PID of the first child process)
	firstChildPID, err := strconv.Atoi(firstLine)
	if err != nil {
		return -1, err
	}

	return int32(firstChildPID), nil
}

func findAllExternalBindMounts() ([][]string, error) {
	allExternalMounts := [][]string{}

	pattern := "/run/kata-containers/*/config.json"
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get config.json files: %w", err)
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", file, err)
		}

		var ociSpec spec.Spec
		if err := json.Unmarshal(data, &ociSpec); err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON from %s: %w", file, err)
		}

		// Skip non-container kata containers
		if ociSpec.Annotations["io.kubernetes.cri.container-type"] != "container" {
			continue
		}

		fileMounts := []string{}

		for _, m := range ociSpec.Mounts {
			if mountIsBind(m) {
				fileMounts = append(fileMounts, fmt.Sprintf("mnt[%s]:%s", m.Destination, m.Destination))
			}
		}

		allExternalMounts = append(allExternalMounts, fileMounts)
	}

	return allExternalMounts, nil
}

func mountIsBind(m spec.Mount) bool {
	for _, o := range m.Options {
		if o == "rbind" {
			return true
		}
	}

	return false
}

func findPidFromCgroups() ([]int32, error) {
	var pids = []int32{}

	pattern := "/run/kata-containers/*/config.json"
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get config.json files: %w", err)
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", file, err)
		}

		var ociSpec spec.Spec
		if err := json.Unmarshal(data, &ociSpec); err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON from %s: %w", file, err)
		}

		// skip non-container kata containers
		if ociSpec.Annotations["io.kubernetes.cri.container-type"] != "container" {
			continue
		}

		parts := strings.Split(ociSpec.Linux.CgroupsPath, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid input format, expected 'slice:cri-containerd:containerID', got: %s with %s parts", ociSpec.Linux.CgroupsPath, len(parts))
		}

		slice := parts[0]
		containerID := parts[2]

		// TODO BS this could be different for different types of cgroups, need to parse
		// Linux.CgroupsPath properly
		cgroupPath := fmt.Sprintf("/sys/fs/cgroup/kubepods.slice/kubepods-besteffort.slice/%s/cri-containerd-%s.scope/cgroup.procs", slice, containerID)

		pidFromFile, err := os.ReadFile(cgroupPath)
		if err != nil {
			return nil, err
		}

		pidFromFileTrimmed := strings.TrimSpace(string(pidFromFile))

		pid, err := strconv.ParseInt(pidFromFileTrimmed, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to convert content to int32: %w", err)
		}

		pids = append(pids, int32(pid))

	}

	if len(pids) == 0 {
		return pids, ERR_NO_KATA_CONTAINERS_FOUND
	}

	return pids, nil
}
