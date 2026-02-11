package cedana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"buf.build/gen/go/cedana/cedana/grpc/go/daemon/daemongrpc"
	multinode "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/multinode"
	"github.com/cedana/cedana/pkg/config"
	"github.com/rs/zerolog/log"
)

type clusterWaiters struct {
	mu       sync.Mutex
	channels []chan []*multinode.GlobalMapEntry
	pids     []int64
}

func (s *Server) RegisterRestoredIP(ctx context.Context, req *multinode.IPReportReq) (*multinode.IPReportResp, error) {
	answerCh := make(chan []*multinode.GlobalMapEntry, 1)
	val, _ := s.pendingMaps.LoadOrStore(config.Global.ClusterID, &clusterWaiters{
		channels: make([]chan []*multinode.GlobalMapEntry, 0),
		pids:     make([]int64, 0),
	})

	waiters := val.(*clusterWaiters)
	waiters.mu.Lock()
	waiters.channels = append(waiters.channels, answerCh)
	waiters.pids = append(waiters.pids, req.Pid) // race fix -> arriving map can still trigger eBPF
	waiters.mu.Unlock()

	defer func() {
		waiters.mu.Lock()
		for i, ch := range waiters.channels {
			if ch == answerCh {
				waiters.channels = append(waiters.channels[:i], waiters.channels[i+1:]...)
				break
			}
		}
		waiters.mu.Unlock()
	}()

	select {
	case s.ipEventCh <- req:
	default:
		return nil, fmt.Errorf("[multinode] helper not connected")
	}

	log.Info().Str("checkpoint_id", req.CheckpointId).Msg("[multinode] Waiting for Global Map from Propagator...")

	select {
	case <-ctx.Done():
		log.Warn().Str("checkpoint_id", req.CheckpointId).Msg("[multinode] gRPC context canceled, eBPF/hosts will be applied via SubmitGlobalMap")
		return &multinode.IPReportResp{Success: true}, nil
	case <-answerCh:
		log.Info().Msg("[multinode] Successfully completed setup for pod")
		return &multinode.IPReportResp{Success: true}, nil
	}
}

func (s *Server) MonitorIPEvents(_ *multinode.MonitorIPEventsReq, stream daemongrpc.Daemon_MonitorIPEventsServer) error {
	log.Info().Msg("[multinode] Helper connected to IP Event Monitor")

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case req := <-s.ipEventCh:
			if err := stream.Send(req); err != nil {
				log.Error().Err(err).Msg("[multinode] Failed to send IP event to helper")
				return err
			}
		}
	}
}

func (s *Server) SubmitGlobalMap(ctx context.Context, req *multinode.GlobalMapReq) (*multinode.GlobalMapResp, error) {
	val, ok := s.pendingMaps.Load(config.Global.ClusterID)
	if !ok {
		return nil, fmt.Errorf("[multinode] no pending restore found for cluster %s", config.Global.ClusterID)
	}

	waiters := val.(*clusterWaiters)
	waiters.mu.Lock()
	defer waiters.mu.Unlock()

	log.Info().
		Str("cluster_id", config.Global.ClusterID).
		Int("pids_to_update", len(waiters.pids)).
		Msg("[multinode] Global Map arrived. Configuring system...")

	mappings := make(map[string]string)
	for _, entry := range req.Entries {
		mappings[entry.OriginalIp] = entry.CurrentIp
	}

	for _, pid := range waiters.pids { // even if the gRPC call dies, PIDs are in this list
		if err := setupMultinodeEBPF(mappings, pid); err != nil {
			log.Error().Err(err).Msg("[multinode] eBPF setup FAILED")
		}
		for _, entry := range req.Entries {
			if err := updateEtcHosts(entry, pid); err != nil {
				log.Warn().Err(err).Int64("pid", pid).Msg("[multinode] Failed late update of /etc/hosts")
			}
		}
	}

	for _, ch := range waiters.channels {
		select {
		case ch <- req.Entries:
		default:
		}
	}

	s.pendingMaps.Delete(config.Global.ClusterID)

	return &multinode.GlobalMapResp{Success: true}, nil
}

func setupMultinodeEBPF(mappings map[string]string, containerPID int64) error {
	mappingsJSON, err := json.Marshal(mappings)
	if err != nil {
		log.Error().Err(err).Msg("[multinode] Failed to marshal mappings")
		return fmt.Errorf("failed to marshal mappings: %w", err)
	}
	log.Info().Msgf("[multinode] Marshaled JSON: %s", string(mappingsJSON))
	cmd := exec.Command("multinode-ctl", "setup", "--interface", "eth0", "--pid", fmt.Sprintf("%d", containerPID), "--clear")
	cmd.Stdin = bytes.NewReader(mappingsJSON)
	log.Info().Msg("[multinode] Executing multinode-ctl command...")
	output, err := cmd.CombinedOutput()
	log.Info().Msgf("[multinode] multinode-ctl output: %s", string(output))
	if err != nil {
		log.Error().Err(err).Msgf("[multinode] multinode-ctl command failed: %s", string(output))
		return fmt.Errorf("multinode-ctl failed: %w, output: %s", err, output)
	}
	log.Info().Msgf("eBPF configured with %d mappings", len(mappings))
	return nil
}

/////////////////
//// Helpers ////
////////////////

func updateEtcHosts(entry *multinode.GlobalMapEntry, containerPID int64) error {
	baseName := entry.PodName
	for _, suffix := range []string{"-worker", "-launcher"} {
		if idx := strings.LastIndex(baseName, suffix); idx != -1 {
			baseName = baseName[:idx]
			break
		}
	}
	fqdn := fmt.Sprintf("%s.%s.%s.svc", entry.PodName, baseName, entry.Namespace)
	newLine := fmt.Sprintf("%s\t%s", entry.OriginalIp, fqdn)

	script := fmt.Sprintf("grep -qF '%s' /etc/hosts || printf '%%s\\n' '%s' >> /etc/hosts", fqdn, newLine)

	log.Info().
		Int64("pid", containerPID).
		Str("pod", entry.PodName).
		Str("fqdn", fqdn).
		Msg("[multinode] Injecting host entry")

	cmd := exec.Command("nsenter", "-t", fmt.Sprintf("%d", containerPID), "-m", "-u", "--",
		"/bin/sh", "-c", script)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nsenter failed (PID %d): %w, output: %s", containerPID, err, output)
	}

	return nil
}
