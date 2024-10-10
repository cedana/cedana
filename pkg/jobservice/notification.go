package jobservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"

	"github.com/rs/zerolog/log"
)

const JQCallbackSocket = "/tmp/jobqueuenotify.sock"

type JobQueueCallback struct {
	Id     string `json:"id"`
	Ref    string `json:"ref"`
	Status string `json:"status"`
}
type Args struct {
	Jqc JobQueueCallback `json:"jqc"`
	Url string           `json:"url"`
}
type InCluster struct{}

// RPC method accepts args to make requests within the clusterdns
// and provide back the reply
func (c *InCluster) Forward(args Args, reply *bool) error {
	log.Info().Msgf("Forwarding Server: JSON RPC: %v to %s", args.Jqc, args.Url)
	b, err := json.Marshal(args.Jqc)
	if err != nil {
		return err
	}
	res, err := http.Post(args.Url, "application/json", bytes.NewReader(b))
	if err != nil {
		log.Error().Err(err).Msg("failed post request")
		return err
	}
	if res.StatusCode == 200 {
		*reply = true
		return nil
	}
	bytes, err := io.ReadAll(res.Body)
	return fmt.Errorf("failed to notify due to %s", string(bytes))
}

func NotifyServer(ctx context.Context) error {
	// Ensure the socket file does not already exist
	hostSocket := "/host" + JQCallbackSocket
	if err := os.RemoveAll(hostSocket); err != nil {
		return err
	}
	listener, err := net.Listen("unix", hostSocket)
	if err != nil {
		return err
	}
	defer listener.Close()
	incluster := new(InCluster)
	rpc.Register(incluster)

	log.Info().Msgf("JSON RPC Forwarding Server is listening on host at %s", JQCallbackSocket)
	// Accept and serve connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Error().Err(err).Msgf("Connection accept error")
			continue
		}
		// Serve the connection using JSON-RPC
		go rpc.ServeCodec(jsonrpc.NewServerCodec(conn))
	}
}

func (js *JobService) notifyJobQueue(kind, id, ref string, failed bool) error {
	log.Info().Msg("Notifing the jobqueue scheduler")
	conn, err := net.Dial("unix", JQCallbackSocket)
	if err != nil {
		return err
	}
	defer conn.Close()
	client := jsonrpc.NewClient(conn)
	var status string
	if failed {
		status = "failed"
	} else {
		status = "succeeded"
	}
	args := Args{
		Jqc: JobQueueCallback{
			Id:     id,
			Ref:    ref,
			Status: status,
		},
		Url: js.GetJobQueueUrl() + "/api/alpha1v1/" + kind + "/callback",
	}
	success := false
	err = client.Call("InCluster.Forward", args, &success)
	if err != nil {
		return err
	}
	if success {
		return nil
	} else {
		return fmt.Errorf("jobqueue rejected state update")
	}
}
