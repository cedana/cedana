package eventstream

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	cedanagosdk "github.com/cedana/cedana-go-sdk"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"github.com/gogo/protobuf/proto"
	"github.com/opencontainers/runtime-spec/specs-go"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
	"github.com/wagslane/go-rabbitmq"
)

type EventStream struct {
	cedana     *client.Client
	propagator *cedanagosdk.ApiClient

	url               string
	checkpoints       *rabbitmq.Publisher
	containerdAddress string
	*rabbitmq.Conn
}

func New(ctx context.Context, cedana *client.Client, propagator *cedanagosdk.ApiClient, containerdAddress string) (*EventStream, error) {
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
	clientName := fmt.Sprintf("cedana-daemon-%s-%d", hostname, time.Now().UnixNano())
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
		cedana:            cedana,
		propagator:        propagator,
		url:               *url,
		Conn:              conn,
		containerdAddress: containerdAddress,
	}
	return es, nil
}

func (es *EventStream) StartCheckpointsConsumer(ctx context.Context) error {
	queueName := "cedana_daemon_helper-" + rand.Text()
	consumer, err := rabbitmq.NewConsumer(
		es.Conn,
		queueName,
		rabbitmq.WithConsumerOptionsExchangeName("daemon_broadcast_request"),
		rabbitmq.WithConsumerOptionsConcurrency(10),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsExchangeKind("fanout"),
		rabbitmq.WithConsumerOptionsConsumerName("cedana_helper"),
		rabbitmq.WithConsumerOptionsRoutingKey(""),
		rabbitmq.WithConsumerOptionsBinding(rabbitmq.Binding{
			RoutingKey:     "",
			BindingOptions: rabbitmq.BindingOptions{},
		}),
	)
	if err != nil {
		return err
	}
	err = consumer.Run(es.checkpointHandler(ctx))
	if err != nil {
		return err
	}
	return nil
}

func (es *EventStream) StartCheckpointsPublisher(ctx context.Context) error {
	publisher, err := rabbitmq.NewPublisher(
		es.Conn,
	)
	if err != nil {
		return err
	}
	es.checkpoints = publisher
	return nil
}

/////////////
// Helpers //
/////////////

type checkpointReq struct {
	PodName   string `json:"pod_name"`
	RuncRoot  string `json:"runc_root"`
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	ActionId  string `json:"action_id"`
}

type checkpointInfo struct {
	ActionId       string        `json:"action_id"`
	PodId          string        `json:"pod_id"`
	CheckpointId   string        `json:"checkpoint_id"`
	CheckpointName string        `json:"checkpoint_name"`
	Status         string        `json:"status"`
	Path           string        `json:"path"`
	GPU            bool          `json:"gpu"`
	Platform       string        `json:"platform"`
	ProfilingInfo  profilingInfo `json:"profiling_info"`
	ContainerOrder int           `json:"container_order"`
}

type profilingInfo struct {
	Raw           *profiling.Data `json:"raw"`
	TotalDuration int64           `json:"total_duration"`
	TotalIO       int64           `json:"total_io"`
}

type imageSecret struct {
	ImageSecret string `json:"image_secret"`
	ImageSource string `json:"image_source"`
}

func (es *EventStream) checkpointHandler(ctx context.Context) rabbitmq.Handler {
	return func(msg rabbitmq.Delivery) rabbitmq.Action {
		log.Trace().Msgf("received checkpoint request: %s", string(msg.Body))

		var req checkpointReq

		if err := json.Unmarshal(msg.Body, &req); err != nil {
			log.Error().Err(err).Msg("failed to unmarshal message")
			return rabbitmq.Ack
		}
		log := log.With().Str("action_id", req.ActionId).Str("kind", req.Kind).Str("pod", req.PodName).Str("namespace", req.Namespace).Logger()

		query := &daemon.QueryReq{
			Type: "k8s",
			K8S: &k8s.QueryReq{
				Root:          req.RuncRoot,
				Namespace:     req.Namespace,
				Names:         []string{req.PodName},
				ContainerType: "container",
			},
		}
		queryResp, err := es.cedana.Query(ctx, query)
		if err != nil {
			log.Error().Err(err).Msg("failed to query pods")
			return rabbitmq.Ack
		}
		if len(queryResp.K8S.Pods) == 0 {
			log.Trace().Msg("no pods found for checkpoint request")
			return rabbitmq.Ack
		}
		containers := queryResp.K8S.Pods[0].Containerd
		if len(containers) == 0 {
			log.Trace().Msg("no containers found in pod for checkpoint request")
			return rabbitmq.Ack
		}
		log.Info().Int("containers", len(containers)).Msg("found container(s) in pod to checkpoint")

		checkpointIdMap := make(map[int]string)
		specMap := make(map[int]*specs.Spec)
		imageMap := make(map[int]string)

		// Initialize spec, checkpoints for all containers
		for i, container := range containers {
			spec, err := runc.LoadSpec(filepath.Join("/host", container.Runc.Bundle, "config.json"))
			if err != nil {
				log.Error().Err(err).Msg("failed to load spec for container")
				return rabbitmq.Ack
			}
			specMap[i] = spec

			checkpointId, err := es.propagator.V2().Checkpoints().Post(ctx, nil)
			if err != nil {
				log.Error().Err(err).Msg("failed to create checkpoint in propagator")
				return rabbitmq.Ack
			}
			checkpointIdMap[i] = *checkpointId
		}

		var imageSecret *imageSecret
		rootfs := strings.HasPrefix(req.Kind, "rootfs")
		rootfsOnly := req.Kind == "rootfsonly"

		if rootfs {
			imageSecret, err = es.getImageSecret()
			if err != nil {
				log.Error().Err(err).Msg("failed to fetch image secret from propagator for rootfs checkpoint")
				return rabbitmq.Ack
			}
		}

		for i, container := range containers {
			imageMap[i] = container.Image.GetName()
			container.Address = es.containerdAddress
			if rootfs {
				// NOTE: Currently we store all containers in the same image repository (with separate tags)
				container.Image = &containerd.Image{
					Name:     imageSecret.ImageSource + ":" + checkpointIdMap[i],
					Username: strings.Split(imageSecret.ImageSecret, ":")[0],
					Secret:   strings.Split(imageSecret.ImageSecret, ":")[1],
				}
				container.Rootfs = rootfs
				container.RootfsOnly = rootfsOnly
			} else {
				container.Image = nil // Ensure this is nil, so rootfs is not dumped
			}
		}

		var dumpReqs []*daemon.DumpReq

		for i, container := range containers {
			dumpReq := &daemon.DumpReq{
				Name: checkpointIdMap[i],
				Type: "containerd",
				Criu: &criu.CriuOpts{
					LeaveRunning:    proto.Bool(true),
					TcpEstablished:  proto.Bool(true),
					TcpSkipInFlight: proto.Bool(true),
				},
				Details: &daemon.Details{
					Containerd: container,
				},
			}
			dumpReqs = append(dumpReqs, dumpReq)
		}

		wg := sync.WaitGroup{}
		errMap := make(map[int]error)
		wg.Add(len(dumpReqs))

		for i, dumpReq := range dumpReqs {
			log := log.With().Int("container_order", i).Str("container", containers[i].ID).Logger()
			go func() {
				defer wg.Done()
				_, _, err = es.cedana.Freeze(ctx, dumpReq)
				if err != nil {
					errMap[i] = err
				}
			}()

			defer func() {
				_, _, err = es.cedana.Unfreeze(ctx, dumpReq)
				if err != nil {
					log.Error().Err(err).Msg("failed to unfreeze container")
				}
			}()
		}

		wg.Wait()

		if len(errMap) > 0 {
			for i, err := range errMap {
				log := log.With().Int("container_order", i).Str("container", containers[i].ID).Logger()
				if err != nil {
					log.Error().Err(err).Msg("failed to freeze container")
				}
				es.publishCheckpoint(
					log.WithContext(ctx),
					req.PodName,
					req.ActionId,
					checkpointIdMap[i],
					nil,
					"",
					nil,
					i,
					specMap[i],
					err,
				)
			}
			return rabbitmq.Ack
		}

		log.Info().Msg("all containers frozen, starting dump")

		wg.Add(len(dumpReqs))

		for i, dumpReq := range dumpReqs {
			go func() {
				defer wg.Done()
				dumpResp, profiling, err := es.cedana.Dump(ctx, dumpReq)
				var path string
				var state *daemon.ProcessState
				if err == nil {
					path = dumpResp.Paths[0]
					state = dumpResp.State
				}
				es.publishCheckpoint(
					log.WithContext(ctx),
					req.PodName,
					req.ActionId,
					checkpointIdMap[i],
					profiling,
					path,
					state,
					i,
					specMap[i],
					err,
				)
			}()
		}

		wg.Wait()

		return rabbitmq.Ack
	}
}

func (es *EventStream) publishCheckpoint(
	ctx context.Context,
	podId string,
	actionId string,
	checkpointId string,
	profilingData *profiling.Data,
	path string,
	state *daemon.ProcessState,
	containerOrder int,
	containerSpec *specs.Spec,
	dumpErr error,
) error {
	log := *log.Ctx(ctx)
	ci := checkpointInfo{
		ActionId:       actionId,
		PodId:          podId,
		CheckpointId:   checkpointId,
		ContainerOrder: containerOrder,
	}
	if dumpErr != nil {
		ci.Status = "error"
	} else {
		ci.Status = "success"
	}

	for _, env := range containerSpec.Process.Env {
		if name, ok := strings.CutPrefix(env, "CEDANA_CHECKPOINT="); ok {
			ci.CheckpointName = name
			log = log.With().Str("checkpoint_name", name).Logger()
		}
	}

	if state != nil {
		ci.GPU = state.GetGPUEnabled()
		ci.Platform = state.GetHost().GetPlatform()
		ci.Path = path
	}

	if profilingData != nil {
		profiling.CleanData(profilingData)
		profiling.FlattenData(profilingData)
		totalDuration := profilingData.Duration
		totalIO := profilingData.IO
		for _, component := range profilingData.Components {
			if !component.Parallel {
				totalIO += component.IO
			}
			totalDuration += component.Duration
		}
		profilingInfo := profilingInfo{
			Raw:           profilingData,
			TotalDuration: totalDuration,
			TotalIO:       totalIO,
		}
		ci.ProfilingInfo = profilingInfo
	}
	data, err := json.Marshal(ci)
	if err != nil {
		return err
	}
	err = es.checkpoints.Publish(data, []string{"checkpoint_response"})
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

func (es *EventStream) getImageSecret() (*imageSecret, error) {
	url := es.propagator.RequestAdapter.GetBaseUrl() + "/v2/secrets"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+config.Global.Connection.AuthToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	secret := imageSecret{}
	err = json.Unmarshal(data, &secret)
	if err != nil {
		return nil, err
	}
	if len(strings.Split(secret.ImageSecret, ":")) <= 1 {
		return nil, fmt.Errorf("invalid image secret received from propagator")
	}
	return &secret, nil
}
