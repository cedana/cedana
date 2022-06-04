package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
)

// server code I think definitely needs to be moved past a REST client
// going to stay REST for faster hacking

func start_server() error {
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalln(err)
		return err
	}
	http.HandleFunc("/init", initHandler)
	http.HandleFunc("/state", getStateHandler)

	return nil
}

func initHandler(w http.ResponseWriter, r *http.Request) {
	// handles initialization request
	// response sends instructions on how to dump
	// everything hardcoded for now
}


func getStateHandler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Panic("Could not read response body", err)
	}

	var request StateHandlerRequest
	err = json.Unmarshal(body, &request)
	if err != nil {
		log.Panic("Could not unmarshal JSON correctly")
	}

	if request.State == "ready" {
		response := &StateHandlerResponse{
			State: request.State,
			Instruction: "dump",
		}

		respBody, err := json.Marshal(response)
		if err != nil {
			log.Fatal("Could not marshal JSON!")
		}

		w.Write(respBody)
	}

	if request.State == "failure" {
		// unimplemented
	}
}
