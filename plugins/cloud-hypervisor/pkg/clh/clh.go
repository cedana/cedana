package clh

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	utils "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/utils"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

const (
	sbsPath     = "/run/vc/sbs"
	persistJson = "persist.json"
)

type CloudHypervisorVM struct {
	fdStore sync.Map
}

type SnapshotRequest struct {
	DestinationURL string `json:"destination_url"`
}

func (u *CloudHypervisorVM) Snapshot(destinationURL, vmSocketPath, vmID string) error {
	data := SnapshotRequest{DestinationURL: destinationURL}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal request data: %w", err)
	}

	sbsVMPath := filepath.Join(sbsPath, vmID)

	normalizedDestinationUrl := strings.TrimPrefix(destinationURL, "file://")

	sandboxPath := filepath.Join(normalizedDestinationUrl, "persist")
	if err := os.Mkdir(sandboxPath, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if err := utils.CopyFiltered(sbsVMPath, sandboxPath, sbsVMPath); err != nil {
		return err
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

func (u *CloudHypervisorVM) Restore(snapshotPath, vmSocketPath string, netConfigs []*daemon.RestoredNetConfig) error {

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

func (u *CloudHypervisorVM) GetPID(vmSocketPath string) (uint32, error) {
	// TODO BS unimplemented
	return 0, nil
}

// NewUnixSocketVMSnapshot creates a new UnixSocketVMSnapshot with the given socket path
func NewUnixSocketVMSnapshot(socketPath string) *CloudHypervisorVM {
	return &CloudHypervisorVM{}
}
