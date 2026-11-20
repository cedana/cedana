package eventstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	propagatorsdk "github.com/cedana/cedana-propagator-sdk/go"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/rs/zerolog/log"
)

// Poll serves checkpoint requests from an in-cluster cedana backend spoke, which queues them per node, in
// place of the RabbitMQ broadcast. It needs no broker and no inbound connection to the node.
func Poll(ctx context.Context, cedana *client.Client, propagator *propagatorsdk.ApiClient, containerdAddress string) error {
	if cedana == nil || propagator == nil {
		return fmt.Errorf("cedana and propagator clients are required")
	}
	node, err := os.Hostname() // the helper runs with the host's network namespace
	if err != nil {
		return err
	}
	es := &EventStream{cedana: cedana, propagator: propagator, node: node, containerdAddress: containerdAddress, polling: true}
	path := "/v1/checkpoint/requests?node=" + url.QueryEscape(node)
	for ctx.Err() == nil {
		var reqs []checkpointReq
		body, err := es.get(ctx, path)
		if err == nil {
			err = json.Unmarshal(body, &reqs)
		}
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Warn().Err(err).Msg("failed to poll for checkpoint requests")
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, req := range reqs {
			l := log.With().Str("action_id", req.ActionId).Str("pod", req.PodName).Str("namespace", req.Namespace).Logger()
			go es.checkpoint(l.WithContext(ctx), req)
		}
	}
	return ctx.Err()
}

func (es *EventStream) get(ctx context.Context, path string) ([]byte, error) {
	return es.do(ctx, http.MethodGet, path, nil)
}

func (es *EventStream) post(ctx context.Context, path string, data []byte) error {
	_, err := es.do(ctx, http.MethodPost, path, data)
	return err
}

func (es *EventStream) do(ctx context.Context, method, path string, data []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, es.propagator.RequestAdapter.GetBaseUrl()+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+config.Global.Connection.AuthToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, res.Status, bytes.TrimSpace(body))
	}
	return body, nil
}
