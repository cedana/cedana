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

type JobQueueCallback struct {
	Id     string `json:"id"`
	Ref    string `json:"ref"`
	Failed bool   `json:"failed"`
}
type Args struct {
	jqc JobQueueCallback
	url string
}
type Reply struct {
	success bool
}
type Notifier struct{}

// RPC method that adds two numbers
func (c *Notifier) notify(args *Args, reply *Reply) error {
	b, err := json.Marshal(args.jqc)
	if err != nil {
		return err
	}
	res, err := http.Post(args.url, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	if res.StatusCode == 200 {
		reply.success = true
		return nil
	}
	bytes, err := io.ReadAll(res.Body)
	return fmt.Errorf("failed to notify due to %s", string(bytes))
}

func NotifyServer(ctx context.Context) error {
	// Ensure the socket file does not already exist
	if _, err := os.Stat(JQCallbackSocket); err == nil {
		os.Remove(JQCallbackSocket)
	}
	listener, err := net.Listen("unix", JQCallbackSocket)
	if err != nil {
		return err
	}
	defer listener.Close()
	notifier := new(Notifier)
	rpc.Register(notifier)

	log.Debug().Msgf("Server is listening on %s", JQCallbackSocket)
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

func (js *JobService) notifyJobQueue(id, ref string, failed bool) error {
	log.Info().Msgf("Notifing the jobqueue scheduler: (failed: %v)", failed)
	conn, err := net.Dial("unix", JQCallbackSocket)
	if err != nil {
		return err
	}
	defer conn.Close()
	client := jsonrpc.NewClient(conn)
	args := Args{
		jqc: JobQueueCallback{
			Id:     id,
			Ref:    ref,
			Failed: failed,
		},
		url: js.GetJobQueueUrl(),
	}
	var reply Reply
	err = client.Call("jqnotify", args, &reply)
	if err != nil {
		return err
	}
	if reply.success {
		return nil
	} else {
		return fmt.Errorf("failed to notify jobqueue")
	}
}
