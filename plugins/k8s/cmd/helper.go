package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	cedanagosdk "github.com/cedana/cedana-go-sdk"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/wagslane/go-rabbitmq"

	"google.golang.org/grpc"
)

var (
	PROTOCOL = os.Getenv("CEDANA_PROTOCOL")
	ADDRESS  = os.Getenv("CEDANA_ADDRESS")
)

const (
	MAX_RETRIES         = 5
	CLIENT_RETRY_PERIOD = time.Second
)

//go:embed scripts/setup-host.sh
var setupHostScript string

//go:embed scripts/cleanup-host.sh
var cleanupHostScript string

//go:embed scripts/bump-restart.sh
var restartScript string

//go:embed scripts/start-chroot.sh
var startChrootScript string

//go:embed scripts/stop-chroot.sh
var stopChrootScript string

func init() {
	HelperCmd.AddCommand(destroyCmd)

	HelperCmd.Flags().Bool("setup-host", false, "Setup host for Cedana")
	HelperCmd.Flags().Bool("restart", false, "Restart the Cedana daemon on the host")
	HelperCmd.Flags().Bool("start-chroot", false, "Start chroot and Cedana daemon")
}

var HelperCmd = &cobra.Command{
	Use:   "k8s-helper",
	Short: "Helper for running in Kubernetes",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		restart, _ := cmd.Flags().GetBool("restart")
		if restart {
			if err := runScript(ctx, restartScript, true); err != nil {
				return fmt.Errorf("error restarting: %w", err)
			}
		}

		setupHost, _ := cmd.Flags().GetBool("setup-host")
		if setupHost {
			if err := runScript(ctx, setupHostScript, true); err != nil {
				return fmt.Errorf("error setting up host: %w", err)
			}
		}

		startChroot, _ := cmd.Flags().GetBool("start-chroot")
		startChroot = startChroot || setupHost

		return startHelper(ctx, ADDRESS, PROTOCOL)
	},
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy cedana from host of kubernetes worker node",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		return destroyDaemon(context.WithoutCancel(ctx))
	},
}

func startHelper(ctx context.Context, address string, protocol string) error {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
	var w sync.WaitGroup

	_, err := createClientWithRetry(address, protocol)
	if err != nil {
		return fmt.Errorf("failed to create client after %d attempts: %w", MAX_RETRIES, err)
	}

	// Goroutine to check if the daemon is running
	w.Add(1)
	go func() {
		defer w.Done()
		ticker := time.NewTicker(time.Second * 10)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				isRunning, err := isDaemonRunning(ctx, address, protocol)
				if err != nil {
					fmt.Printf("Error checking if daemon is running: %v\n", err)
					continue
				}
				if !isRunning {
					fmt.Printf("Daemon is not running. Restarting...\n")

					err := restartDaemon(ctx)
					if err != nil {
						fmt.Printf("Error restarting Cedana: %v\n", err)
						continue
					}

					_, err = createClientWithRetry(address, protocol)
					if err != nil {
						fmt.Printf("Failed to create client after %d attempts: %v\n", MAX_RETRIES, err)
						continue
					}

					fmt.Println("Daemon restarted.")
				}

			case <-signalChannel:
				fmt.Println("Received kill signal. Stopping...")
				err := stopDaemon(context.WithoutCancel(ctx))
				if err != nil {
					os.Exit(1)
					fmt.Printf("Error stopping Cedana daemon: %v\n", err)
				}
				os.Exit(0)
			}
		}
	}()

	cedanaUrl := os.Getenv("CEDANA_URL")
	log.Info().Msgf("cedanaURL: %v", cedanaUrl)
	client := cedanagosdk.NewCedanaClient(
		cedanaUrl,
		os.Getenv("CEDANA_AUTH_TOKEN"),
	)
	url, err := client.V2().Discover().ByName("rabbitmq").Get(ctx, nil)
	if err != nil {
		return err
	}
	stream, err := NewEventStream(*url)
	if err != nil {
		return err
	}
	go func() {
		log.Info().Msg("Listening on rabbitmq stream for checkpoint requests")
		consumer, err := stream.ConsumeCheckpointRequest(address, protocol)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup checkpint request consumer")
		}
		defer consumer.Close()
		w.Wait()
	}()

	// scrape daemon logs for kubectl logs output
	go func() {
		defer w.Done()
		file, err := os.Open("/host/var/log/cedana-daemon.log")
		if err != nil {
			fmt.Println("Failed to open cedana-daemon.log")
			return
		}
		defer file.Close()

		reader := bufio.NewReader(file)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					time.Sleep(1 * time.Second)
					continue
				}
				fmt.Println("Error reading cedana-daemon.log")
				return
			}
			trimmed := strings.TrimSpace(line)
			if len(trimmed) > 0 {
				// we don't use the log function as the logs should have their own timing data
				fmt.Println(trimmed)
			}
		}
	}()

	w.Wait()

	return nil
}

func destroyDaemon(ctx context.Context) error {
	if err := runScript(ctx, cleanupHostScript, true); err != nil {
		return fmt.Errorf("error cleaning up host: %w", err)
	}

	return nil
}

func restartDaemon(ctx context.Context) error {
	return runScript(ctx, startChrootScript, true)
}

func stopDaemon(ctx context.Context) error {
	return runScript(ctx, stopChrootScript, true)
}

func createClientWithRetry(address, protocol string) (*client.Client, error) {
	var c *client.Client
	var err error

	for i := range MAX_RETRIES {
		c, err = client.New(address, protocol)
		if err == nil {
			// Successfully created the client, break out of the loop
			break
		}

		fmt.Printf("Error creating client: %v. Retrying...\n", err)
		time.Sleep(CLIENT_RETRY_PERIOD)

		if i == MAX_RETRIES-1 {
			// If it's the last attempt, return the error
			return nil, fmt.Errorf("failed to create client after %d attempts", MAX_RETRIES)
		}
	}

	return c, nil
}

func isDaemonRunning(ctx context.Context, address, protocol string) (bool, error) {
	client, err := client.New(address, protocol)
	if err != nil {
		return false, err
	}
	defer client.Close()
	// Wait for the daemon to be ready, and do health check
	resp, err := client.HealthCheck(ctx, &daemon.HealthCheckReq{Full: false}, grpc.WaitForReady(true))
	if err != nil {
		return false, fmt.Errorf("cedana health check failed: %w", err)
	}
	errorsFound := false
	for _, result := range resp.Results {
		for _, component := range result.Components {
			for _, errs := range component.Errors {
				log.Error().Str("name", component.Name).Str("data", component.Data).Msgf("cedana health check error: %v", errs)
				errorsFound = true
			}
			for _, warning := range component.Warnings {
				log.Warn().Str("name", component.Name).Str("data", component.Data).Msgf("cedana health check warning: %v", warning)
			}
		}
	}
	if errorsFound {
		return false, fmt.Errorf("cedana health check failed")
	}
	return true, nil
}

func runScript(ctx context.Context, script string, logOutput bool) error {
	cmd := exec.CommandContext(ctx, "bash")
	cmd.Stdin = strings.NewReader(script)

	if logOutput {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}

type EventStream struct {
	conn     *rabbitmq.Conn
	queueURL string
}

func NewEventStream(queueURL string) (*EventStream, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	clientName := fmt.Sprintf("cedana-daemon-%s-%d", hostname, time.Now().UnixNano())

	config := rabbitmq.Config{
		Properties: amqp.Table{
			"connection_name": clientName,
		},
	}
	conn, err := rabbitmq.NewConn(
		queueURL,
		rabbitmq.WithConnectionOptionsConfig(config),
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to RabbitMQ: %v", err)
	}

	es := &EventStream{
		conn:     conn,
		queueURL: queueURL,
	}
	return es, nil
}

func (es *EventStream) NewPublisher() error {
	return nil
}

type CheckpointPodReq struct {
	PodName   string `json:"pod_name"`
	RuncRoot  string `json:"runc_root"`
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	ActionId  string `json:"action_id"`
}

func FindContainersForReq(req CheckpointPodReq, address, protocol string) ([]*k8s.Container, error) {
	client, err := client.New(address, protocol)
	if err != nil {
		return nil, err
	}
	query := &daemon.QueryReq{
		Type: "k8s",
		K8S: &k8s.QueryReq{
			Root:         req.RuncRoot,
			Namespace:    req.Namespace,
			SandboxNames: []string{req.PodName},
		},
	}
	resp, err := client.Query(context.Background(), query)
	if err != nil {
		return nil, err
	}
	res := []*k8s.Container{}
	for _, c := range resp.K8S.Containers {
		res = append(res, c)
	}
	return res, nil
}

func CheckpointContainer(ctx context.Context, checkpointId, runcId, runcRoot, address, protocol string) (*daemon.DumpResp, *profiling.Data, error) {
	client, err := client.New(address, protocol)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	leaveRunning := true
	tcpEstablished := true
	tcpSkipInFlight := true

	resp, profiling, err := client.Dump(ctx, &daemon.DumpReq{
		Dir:  fmt.Sprintf("cedana://%s", checkpointId),
		Type: "runc",
		Criu: &criu.CriuOpts{
			LeaveRunning:    &leaveRunning,
			TcpEstablished:  &tcpEstablished,
			TcpSkipInFlight: &tcpSkipInFlight,
		},
		Details: &daemon.Details{
			Runc: &runc.Runc{
				ID:   runcId,
				Root: runcRoot,
			},
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return resp, profiling, nil
}

type Info struct {
	Data *profiling.Data `json:"data"`
}

type ImageSecret struct {
	ImageSecret string `json:"image_secret"`
	ImageSource string `json:"image_source"`
}

func GetImageSecret() (*ImageSecret, error) {
	cedanaClient := cedanagosdk.NewCedanaClient(config.Global.Connection.URL, config.Global.Connection.AuthToken)
	url := cedanaClient.RequestAdapter.GetBaseUrl() + "/v2/secrets"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+os.Getenv("CEDANA_AUTH_TOKEN"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	secret := ImageSecret{}
	err = json.Unmarshal(data, &secret)
	if err != nil {
		return nil, err
	}
	return &secret, nil
}

func CheckpointContainerRootfs(ctx context.Context, checkpointId, runcId, namespace, address, protocol string, rootfsOnly bool) (*daemon.DumpResp, *profiling.Data, error) {
	client, err := client.New(address, protocol)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	leaveRunning := true
	tcpEstablished := true

	image, err := GetImageSecret()
	if err != nil {
		// failed to fetch the image upload information
		return nil, nil, err
	}
	s := strings.Split(image.ImageSecret, ":")
	if len(s) <= 1 {
		return nil, nil, fmt.Errorf("failed to fetch valid image secrets; failed to parse secrets from propagator")
	}
	username := s[0]
	secret := s[1]

	resp, profiling, err := client.Dump(ctx, &daemon.DumpReq{
		Dir:  fmt.Sprintf("cedana://%s", checkpointId),
		Type: "containerd",
		Criu: &criu.CriuOpts{
			LeaveRunning:   &leaveRunning,
			TcpEstablished: &tcpEstablished,
		},
		Details: &daemon.Details{
			Containerd: &containerd.Containerd{
				ID:         runcId,
				Image:      image.ImageSource + ":" + checkpointId,
				Namespace:  "k8s.io",
				RootfsOnly: rootfsOnly,
				Username:   username,
				Secret:     secret,
				Address:    "/run/k3s/containerd/containerd.sock",
			},
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return resp, profiling, nil
}

type CheckpointInformation struct {
	ActionId     string `json:"action_id"`
	PodId        string `json:"pod_id"`
	CheckpointId string `json:"checkpoint_id"`
	Status       string `json:"status"`
	Path         string `json:"path"`
	Gpu          bool   `json:"gpu"`
	Platform     string `json:"platform"`
	Info         Info   `json:"info"`
}

func (es *EventStream) PublishCheckpointSuccess(req CheckpointPodReq, pod_id, id string, profiling *profiling.Data, resp *daemon.DumpResp, rootfs bool) error {
	publisher, err := rabbitmq.NewPublisher(
		es.conn,
	)
	if err != nil {
		log.Error().Err(err).Msg("creation of publisher failed")
		return err
	}

	info := Info{
		Data: profiling,
	}
	ci := CheckpointInformation{
		ActionId:     req.ActionId,
		PodId:        pod_id,
		CheckpointId: id,
		Status:       "success",
		Info:         info,
	}
	if !rootfs {
		ci.Gpu = resp.State.GPUEnabled
		ci.Platform = resp.State.Host.Platform
		ci.Path = resp.Path
	}
	data, err := json.Marshal(ci)
	if err != nil {
		log.Error().Err(err).Msg("failed to create checkpoint info")
		return err
	}
	err = publisher.Publish(data, []string{"checkpoint_response"})
	if err != nil {
		log.Error().Err(err).Msg("creation of publisher failed")
		return err
	}
	log.Info().Msg("Published checkpoint sucessfully")
	return err
}

func (es *EventStream) ConsumeCheckpointRequest(address, protocol string) (*rabbitmq.Consumer, error) {
	queueName := "cedana_daemon_helper-" + rand.Text()
	consumer, err := rabbitmq.NewConsumer(
		es.conn,
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
		log.Error().Err(err).Msg("Failed to connect to rabbitmq")
		return nil, fmt.Errorf("Failed to connect to RabbitMQ: %v", err)
	}
	err = consumer.Run(func(msg rabbitmq.Delivery) rabbitmq.Action {
		log.Info().Msg("Received Checkpoint Request over RabbitMQ")
		var req CheckpointPodReq
		if err := json.Unmarshal(msg.Body, &req); err != nil {
			log.Error().Err(err).Msg("Failed to unmarshal message")
			return rabbitmq.Ack
		}
		containers, err := FindContainersForReq(req, address, protocol)
		if err != nil {
			log.Error().Err(err).Msg("Failed to find pod")
			return rabbitmq.Ack
		}
		// if no containers found skip
		if len(containers) == 0 {
			return rabbitmq.Ack
		}
		runcRoot := req.RuncRoot
		// TODO SA: support multiple container pod checkpoint/restore
		cedanaClient := cedanagosdk.NewCedanaClient(config.Global.Connection.URL, config.Global.Connection.AuthToken)
		for _, container := range containers {
			checkpointId, err := cedanaClient.V2().Checkpoints().Post(context.Background(), nil)
			if err != nil {
				// if propagator is reachable we make the dump request otherwise we log error
				log.Error().Err(err).Str("CedanaUrl", config.Global.Connection.URL).Msg("Failed to populate a remote checkpoint in cedana database")
				continue
			}
			if req.Kind == "rootfs" || req.Kind == "rootfsonly" {
				resp, profiling, err := CheckpointContainerRootfs(
					context.Background(),
					*checkpointId,
					container.Runc.ID,
					req.Namespace,
					address,
					protocol,
					req.Kind == "rootfsonly",
				)
				if err != nil {
					log.Error().Err(err).Msg("Failed to roofs checkpoint container in pod")
				} else {
					log.Info().Msg("Publishing checkpoint...")
					err := es.PublishCheckpointSuccess(req, container.SandboxUID, *checkpointId, profiling, resp, true)
					if err != nil {
						log.Error().Err(err).Msg("failed to publish checkpoint success")
					}
				}
			} else {
				resp, profiling, err := CheckpointContainer(
					context.Background(),
					*checkpointId,
					container.Runc.ID,
					runcRoot,
					address,
					protocol,
				)
				if err != nil {
					log.Error().Err(err).Msg("Failed to checkpoint pod containers")
				} else {
					log.Info().Msg("Publishing checkpoint...")
					err := es.PublishCheckpointSuccess(req, container.SandboxUID, *checkpointId, profiling, resp, false)
					if err != nil {
						log.Error().Err(err).Msg("failed to publish checkpoint success")
					}
				}
			}
			break
		}
		return rabbitmq.Ack
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to create checkpoint")
		return nil, err
	}
	return consumer, err
}
