package cmd

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/client"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/wagslane/go-rabbitmq"

	"google.golang.org/grpc"
)

const (
	DEFAULT_PROTOCOL    = "tcp"
	DEFAULT_ADDRESS     = "0.0.0.0:8080"
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

		return startHelper(ctx, startChroot, DEFAULT_ADDRESS, DEFAULT_PROTOCOL)
	},
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy cedana from host of kubernetes worker node",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if err := destroyCedana(ctx); err != nil {
			return fmt.Errorf("error destroying cedana on host: %w", err)
		}

		return nil
	},
}

func destroyCedana(ctx context.Context) error {
	if err := runScript(ctx, cleanupHostScript, true); err != nil {
		return fmt.Errorf("error cleaning up host: %w", err)
	}

	return nil
}

type EventStream struct {
	conn      *rabbitmq.Conn
	consumer  *rabbitmq.Consumer
	publisher *rabbitmq.Publisher
	queueURL  string
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
	publisher, err := rabbitmq.NewPublisher(
		es.conn,
	)
	if err != nil {
		log.Fatal().Err(fmt.Errorf("Failed to connect to RabbitMQ: %v", err)).Msg("new publisher failed")
	}
	es.publisher = publisher
	return nil
}

type CheckpointPodReq struct {
	PodName   string `json:"pod_name"`
	RuncRoot  string `json:"runc_root"`
	Namespace string `json:"namespace"`

	ActionId string `json:"action_id"`
}

func FindContainersForReq(req CheckpointPodReq, address, protocol string) ([]string, error) {
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
	res := []string{}
	for _, c := range resp.K8S.Containers {
		res = append(res, c.Runc.ID)
	}
	return res, nil
}

func CheckpointContainer(ctx context.Context, runcId, runcRoot, address, protocol string) (*daemon.DumpResp, error) {
	client, err := client.New(address, protocol)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	leaveRunning := true
	resp, _, err := client.Dump(ctx, &daemon.DumpReq{
		Dir:    "/tmp",
		Stream: 0,
		Type:   "runc",
		Criu: &criu.CriuOpts{
			LeaveRunning: &leaveRunning,
		},
		Details: &daemon.Details{
			Runc: &runc.Runc{
				ID:   runcId,
				Root: runcRoot,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

type CheckpointInformation struct {
	ActionId     string `json:"action_id"`
	PodId        string `json:"pod_id"`
	CheckpointId string `json:"checkpoint_id"`
	Status       string `json:"status"`
}

func (es *EventStream) PublishCheckpointSuccess(req CheckpointPodReq, resp *daemon.DumpResp) rabbitmq.Action {
	err := es.NewPublisher()
	if err != nil {
		log.Error().Err(err).Msg("creation of publisher failed")
		return rabbitmq.NackDiscard
	}
	data, err := json.Marshal(CheckpointInformation{
		ActionId:     req.ActionId,
		CheckpointId: *resp.Id,
		Status:       "success",
	})
	if err != nil {
		log.Error().Err(err).Msg("failed to create checkpoint info")
		return rabbitmq.Ack
	}
	err = es.publisher.Publish(data, []string{"checkpoint_response"})
	if err != nil {
		log.Error().Err(err).Msg("creation of publisher failed")
		return rabbitmq.Ack
	}
	return rabbitmq.Ack
}

func (es *EventStream) ConsumeCheckpointRequest(address, protocol string) error {
	queueName := "cedana_daemon_helper"
	consumer, err := rabbitmq.NewConsumer(
		es.conn,
		queueName,
		rabbitmq.WithConsumerOptionsExchangeName("daemon_broadcast_request"),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsExchangeKind("fanout"),
		rabbitmq.WithConsumerOptionsConsumerName("cedana_helper"),
	)
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to rabbitmq")
		return fmt.Errorf("Failed to connect to RabbitMQ: %v", err)
	}
	defer consumer.Close()

	err = es.consumer.Run(func(msg rabbitmq.Delivery) rabbitmq.Action {
		var req CheckpointPodReq
		if err := json.Unmarshal(msg.Body, &req); err != nil {
			log.Error().Err(err).Msg("Failed to unmarshal message")
			return rabbitmq.Manual
		}
		containers, err := FindContainersForReq(req, address, protocol)
		if err != nil {
			log.Info().Err(err).Msg("Failed to find pod")
			return rabbitmq.Manual
		}
		// if no containers found skip
		if len(containers) == 0 {
			return rabbitmq.Manual
		}
		runcRoot := req.RuncRoot
		// TODO SA: support multiple container pod checkpoint/restore
		for _, runcId := range containers {
			resp, err := CheckpointContainer(
				context.Background(),
				runcId,
				runcRoot,
				address,
				protocol,
			)
			if err != nil {
				log.Error().Err(err).Msg("Failed to checkpoint pod containers")
				return rabbitmq.Ack
			} else {
				return es.PublishCheckpointSuccess(req, resp)
			}
		}
		return rabbitmq.Ack
	})
	if err != nil {
		log.Error().Err(err).Msg("Create Workload Consumer failed")
		return err
	}
	return nil
}

func startHelper(ctx context.Context, startChroot bool, address string, protocol string) error {
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

					err := startDaemon(ctx, startChroot, address, protocol)
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
				fmt.Println("Received kill signal. Exiting...")
				os.Exit(0)
			}
		}
	}()

	w.Add(1)
	go func() {
		defer w.Done()

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

func startDaemon(ctx context.Context, startChroot bool, address string, protocol string) error {
	if startChroot {
		err := runScript(ctx, startChrootScript, true)
		if err != nil {
			return err
		}
	} else {
		err := runCommand(ctx, "cedana", "daemon", "start", "--address", address, "--protocol", protocol)
		if err != nil {
			return err
		}
	}

	return nil
}

func createClientWithRetry(address, protocol string) (*client.Client, error) {
	var c *client.Client
	var err error

	for i := 0; i < MAX_RETRIES; i++ {
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

func runCommand(ctx context.Context, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
