package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"

	"github.com/checkpoint-restore/go-criu/"
	"github.com/checkpoint-restore/go-criu/rpc"
	"google.golang.org/protobuf/proto"
)

type StateHandlerRequest struct {
	State string `valid:"state"`
}

type StateHandlerResponse struct {
	State string `valid:"state"`
	Instruction string `valid:"instruction"`
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

	return c, nil
}


func dump(c *criu.Criu, pidS string) error {
	pid, err := strconv.ParseInt(pidS, 10, 0)
	if err != nil {
		return fmt.Errorf("can't parse pid: %w", err)
	}

	opts := &rpc.CriuOpts{
		Pid:         proto.Int32(int32(pid)),
		LogLevel:    proto.Int32(4),
		LogFile:     proto.String("dump.log"),
	}

	err = c.Dump(opts, criu.NoNotify{})
	if err != nil {
		log.Fatal("Error dumping process!", err)
		return err
	}

	return nil
}

func restore(c *criu.Criu) error {
	opts := &rpc.CriuOpts{
		Pid:         proto.Int32(int32(pid)),
		LogLevel:    proto.Int32(4),
		LogFile:     proto.String("dump.log"),
	}

	err := c.Restore(opts, criu.NoNotify{})
	if err != nil {
		log.Fatal("Error restoring process!", err)
		return err
	}
}


func start_client() error  {
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
