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
}

func (s *Server) RegisterRestoredIP(ctx context.Context, req *multinode.IPReportReq) (*multinode.IPReportResp, error) {
	answerCh := make(chan []*multinode.GlobalMapEntry, 1)
	val, _ := s.pendingMaps.LoadOrStore(config.Global.ClusterID, &clusterWaiters{
		channels: make([]chan []*multinode.GlobalMapEntry, 0),
	})

	waiters := val.(*clusterWaiters)
	waiters.mu.Lock()
	waiters.channels = append(waiters.channels, answerCh)
	waiters.mu.Unlock()

	defer func() {
		waiters.mu.Lock()
		for i, ch := range waiters.channels {
			if ch == answerCh {
				waiters.channels = append(waiters.channels[:i], waiters.channels[i+1:]...)
				break
			}
		}
		if len(waiters.channels) == 0 {
			s.pendingMaps.Delete(config.Global.ClusterID)
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
		return nil, ctx.Err()
	case entries := <-answerCh:
		log.Info().Int("entries", len(entries)).Msg("[multinode] Received global map")

		mappings := make(map[string]string)
		for _, entry := range entries {
			mappings[entry.OriginalIp] = entry.CurrentIp
			//if err := updateEtcHosts(entry, req.Pid); err != nil {
		  //	log.Warn().Err(err).Msgf("[multinode] Failed to update /etc/hosts for %s", entry.PodName)
			//}
		}
		if err := setupMultinodeEBPF(mappings); err != nil {
      log.Error().Err(err).Msg("[multinode] eBPF setup FAILED")
			return &multinode.IPReportResp{
				Success: false,
				Message: fmt.Sprintf("[multinode] eBPF setup failed: %v", err),
			}, nil
		}
		log.Info().Msg("eBPF configured successfully")
	}

	return &multinode.IPReportResp{Success: true}, nil
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
	val, ok := s.pendingMaps.Load(req.ClusterId)
	if !ok {
		return nil, fmt.Errorf("[multinode] no pending restore found for cluster %s", req.ClusterId)
	}

	waiters := val.(*clusterWaiters)
	waiters.mu.Lock()
	defer waiters.mu.Unlock()

	log.Info().
		Str("cluster_id", req.ClusterId).
		Int("waiting_pods", len(waiters.channels)).
		Int("entries", len(req.Entries)).
		Msg("[multinode] Broadcasting global map to all waiting pods")

	for _, ch := range waiters.channels {
		select {
		case ch <- req.Entries:
		default:
			log.Warn().Msg("[multinode] Failed to send to a waiting channel (full)")
		}
	}

	return &multinode.GlobalMapResp{Success: true}, nil
}

func setupMultinodeEBPF(mappings map[string]string) error {
	mappingsJSON, err := json.Marshal(mappings)
  if err != nil {
		log.Error().Err(err).Msg("[multinode] Failed to marshal mappings")
		return fmt.Errorf("failed to marshal mappings: %w", err)
	}
  log.Info().Msgf("[multinode] Marshaled JSON: %s", string(mappingsJSON))
	cmd := exec.Command("multinode-ctl", "setup", "--interface", "eth0", "--clear")
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

func updateEtcHosts(entry *multinode.GlobalMapEntry, containerPID int64) error {
	if launcher := strings.Contains(entry.PodName, "-launcher"); launcher {
		return nil
	}
	jobName := strings.TrimSuffix(entry.PodName, "-worker")
	log.Info().Msgf("[multinode] Job name idenitifed as %s", jobName)

	fqdn := fmt.Sprintf("%s.%s.%s.svc", entry.PodName, jobName, entry.Namespace)
	newLine := fmt.Sprintf("%s\t%s\n", entry.OriginalIp, fqdn)

	cmd := exec.Command("nsenter", "-t", fmt.Sprintf("%d", containerPID), "-m", "--",
		"sh", "-c",
		fmt.Sprintf("echo '%s' >> /etc/hosts", newLine))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("[multinode] nsenter failed: %w, output: %s", err, output)
	}

	log.Info().Msgf("[multinode] Added %s -> %s to /etc/hosts in container %d",
		entry.OriginalIp, fqdn, containerPID)
	return nil
}
