package server

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

// server code I think definitely needs to be moved past a REST client
// going to stay REST for faster hacking
type StateHandlerRequest struct {
	State string `valid:"state"`
}

type StateHandlerResponse struct {
	State       string `valid:"state"`
	Instruction string `valid:"instruction"`
}

func newHTTPServer() (*http.Server, error) {
	r := mux.NewRouter()
	r.HandleFunc("/init", initHandler).Methods("GET")
	r.HandleFunc("/state", getStateHandler).Methods("POST")

	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		log.Fatalln(err)
		return nil, err
	}

	return &http.Server{
		Addr:    "8081",
		Handler: r,
	}, nil
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
			State:       request.State,
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
