package api

// Implements the task service functions for kata container workloads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	task "buf.build/gen/go/cedana/task/protocolbuffers/go"

	"github.com/cedana/cedana/pkg/utils"
	spec "github.com/opencontainers/runtime-spec/specs-go"
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

func (s *service) HostKataRestore(ctx context.Context, args *task.HostRestoreKataArgs) (*task.HostRestoreKataResp, error) {
	isVMSnapshot := args.VMSnapshot
	snapshot := args.VMSnapshotPath
	socketPath := args.VMSocketPath

	if isVMSnapshot {
		err := s.vmSnapshotter.Restore(snapshot, socketPath)
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
	Restore(snapshotPath, vmSocketPath string) error
	Pause(vmSocketPath string) error
	Resume(vmSocketPath string) error
}

type SnapshotRequest struct {
	DestinationURL string `json:"destination_url"`
}

type CloudHypervisorVM struct {
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

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("error snapshotting vm: %d, %v, req data: %v", resp.StatusCode, resp.Body, data)
	}

	return nil
}

type RestoreConfig struct {
	SourceURL string               `json:"source_url"`
	Prefault  bool                 `json:"prefault"`
	NetFds    *[]RestoredNetConfig `json:"net_fds,omitempty"`
}

type RestoredNetConfig struct {
	Fds []int `json:"fds"`
}

func (u *CloudHypervisorVM) Restore(snapshotPath, vmSocketPath string) error {
	data := RestoreConfig{SourceURL: snapshotPath, Prefault: true}
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

	req, err := http.NewRequest("PUT", "http://localhost/api/v1/vm.restore", bytes.NewBuffer(jsonData))

	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("error restoring vm: %d, %v, req data: %v, socket path: %s", resp.StatusCode, string(body), data, vmSocketPath)
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
