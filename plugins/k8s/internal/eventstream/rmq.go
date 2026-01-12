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
	"github.com/cedana/cedana/pkg/features"
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

func (es *EventStream) StartMultiPodConsumer(ctx context.Context) error {
	queueName := "cedana_multinode_helper-" + rand.Text()
	consumer, err := rabbitmq.NewConsumer(
		es.Conn,
		queueName,
		rabbitmq.WithConsumerOptionsExchangeName("multinode_broadcast_request"),
		rabbitmq.WithConsumerOptionsExchangeKind("fanout"),
		rabbitmq.WithConsumerOptionsConcurrency(10),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsConsumerName("cedana_multinode_helper"),
		rabbitmq.WithConsumerOptionsRoutingKey(""),
		rabbitmq.WithConsumerOptionsBinding(rabbitmq.Binding{
			RoutingKey:     "",
			BindingOptions: rabbitmq.BindingOptions{},
		}),
	)
	if err != nil {
		return err
	}

	return consumer.Run(func(msg rabbitmq.Delivery) rabbitmq.Action {
		var cmd MultiNodeCommand
		if err := json.Unmarshal(msg.Body, &cmd); err != nil {
			return rabbitmq.Ack
		}

		var req *checkpointReq
		var action string

		if cmd.Freeze != nil {
			req = cmd.Freeze
			action = "FREEZE"
		} else if cmd.Dump != nil {
			req = cmd.Dump
			action = "DUMP"
		} else if cmd.Unfreeze != nil {
			req = cmd.Unfreeze
			action = "UNFREEZE"
		}

		podsHandled, err := es.handleMultiNodeAction(ctx, action, req)

		// send response for each pod handled -> am thinking could have multiple allocated to same node
		if msg.ReplyTo != "" {
			if podsHandled == 0 {
				// If this node has no pods, it shouldn't send anything
				// because Rust isn't expecting a message from "every node",
				// it's expecting a message for "every pod".
				return rabbitmq.Ack
			}

			// Send a success message for every pod this node handled
			for i := 0; i < podsHandled; i++ {
				es.sendMultiNodeResponseToPropagator(ctx, msg.ReplyTo, msg.CorrelationId, err)
			}
		}

		return rabbitmq.Ack
	})
}

/////////////
// Helpers //
/////////////

type checkpointReq struct {
	PodID                []string             `json:"pod_id,omitempty"`
	PodName              []string             `json:"pod_name,omitempty"`
	Namespace            string               `json:"namespace,omitempty"`
	ClusterID            string               `json:"cluster_id,omitempty"`
	AllInCedanaNamespace bool                 `json:"all_in_cedana_namespace,omitempty"`
	ActionId             string               `json:"action_id,omitempty"`
	Kind                 string               `json:"kind,omitempty"`
	Reason               string               `json:"reason,omitempty"`
	Overrides            *checkpointOverrides `json:"overrides,omitempty"`
}

type checkpointOverrides struct {
	CRIUOpts    string `json:"criu_opts"`
	Directory   string `json:"directory"`
	Compression string `json:"compression"`
	Streams     int    `json:"streams"`
	Async       bool   `json:"asynchronous"`
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

type MultiNodeCommand struct {
	Freeze   *checkpointReq `json:"Freeze,omitempty"`
	Dump     *checkpointReq `json:"Dump,omitempty"`
	Unfreeze *checkpointReq `json:"Unfreeze,omitempty"`
}

// to shared state across stages
type multiNodePodState struct {
	containers      []*containerd.Containerd
	checkpointIdMap map[int]string
	specMap         map[int]*specs.Spec
	imageMap        map[int]string
	dumpReqs        []*daemon.DumpReq
	imageSecret     *imageSecret
}

// keyed by action ID + pod name
var multiNodeStates = sync.Map{}

func (es *EventStream) checkpointHandler(ctx context.Context) rabbitmq.Handler {
	return func(msg rabbitmq.Delivery) rabbitmq.Action {
		log.Trace().Msgf("received checkpoint request: %s", string(msg.Body))

		var req checkpointReq

		if err := json.Unmarshal(msg.Body, &req); err != nil {
			log.Error().Err(err).Msg("failed to unmarshal message")
			return rabbitmq.Ack
		}
		if len(req.PodName) == 0 {
			log.Warn().Msg("checkpoint request missing pod_name; ignoring (likely multi-node request)")
			return rabbitmq.Ack
		}

		log := log.With().Str("action_id", req.ActionId).Str("kind", req.Kind).Str("pod", req.PodName[0]).Str("namespace", req.Namespace).Logger()

		query := &daemon.QueryReq{
			Type: "k8s",
			K8S: &k8s.QueryReq{
				Names:         req.PodName,
				Namespace:     req.Namespace,
				ContainerType: "container",
			},
		}
		queryResp, err := es.cedana.Query(ctx, query)
		if err != nil {
			log.Error().Err(err).Msg("failed to query pods")
			return rabbitmq.Ack
		}
		if len(queryResp.K8S.Pods) == 0 {
			log.Debug().Msg("no pods found for checkpoint request")
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
				container.Image = nil
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
					LinkRemap:       proto.Bool(true),
				},
				Details: &daemon.Details{
					Containerd: container,
				},
			}
			if req.Overrides != nil {
				criuOpts := &criu.CriuOpts{}
				err = json.Unmarshal([]byte(req.Overrides.CRIUOpts), criuOpts)
				if err != nil {
					log.Error().Err(err).Msg("failed to unmarshal CRIU option overrides from checkpoint request")
				} else {
					dumpReq.Criu = criuOpts
				}
				dumpReq.Compression = req.Overrides.Compression
				dumpReq.Dir = req.Overrides.Directory
				dumpReq.Streams = int32(req.Overrides.Streams)
				dumpReq.Async = req.Overrides.Async
			}
			log.Debug().Str("container", container.ID).Interface("req", dumpReq).Msg("prepared dump request for container")
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
					req.PodName[0],
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
					req.PodName[0],
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

func (es *EventStream) handleMultiNodeAction(ctx context.Context, action string, req *checkpointReq) (int, error) {
	if req == nil || len(req.PodName) == 0 {
		return 0, fmt.Errorf("no pod names provided")
	}

	log := log.With().
		Str("action", action).
		Str("action_id", req.ActionId).
		Strs("pod_names", req.PodName).
		Str("namespace", req.Namespace).
		Logger()

	// instead of ckpt single pod, we pass whole list to query local daemon for all pod names in the list
	query := &daemon.QueryReq{
		Type: "k8s",
		K8S: &k8s.QueryReq{
			Names:         req.PodName, // pass entire list
			Namespace:     req.Namespace,
			ContainerType: "container",
		},
	}

	queryResp, err := es.cedana.Query(ctx, query)
	if err != nil {
		log.Error().Err(err).Msg("failed to query pods")
		return 0, err
	}

	if len(queryResp.K8S.Pods) == 0 {
		log.Debug().Msg("no pods found on this node for multinode action")
		return 0, nil
	}

	podsFound := len(queryResp.K8S.Pods)
	log.Info().Int("pods_found", podsFound).Msg("[multinode] found pods on this node")

	// process each pod found on this node
	errChan := make(chan error, podsFound)
	wg := sync.WaitGroup{}
	wg.Add(podsFound)

	for _, pod := range queryResp.K8S.Pods {
		p := pod
		go func(p *k8s.Pod) {
			defer wg.Done()

			var err error
			switch action {
			case "FREEZE":
				err = es.freezeMultiPod(ctx, p, req)
			case "DUMP":
				err = es.dumpMultiPod(ctx, p, req)
			case "UNFREEZE":
				err = es.unfreezeMultiPod(ctx, p, req)
			default:
				err = fmt.Errorf("unknown action: %s", action)
			}

			if err != nil {
				errChan <- err
			}
		}(p)
	}

	wg.Wait()
	close(errChan)

	// Check if any pod failed
	for err := range errChan {
		if err != nil {
			return podsFound, err // Return count even on error so propagator knows
		}
	}

	return podsFound, nil
}

func (es *EventStream) freezeMultiPod(ctx context.Context, pod *k8s.Pod, req *checkpointReq) error {
	containers := pod.Containerd
	if (len(containers)) == 0 {
		return fmt.Errorf("no containers found in pod")
	}

	podName := pod.Name

	log := log.With().
		Str("pod", podName).
		Str("action_id", req.ActionId).
		Logger()

	log.Info().Int("containers", len(containers)).Msg("[multinode] freezing pod containers")

	state := &multiNodePodState{
		containers:      containers,
		checkpointIdMap: make(map[int]string),
		specMap:         make(map[int]*specs.Spec),
		imageMap:        make(map[int]string),
		dumpReqs:        make([]*daemon.DumpReq, 0),
	}

	for i, container := range containers {
		spec, err := runc.LoadSpec(filepath.Join("/host", container.Runc.Bundle, "config.json"))
		if err != nil {
			return fmt.Errorf("failed to load spec for container: %w", err)
		}
		state.specMap[i] = spec

		checkpointId, err := es.propagator.V2().Checkpoints().Post(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to create checkpoint in propagator: %w", err)
		}
		state.checkpointIdMap[i] = *checkpointId
	}

	rootfs := strings.HasPrefix(req.Kind, "rootfs")
	rootfsOnly := req.Kind == "rootfsonly"

	if rootfs {
		imageSecret, err := es.getImageSecret()
		if err != nil {
			return fmt.Errorf("failed to fetch image secret: %w", err)
		}
		state.imageSecret = imageSecret
	}

	for i, container := range containers {
		state.imageMap[i] = container.Image.GetName()
		container.Address = es.containerdAddress

		if rootfs {
			container.Image = &containerd.Image{
				Name:     state.imageSecret.ImageSource + ":" + state.checkpointIdMap[i],
				Username: strings.Split(state.imageSecret.ImageSecret, ":")[0],
				Secret:   strings.Split(state.imageSecret.ImageSecret, ":")[1],
			}
			container.Rootfs = rootfs
			container.RootfsOnly = rootfsOnly
		} else {
			container.Image = nil
		}

		dumpReq := &daemon.DumpReq{
			Name: state.checkpointIdMap[i],
			Type: "containerd",
			Criu: &criu.CriuOpts{
				LeaveRunning:    proto.Bool(true),
				TcpEstablished:  proto.Bool(true),
				TcpSkipInFlight: proto.Bool(true),
				LinkRemap:       proto.Bool(true),
			},
			Details: &daemon.Details{
				Containerd: container,
			},
		}

		if req.Overrides != nil {
			criuOpts := &criu.CriuOpts{}
			err := json.Unmarshal([]byte(req.Overrides.CRIUOpts), criuOpts)
			if err == nil {
				dumpReq.Criu = criuOpts
			}
			dumpReq.Compression = req.Overrides.Compression
			dumpReq.Dir = req.Overrides.Directory
			dumpReq.Streams = int32(req.Overrides.Streams)
			dumpReq.Async = req.Overrides.Async
		}

		state.dumpReqs = append(state.dumpReqs, dumpReq)
	}

	wg := sync.WaitGroup{}
	var mu sync.Mutex
	errMap := make(map[int]error)
	wg.Add(len(state.dumpReqs))

	for i, dumpReq := range state.dumpReqs {
		go func(i int, dumpReq *daemon.DumpReq) {
			defer wg.Done()
			_, _, err := es.cedana.Freeze(ctx, dumpReq)
			if err != nil {
				mu.Lock()
				errMap[i] = err
				mu.Unlock()
			}
		}(i, dumpReq)
	}

	wg.Wait()

	if len(errMap) > 0 {
		for i, err := range errMap {
			log.Error().
				Int("container_order", i).
				Str("container", containers[i].ID).
				Err(err).
				Msg("[multinode] failed to freeze container")

			// for tracking if individual containers failed -> publish to propagator
			es.publishCheckpoint(
				log.WithContext(ctx),
				podName,
				req.ActionId,
				state.checkpointIdMap[i],
				nil, "", nil, i,
				state.specMap[i],
				err,
			)
		}
		return fmt.Errorf("[multinode] failed to freeze some containers")
	}

	log.Info().Msg("[multinode] all containers frozen successfully")

	// state stored for DUMP phase
	stateKey := fmt.Sprintf("%s:%s", req.ActionId, podName)
	multiNodeStates.Store(stateKey, state)

	return nil
}

func (es *EventStream) dumpMultiPod(ctx context.Context, pod *k8s.Pod, req *checkpointReq) error {
	podName := pod.Name

	log := log.With().
		Str("pod", podName).
		Str("action_id", req.ActionId).
		Logger()

	stateKey := fmt.Sprintf("%s:%s", req.ActionId, podName)
	stateVal, ok := multiNodeStates.Load(stateKey)
	if !ok {
		return fmt.Errorf("no freeze state found for pod - was FREEZE called first?")
	}
	state := stateVal.(*multiNodePodState)

	log.Info().Msg("[multinode] starting dump of frozen containers")
	wg := sync.WaitGroup{}
	wg.Add(len(state.dumpReqs))

	for i, dumpReq := range state.dumpReqs {
		go func(i int, dumpReq *daemon.DumpReq) {
			defer wg.Done()

			dumpResp, profiling, err := es.cedana.Dump(ctx, dumpReq)
			var path string
			var stateData *daemon.ProcessState
			if err == nil {
				path = dumpResp.Paths[0]
				stateData = dumpResp.State
			}

			es.publishCheckpoint(
				log.WithContext(ctx),
				podName,
				req.ActionId,
				state.checkpointIdMap[i],
				profiling,
				path,
				stateData,
				i,
				state.specMap[i],
				err,
			)
		}(i, dumpReq)
	}

	wg.Wait()
	log.Info().Msg("[multinode] dump complete for all containers")

	return nil
}

func (es *EventStream) unfreezeMultiPod(ctx context.Context, pod *k8s.Pod, req *checkpointReq) error {
	podName := pod.Name

	log := log.With().
		Str("pod", podName).
		Str("action_id", req.ActionId).
		Logger()

	stateKey := fmt.Sprintf("%s:%s", req.ActionId, podName)
	stateVal, ok := multiNodeStates.Load(stateKey)
	if !ok {
		return fmt.Errorf("no state found for pod - was FREEZE called first?")
	}
	state := stateVal.(*multiNodePodState)

	log.Info().Msg("[multinode] unfreezing all containers")

	wg := sync.WaitGroup{}
	var mu sync.Mutex
	errMap := make(map[int]error)
	wg.Add(len(state.dumpReqs))

	for i, dumpReq := range state.dumpReqs {
		go func(i int, dumpReq *daemon.DumpReq) {
			defer wg.Done()
			_, _, err := es.cedana.Unfreeze(ctx, dumpReq)
			if err != nil {
				mu.Lock()
				errMap[i] = err
				mu.Unlock()
			}
		}(i, dumpReq)
	}

	wg.Wait()

	for i, err := range errMap {
		if err != nil {
			log.Error().
				Int("container_order", i).
				Str("container", state.containers[i].ID).
				Err(err).
				Msg("failed to unfreeze container")
		}
	}

	// clean up state
	multiNodeStates.Delete(stateKey)
	log.Info().Msg("[multinode] unfreeze complete, state cleaned up")

	return nil
}

func (es *EventStream) sendMultiNodeResponseToPropagator(ctx context.Context, replyTo string, correlationId string, err error) {
	status := "SUCCESS"
	if err != nil {
		status = err.Error()
	}

	// default exchange with rk as queue name -> direct publish to queue
	// uses the same checkpoints (publisher) established by StartCheckpointsPublisher
	es.checkpoints.Publish(
		[]byte(status),
		[]string{replyTo},
		rabbitmq.WithPublishOptionsExchange(""),
		rabbitmq.WithPublishOptionsCorrelationID(correlationId),
	)
}
