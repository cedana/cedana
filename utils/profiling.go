package utils

import (
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

		w.WriteHeader(http.StatusOK)
	})

	// Handler to stop CPU profiling
	http.HandleFunc("/stop-profiling", func(w http.ResponseWriter, r *http.Request) {
		pprof.StopCPUProfile()
		w.WriteHeader(http.StatusOK)
	})
}

func StartPprofServer() {
	setupProfilerHandlers()
	go http.ListenAndServe("localhost:6060", nil)
}
