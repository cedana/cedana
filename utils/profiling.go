package utils

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime/pprof"
)

func setupProfilerHandlers() {
	// Handler to start CPU profiling
	http.HandleFunc("/start-profiling", func(w http.ResponseWriter, r *http.Request) {
		var filename string
		prefix := r.URL.Query().Get("prefix")
		if prefix == "" {
			filename = "cpu.pprof"
		} else {
			filename = prefix + ".pprof"
		}

		cpu, err := os.Create(filename)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		pprof.StartCPUProfile(cpu)

		// TODO NR - add memory profile
		fmt.Println("Started CPU profiling")

		w.WriteHeader(http.StatusOK)
	})

	// Handler to stop CPU profiling
	http.HandleFunc("/stop-profiling", func(w http.ResponseWriter, r *http.Request) {

		filename := r.URL.Query().Get("filename")
		if filename == "" {
			// Set the header before writing the response body
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "Filename parameter is required")
			return
		}

		pprof.StopCPUProfile()
		f, err := os.Open(filename + ".pprof")
		if err != nil {
			// Set the header before writing the response body
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Error opening file: %v", err)
			return
		}
		defer f.Close()

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Stopped CPU profiling for file: %s\n", filename+".pprof")
	})
}

func StartPprofServer() {
	setupProfilerHandlers()
	go http.ListenAndServe("localhost:6060", nil)
}
