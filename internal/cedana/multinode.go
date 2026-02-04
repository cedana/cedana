package cedana

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
  "sync"

  "github.com/cedana/cedana/pkg/config"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/rs/zerolog/log"
)

type GlobalMapEntry struct {
	OriginalIP string `json:"original_ip"`
	CurrentIP  string `json:"current_ip"`
	PodName    string `json:"pod_name"`
	Namespace  string `json:"namespace"`
}

type clusterWaiters struct {
  mu sync.Mutex
  channels []chan []GlobalMapEntry
}

func (s *Server) ReportIPMapping(ctx context.Context, req *daemon.IPReportReq) (*daemon.IPReportResp, error) {
	answerCh := make(chan []GlobalMapEntry, 1)
  val, _ := s.pendingMaps.LoadOrStore(config.Global.ClusterID, &clusterWaiters{
    channels: make([]chan []GlobalMapEntry, 0),
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
			mappings[entry.OriginalIP] = entry.CurrentIP
			if err := updateEtcHosts(entry); err != nil {
				log.Warn().Err(err).Msgf("[multinode] Failed to update /etc/hosts for %s", entry.PodName)
			}
		}
		if err := setupMultinodeEBPF(mappings); err != nil {
			return &daemon.IPReportResp{
				Success: false,
				Message: fmt.Sprintf("[multinode] eBPF setup failed: %v", err),
			}, nil
		}
    log.Info().Msg("eBPF configured successfully")
	}

	return &daemon.IPReportResp{Success: true}, nil
}

func (s *Server) MonitorIPEvents(_ *daemon.MonitorIPEventsReq, stream daemon.Daemon_MonitorIPEventsServer) error {
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

func (s *Server) SubmitGlobalMap(ctx context.Context, req *daemon.GlobalMapReq) (*daemon.GlobalMapResp, error) {
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

	return &daemon.GlobalMapResp{Success: true}, nil
}

func setupMultinodeEBPF(mappings map[string]string) error {
	mappingsJSON, _ := json.Marshal(mappings)
	cmd := exec.Command("multinode-ctl", "setup", "--interface", "eth0", "--clear")
	cmd.Stdin = bytes.NewReader(mappingsJSON)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("multinode-ctl failed: %w, output: %s", err, output)
	}
	log.Info().Msgf("eBPF configured with %d mappings", len(mappings))
	return nil
}

func updateEtcHosts(entry GlobalMapEntry) error {
	hostsPath := "/etc/hosts"

  if launcher := strings.Contains(entry.PodName, "-launcher"); launcher {
    return nil
  }
  jobName := strings.TrimSuffix(entry.PodName, "-worker")
  log.Info().Msgf("[multinode] Job name idenitifed as ", jobName)

	fqdn := fmt.Sprintf("%s.%s.%s.svc", entry.PodName, jobName, entry.Namespace)
	newLine := fmt.Sprintf("%s\t%s\n", entry.OriginalIP, fqdn)

	f, err := os.OpenFile(hostsPath, os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("[multinode] failed to open hosts file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), fqdn) || strings.Contains(scanner.Text(), entry.OriginalIP) {
			log.Debug().Msgf("[multinode] Hosts entry for %s already exists, skipping", fqdn)
			return nil
		}
	}

	if _, err := f.WriteString(newLine); err != nil {
		return fmt.Errorf("[multinode] failed to write to hosts file: %v", err)
	}

	log.Info().Msgf("[multinode] Added %s -> %s to /etc/hosts", entry.OriginalIP, fqdn)
	return nil
}
