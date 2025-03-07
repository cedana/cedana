package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cedana/cedana/pkg/utils"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)

const (
	METRICS_POLL_INTERVAL = 10 * time.Second
	METRICS_QUEUE_NAME    = "metrics"
)

type ServiceDiscoveryResponse struct {
	QueueURL string `json:"QueueURL"`
}

type MetricsStream struct {
	conn        *amqp.Connection
	ch          *amqp.Channel
	queueURL    string
	queue       *amqp.Queue
	notifyClose chan *amqp.Error
}

func failOnError(err error, msg string) error {
	if err != nil {
		log.Error().Msgf("%s: %s", msg, err)
	}
	return err
}

func NewMetricsStream(queueURL string) (*MetricsStream, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	clientName := fmt.Sprintf("cedana-metrics-%s-%d", hostname, time.Now().UnixNano())

	config := amqp.Config{
		Properties: amqp.Table{
			"connection_name": clientName,
		},
	}

	conn, err := amqp.DialConfig(queueURL, config)
	if err := failOnError(err, "Failed to connect to RabbitMQ"); err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err := failOnError(err, "Failed to open a channel"); err != nil {
		conn.Close() // Fix 1: Close connection if channel creation fails
		return nil, err
	}

	ms := &MetricsStream{
		conn:        conn,
		ch:          ch,
		queueURL:    queueURL,
		notifyClose: make(chan *amqp.Error),
	}

	queue, err := ms.declareQueue()
	if err := failOnError(err, "Failed to declare queue"); err != nil {
		ms.Close()
		return nil, err
	}
	ms.queue = queue

	conn.NotifyClose(ms.notifyClose)
	go ms.monitorConnection()

	return ms, nil
}

func (ms *MetricsStream) declareQueue() (*amqp.Queue, error) {
	q, err := ms.ch.QueueDeclare(
		METRICS_QUEUE_NAME, // name
		true,               // durable
		false,              // delete when unused
		false,              // exclusive
		false,              // no-wait
		nil,                // arguments
	)
	if err := failOnError(err, "Failed to declare a queue"); err != nil {
		return nil, err
	}
	return &q, nil
}

func (ms *MetricsStream) monitorConnection() {
	for err := range ms.notifyClose {
		log.Error().Msgf("AMQP connection closed: %v", err)
		// Attempt to reconnect
		for {
			if err := ms.reconnect(); err == nil {
				break
			}
			time.Sleep(5 * time.Second)
		}
	}
}

func (ms *MetricsStream) reconnect() error {
	ms.Close()
	newMs, err := NewMetricsStream(ms.queueURL)
	if err != nil {
		return err
	}
	*ms = *newMs
	return nil
}

func (ms *MetricsStream) Close() {
	if ms.ch != nil {
		ms.ch.Close()
	}
	if ms.conn != nil {
		ms.conn.Close()
	}
}

func (ms *MetricsStream) publishMetrics(ctx context.Context, metrics DummyMetrics) error {
	body, err := json.Marshal(metrics)
	if err := failOnError(err, "Failed to marshal metrics"); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err = ms.ch.PublishWithContext(ctx,
		"",            // exchange
		ms.queue.Name, // routing key
		false,         // mandatory
		false,         // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		})
	if err := failOnError(err, "Failed to publish metrics"); err != nil {
		return err
	}

	log.Debug().Msgf(" [x] Sent metrics: %s", string(body))
	return nil
}

func PollAndPublishMetrics(ctx context.Context, serviceURL string) error {
	queueURL, err := discoverQueueURL(serviceURL + "/service/discover")
	if err != nil {
		return fmt.Errorf("failed to discover queue URL: %w", err)
	}

	ms, err := NewMetricsStream(queueURL)
	if err != nil {
		return err
	}

	ctxWithCancel, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start publishing metrics
	go func() {
		macAddr, _ := utils.GetMACAddress()
		hostname, _ := os.Hostname()

		for {
			select {
			case <-ctxWithCancel.Done():
				ms.Close()
				return
			default:
				metrics := generateDummyMetrics(macAddr, hostname)
				if err := ms.publishMetrics(ctx, metrics); err != nil {
					log.Error().Err(err).Msg("failed to publish metrics")
				}
				time.Sleep(METRICS_POLL_INTERVAL)
			}
		}
	}()

	return nil
}

func discoverQueueURL(serviceURL string) (string, error) {
	resp, err := http.Get(serviceURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var discovery ServiceDiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return "", err
	}

	return discovery.QueueURL, nil
}

type DummyMetrics struct {
	Timestamp string  `json:"timestamp"`
	MacAddr   string  `json:"mac_addr"`
	Hostname  string  `json:"hostname"`
	CPU       float64 `json:"cpu_usage"`
	Memory    float64 `json:"memory_usage"`
}

func generateDummyMetrics(macAddr, hostname string) DummyMetrics {
	return DummyMetrics{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		MacAddr:   macAddr,
		Hostname:  hostname,
		CPU:       float64(time.Now().Unix()%60) / 100, // dummy CPU usage 0-0.6
		Memory:    float64(time.Now().Unix()%80) / 100, // dummy memory usage 0-0.8
	}
}
