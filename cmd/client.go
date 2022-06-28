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
	rootCmd.AddCommand(clientCommand)
}

var clientCommand = &cobra.Command{
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

func instantiate_client() (*criu.Criu, error) {
	c := criu.MakeCriu()
	// check if version is good, otherwise get out
	_, err := c.GetCriuVersion()
	if err != nil {
		log.Fatal("Error checking CRIU version!", err)
		return nil, err
	}

	// prepare client

	err = c.Prepare()
	if err != nil {
		log.Fatal("Error preparing CRIU client", err)
		return nil, err
	}

	// TODO: How to clean up client? Don't want to instatiate every time

	return c, nil
}

func dump(c *criu.Criu, pidS string) error {
	pid, err := strconv.ParseInt(pidS, 10, 0)
	if err != nil {
		return fmt.Errorf("can't parse pid: %w", err)
	}

	img, err := os.Open("/home/nravic/code/oort")
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

func restore(c *criu.Criu) error {
	opts := rpc.CriuOpts{
		Pid:      proto.Int32(int32(1000)),
		LogLevel: proto.Int32(4),
		LogFile:  proto.String("dump.log"),
	}

	err := c.Restore(opts, criu.NoNotify{})
	if err != nil {
		log.Fatal("Error restoring process!", err)
		return err
	}

	return nil
}

func start_client() error {
	// get opts from Server (opts are stuff like what PID, how long, etc etc)
	// dump PID from opts
	// get restore command from Server
	// if restore, RESTORE
	// stop dumping, send status to server

	// response is useless for now, but should contain instructions on dumping
	c, err := instantiate_client()
	if err != nil {
		log.Fatal("Could not instantiate client", err)
		return err
	}

	_, err = http.Get("someUrl/init")
	if err != nil {
		log.Fatal("Could not init w/ server", err)
		return err
	}

	// do this in a loop/use some scheduler. Hacky, just dump once hehe
	postBody, _ := json.Marshal(StateHandlerRequest{
		State: "ready",
	})

	responseBody := bytes.NewBuffer(postBody)
	resp, err := http.Post("someUrl/state", "application/json", responseBody)

	if err != nil {
		log.Fatal("Could not post state to server")
		return err
	}

	body, err := ioutil.ReadAll(resp.Body)

	var response StateHandlerResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		log.Fatal("Could not unmarshal JSON correctly")
		return err
	}

	if response.Instruction == "dump" {
		err = dump(c, "1000")
		if err != nil {
			log.Fatal("Error dumping process to file", err)
			return err
		}
	}

	return nil

}
