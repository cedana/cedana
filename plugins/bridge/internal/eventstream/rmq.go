package eventstream

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	cedanagosdk "github.com/cedana/cedana-go-sdk"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/profiling"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
	"github.com/wagslane/go-rabbitmq"
	"google.golang.org/protobuf/proto"
)

type EventStream struct {
	cedana     *client.Client
	propagator *cedanagosdk.ApiClient

	url                string
	checkpoints        *rabbitmq.Publisher
	checkpointRequests *rabbitmq.Consumer
	lifecycleMu        sync.RWMutex
	closeOnce          sync.Once
	closeErr           error
	*rabbitmq.Conn
}

var queryExpiryMs = 30 * time.Minute.Milliseconds()

func New(ctx context.Context, cedana *client.Client, propagator *cedanagosdk.ApiClient) (*EventStream, error) {
	if cedana == nil {
		return nil, fmt.Errorf("cedana client is nil")
	}
	if propagator == nil {
		return nil, fmt.Errorf("propagator client is nil")
	}
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	clientName := fmt.Sprintf("cedana-bridge-%s-%d", hostname, time.Now().UnixNano())
	url, err := propagator.V2().Discover().ByName("rabbitmq").Get(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to discover rabbitmq service: %v", err)
	}
	conn, err := rabbitmq.NewConn(
		*url,
		rabbitmq.WithConnectionOptionsConfig(
			rabbitmq.Config{
				Properties: amqp.Table{
					"connection_name": clientName,
				},
			},
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to rmq: %v", err)
	}
	es := &EventStream{
		cedana:     cedana,
		propagator: propagator,
		url:        *url,
		Conn:       conn,
	}
	return es, nil
}

func (es *EventStream) StartCheckpointsConsumer(ctx context.Context) error {
	es.lifecycleMu.RLock()
	conn := es.Conn
	es.lifecycleMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("rabbitmq connection is closed")
	}

	queueName := "cedana_bridge-" + rand.Text()
	consumer, err := rabbitmq.NewConsumer(
		conn,
		queueName,
		rabbitmq.WithConsumerOptionsExchangeName("bridge_broadcast_request"),
		rabbitmq.WithConsumerOptionsConcurrency(10),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsExchangeKind("fanout"),
		rabbitmq.WithConsumerOptionsConsumerName("cedana_bridge"),
		rabbitmq.WithConsumerOptionsRoutingKey(""),
		rabbitmq.WithConsumerOptionsQueueExclusive,
		rabbitmq.WithConsumerOptionsQueueAutoDelete,
		rabbitmq.WithConsumerOptionsQueueArgs(rabbitmq.Table{
			"x-expires": queryExpiryMs,
		}),
		rabbitmq.WithConsumerOptionsBinding(rabbitmq.Binding{
			RoutingKey:     "",
			BindingOptions: rabbitmq.BindingOptions{},
		}),
	)
	if err != nil {
		return err
	}

	es.lifecycleMu.Lock()
	if es.Conn == nil {
		es.lifecycleMu.Unlock()
		consumer.Close()
		return fmt.Errorf("rabbitmq connection is closed")
	}
	if es.checkpointRequests != nil {
		es.lifecycleMu.Unlock()
		consumer.Close()
		return fmt.Errorf("checkpoints consumer is already running")
	}
	es.checkpointRequests = consumer
	es.lifecycleMu.Unlock()

	defer func() {
		es.lifecycleMu.Lock()
		if es.checkpointRequests == consumer {
			es.checkpointRequests = nil
		}
		es.lifecycleMu.Unlock()
	}()

	if err := consumer.Run(es.checkpointHandler(ctx)); err != nil {
		consumer.Close()
		return err
	}
	return nil
}

func (es *EventStream) StartCheckpointsPublisher(ctx context.Context) error {
	es.lifecycleMu.RLock()
	conn := es.Conn
	es.lifecycleMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("rabbitmq connection is closed")
	}

	publisher, err := rabbitmq.NewPublisher(
		conn,
	)
	if err != nil {
		return err
	}

	es.lifecycleMu.Lock()
	defer es.lifecycleMu.Unlock()
	if es.Conn == nil {
		publisher.Close()
		return fmt.Errorf("rabbitmq connection is closed")
	}
	if es.checkpoints != nil {
		publisher.Close()
		return fmt.Errorf("checkpoints publisher is already running")
	}
	es.checkpoints = publisher
	return nil
}

func (es *EventStream) StartRestoresConsumer(ctx context.Context) error {
	es.lifecycleMu.RLock()
	conn := es.Conn
	es.lifecycleMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("rabbitmq connection is closed")
	}

	queueName := "cedana_bridge_restore-" + rand.Text()
	consumer, err := rabbitmq.NewConsumer(
		conn,
		queueName,
		rabbitmq.WithConsumerOptionsExchangeName("bridge_restore_request"),
		rabbitmq.WithConsumerOptionsConcurrency(10),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsExchangeKind("fanout"),
		rabbitmq.WithConsumerOptionsConsumerName("cedana_bridge_restore"),
		rabbitmq.WithConsumerOptionsRoutingKey(""),
		rabbitmq.WithConsumerOptionsQueueExclusive,
		rabbitmq.WithConsumerOptionsQueueAutoDelete,
		rabbitmq.WithConsumerOptionsQueueArgs(rabbitmq.Table{
			"x-expires": queryExpiryMs,
		}),
		rabbitmq.WithConsumerOptionsBinding(rabbitmq.Binding{
			RoutingKey:     "",
			BindingOptions: rabbitmq.BindingOptions{},
		}),
	)
	if err != nil {
		return err
	}

	es.lifecycleMu.Lock()
	if es.Conn == nil {
		es.lifecycleMu.Unlock()
		consumer.Close()
		return fmt.Errorf("rabbitmq connection is closed")
	}
	es.lifecycleMu.Unlock()

	if err := consumer.Run(es.restoreHandler(ctx)); err != nil {
		consumer.Close()
		return err
	}
	return nil
}

func (es *EventStream) Close() error {
	es.closeOnce.Do(func() {
		es.lifecycleMu.Lock()
		consumer := es.checkpointRequests
		publisher := es.checkpoints
		conn := es.Conn
		es.checkpointRequests = nil
		es.checkpoints = nil
		es.Conn = nil
		es.lifecycleMu.Unlock()

		if consumer != nil {
			consumer.Close()
		}
		if publisher != nil {
			publisher.Close()
		}
		if conn != nil {
			if err := conn.Close(); err != nil {
				es.closeErr = errors.Join(es.closeErr, fmt.Errorf("failed to close rabbitmq connection: %w", err))
			}
		}
	})

	return es.closeErr
}

/////////////
// Helpers //
/////////////

type checkpointReq struct {
	JobID    string `json:"job_id"`
	JobName  string `json:"job_name"`
	ActionID string `json:"action_id"`
	Kind     string `json:"kind"`
}

type checkpointInfo struct {
	ActionID      string        `json:"action_id"`
	JobID         string        `json:"job_id"`
	CheckpointID  string        `json:"checkpoint_id"`
	Status        string        `json:"status"`
	Path          string        `json:"path"`
	GPU           bool          `json:"gpu"`
	Platform      string        `json:"platform"`
	ProfilingInfo profilingInfo `json:"profiling_info"`
}

type profilingInfo struct {
	Raw           *profiling.Data `json:"raw"`
	TotalDuration int64           `json:"total_duration"`
	TotalIO       int64           `json:"total_io"`
}

type restoreReq struct {
	ActionID       string `json:"action_id"`
	CheckpointID   string `json:"checkpoint_id"`
	CheckpointPath string `json:"checkpoint_path"`
	JobID          string `json:"job_id"`
	ClusterID      string `json:"cluster_id"`
}

type restoreInfo struct {
	ActionID     string `json:"action_id"`
	Status       string `json:"status"`
	CheckpointID string `json:"checkpoint_id"`
	Error        string `json:"error,omitempty"`
}

func (es *EventStream) restoreHandler(ctx context.Context) rabbitmq.Handler {
	return func(msg rabbitmq.Delivery) rabbitmq.Action {
		log.Trace().Msgf("received restore request: %s", string(msg.Body))

		var req restoreReq
		if err := json.Unmarshal(msg.Body, &req); err != nil {
			log.Error().Err(err).Msg("failed to unmarshal restore request")
			return rabbitmq.Ack
		}
		log := log.With().Str("action_id", req.ActionID).Str("checkpoint_id", req.CheckpointID).Str("job_id", req.JobID).Str("checkpoint_path", req.CheckpointPath).Logger()

		if req.CheckpointPath == "" {
			err := fmt.Errorf("missing checkpoint_path in restore request")
			log.Error().Err(err).Msg("failed to restore job")
			if publishErr := es.publishRestore(ctx, req, err); publishErr != nil {
				log.Error().Err(publishErr).Msg("failed to publish restore status")
			}
			return rabbitmq.Ack
		}

		restoreReq := &daemon.RestoreReq{
			Path: req.CheckpointPath,
			Type: "process",
		}
		if req.JobID != "" {
			restoreReq.Details = &daemon.Details{JID: proto.String(req.JobID)}
		} else {
			log.Warn().Msg("restore request missing job_id; restoring as unmanaged process")
		}

		// Execute restore
		restoreResp, _, err := es.cedana.Restore(ctx, restoreReq)

		if publishErr := es.publishRestore(ctx, req, err); publishErr != nil {
			log.Error().Err(publishErr).Msg("failed to publish restore status")
		}

		if err != nil {
			log.Error().Err(err).Msg("failed to restore job")
			return rabbitmq.Ack
		}

		log.Info().Msgf("Restore response: %v", restoreResp)
		return rabbitmq.Ack
	}
}

func (es *EventStream) publishRestore(
	ctx context.Context,
	req restoreReq,
	restoreErr error,
) error {
	es.lifecycleMu.RLock()
	publisher := es.checkpoints
	es.lifecycleMu.RUnlock()
	if publisher == nil {
		return fmt.Errorf("checkpoints publisher is not initialized")
	}

	ri := restoreInfo{
		ActionID:     req.ActionID,
		CheckpointID: req.CheckpointID,
	}
	if restoreErr != nil {
		ri.Status = "error"
		ri.Error = restoreErr.Error()
	} else {
		ri.Status = "success"
	}

	data, err := json.Marshal(ri)
	if err != nil {
		return err
	}
	err = publisher.Publish(
		data,
		[]string{"bridge_restore_response"},
		rabbitmq.WithPublishOptionsExchange("bridge_restore_response"),
	)
	if err != nil {
		return err
	}
	if restoreErr != nil {
		log.Error().Err(restoreErr).Msg("restore published with error")
	} else {
		log.Info().Msg("restore published successfully")
	}
	return nil
}

func (es *EventStream) checkpointHandler(ctx context.Context) rabbitmq.Handler {
	return func(msg rabbitmq.Delivery) rabbitmq.Action {
		log.Trace().Msgf("received checkpoint request: %s", string(msg.Body))

		var req checkpointReq
		if err := json.Unmarshal(msg.Body, &req); err != nil {
			log.Error().Err(err).Msg("failed to unmarshal checkpoint request")
			return rabbitmq.Ack
		}
		log := log.With().Str("action_id", req.ActionID).Str("kind", req.Kind).Str("job_id", req.JobID).Logger()

		// List local jobs and find the one matching the requested job ID
		listResp, err := es.cedana.List(ctx, &daemon.ListReq{})
		if err != nil {
			log.Error().Err(err).Msg("failed to list jobs")
			return rabbitmq.Ack
		}

		var job *daemon.Job
		for _, j := range listResp.Jobs {
			if j.JID == req.JobID {
				job = j
				break
			}
		}
		if job == nil {
			log.Debug().Msg("job not found on this node, skipping checkpoint request")
			return rabbitmq.Ack
		}

		log.Info().Uint32("pid", job.GetState().GetPID()).Msg("found job to checkpoint")

		// Create checkpoint ID via propagator
		checkpointID, err := es.propagator.V2().Checkpoints().Post(ctx, nil)
		if err != nil {
			log.Error().Err(err).Msg("failed to create checkpoint in propagator")
			return rabbitmq.Ack
		}

		// Build dump request
		dumpReq := &daemon.DumpReq{
			Name: *checkpointID,
			Details: &daemon.Details{
				JID: proto.String(req.JobID),
			},
		}

		// Execute dump
		dumpResp, profilingData, err := es.cedana.Dump(ctx, dumpReq)
		var path string
		if err == nil && len(dumpResp.Paths) > 0 {
			path = dumpResp.Paths[0]
		}

		es.publishCheckpoint(ctx, req, *checkpointID, profilingData, path, dumpResp, err)

		return rabbitmq.Ack
	}
}

func (es *EventStream) publishCheckpoint(
	ctx context.Context,
	req checkpointReq,
	checkpointID string,
	profilingData *profiling.Data,
	path string,
	dumpResp *daemon.DumpResp,
	dumpErr error,
) error {
	es.lifecycleMu.RLock()
	publisher := es.checkpoints
	es.lifecycleMu.RUnlock()
	if publisher == nil {
		return fmt.Errorf("checkpoints publisher is not initialized")
	}

	ci := checkpointInfo{
		ActionID:     req.ActionID,
		JobID:        req.JobID,
		CheckpointID: checkpointID,
	}
	if dumpErr != nil {
		ci.Status = "error"
	} else {
		ci.Status = "success"
		ci.Path = path
		if dumpResp != nil && dumpResp.State != nil {
			ci.GPU = dumpResp.State.GetGPUEnabled()
			ci.Platform = dumpResp.State.GetHost().GetPlatform()
		}
	}

	if profilingData != nil {
		profiling.Clean(profilingData)
		profiling.Flatten(profilingData)

		fmt.Println()
		profiling.Print(profilingData, features.Theme())

		var totalDuration, totalIO int64
		for _, component := range profilingData.Components {
			if !(component.Parallel || component.Redundant) {
				totalDuration += component.Duration
			}
			if !component.Redundant {
				totalIO += component.IO
			}
		}
		ci.ProfilingInfo = profilingInfo{
			Raw:           profilingData,
			TotalDuration: totalDuration,
			TotalIO:       totalIO,
		}
	}

	data, err := json.Marshal(ci)
	if err != nil {
		return err
	}
	err = publisher.Publish(
		data,
		[]string{"bridge_checkpoint_response"},
		rabbitmq.WithPublishOptionsExchange("bridge_checkpoint_response"),
	)
	if err != nil {
		return err
	}
	if dumpErr != nil {
		log.Error().Err(dumpErr).Msg("checkpoint published with error")
	} else {
		log.Info().Str("path", path).Bool("GPU", ci.GPU).Msg("checkpoint published")
	}
	return nil
}
