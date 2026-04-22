package restorenotify

import (
	"context"
	"fmt"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/wagslane/go-rabbitmq"
)

type rabbitPublisher struct {
	conn *rabbitmq.Conn
	pub  *rabbitmq.Publisher
}

func NewRabbitPublisher(_ context.Context, url string) (Publisher, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	conn, err := rabbitmq.NewConn(
		url,
		rabbitmq.WithConnectionOptionsConfig(
			rabbitmq.Config{
				Properties: amqp.Table{
					"connection_name": fmt.Sprintf("cedana-restore-%s-%d", hostname, time.Now().UnixNano()),
				},
			},
		),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to rabbitmq: %w", err)
	}

	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create rabbitmq publisher: %w", err)
	}

	return &rabbitPublisher{conn: conn, pub: pub}, nil
}

func (p *rabbitPublisher) Publish(ctx context.Context, queue string, payload []byte) error {
	return p.pub.PublishWithContext(
		ctx,
		payload,
		[]string{queue},
		rabbitmq.WithPublishOptionsExchange(""),
		rabbitmq.WithPublishOptionsContentType("application/json"),
		rabbitmq.WithPublishOptionsPersistentDelivery,
	)
}

func (p *rabbitPublisher) Close() error {
	if p.pub != nil {
		p.pub.Close()
	}
	var err error
	if p.conn != nil {
		if closeErr := p.conn.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}
