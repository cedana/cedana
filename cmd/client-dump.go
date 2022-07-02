package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/checkpoint-restore/go-criu"
	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

type StateHandlerRequest struct {
	State string `valid:"state"`
}

type StateHandlerResponse struct {
	State       string `valid:"state"`
	Instruction string `valid:"instruction"`
}

func init() {
	clientCommand.AddCommand(dumpCommand)
}

var dumpCommand = &cobra.Command{
	Use:   "client",
	Short: "Initialize Client and dump a PID",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := instantiate_client()
		if err != nil {
			return err
		}
		err = dump(c, args[0])
		if err != nil {
			return err
		}
		return nil
	},
}

func dump(c *criu.Criu, pidS string) error {
	pid, err := strconv.ParseInt(pidS, 10, 0)
	if err != nil {
		return fmt.Errorf("can't parse pid: %w", err)
	}

	// TODO - Configurable storage location
	// TODO - Dynamic storage (depending on process)
	img, err := os.Open("~/dump_images")
	if err != nil {
		return fmt.Errorf("Can't open image dir")
	}
	defer img.Close()

	opts := rpc.CriuOpts{
		// TODO: need to annotate this stuff, make it programmable/configurable
		Pid:         proto.Int32(int32(pid)),
		LogLevel:    proto.Int32(1),
		LogFile:     proto.String("dump.log"),
		ImagesDirFd: proto.Int32(int32(img.Fd())),
		ExtMasters:  proto.Bool(true),
		ShellJob:    proto.Bool(true),
		ExtUnixSk:   proto.Bool(true),
	}

	err = c.Dump(opts, criu.NoNotify{})
	if err != nil {
		log.Fatal("Error dumping process: ", err)
		return err
	}

	c.Cleanup()

	return nil
}

func poll_server() (*StateHandlerResponse, error) {
	postBody, _ := json.Marshal(StateHandlerRequest{
		State: "ready",
	})

	responseBody := bytes.NewBuffer(postBody)
	resp, err := http.Post("someUrl/state", "application/json", responseBody)

	if err != nil {
		log.Fatal("Could not post state to server")
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)

	var response StateHandlerResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		log.Fatal("Could not unmarshal JSON correctly")
		return nil, err
	}

	return &response, nil

}

func dump_with_server() {
	// call poll_server, get instructions and dump
}
